"""AutoGen (AgentChat 0.4+) adapter.

Assumes ``autogen-agentchat>=0.4`` / ``autogen-core>=0.4``: the
``ChatCompletionClient`` interface (``create`` returning ``CreateResult``)
and ``FunctionTool``. The model client implements ``create`` through the
platform LLM gateway; streaming (``create_stream``) yields the single
non-streamed result. Tool calls requested by the model are not passed back
as structured function calls - AutoGen agents should use the tools from
:func:`weft_autogen_tools`, which AutoGen executes itself and which call
the platform tool gateway.

Each tool takes a single ``args_json`` string argument (the JSON arguments
object; its schema is in the tool description). That keeps the wrapper
independent of AutoGen's pydantic schema inference.

Usage::

    def build(ctx, model_client, tools):
        return AssistantAgent("dev", model_client=model_client, tools=tools)

    dev = AutoGenAgent("developer", build, emit_type="code.ready")
"""
from __future__ import annotations

import asyncio
import json
from typing import Any, Callable, List, Optional

from ..agent import Agent
from ..context import TurnContext
from ..types import ChatMessage
from . import common


class AutoGenAgent(Agent):
    framework = "autogen"

    def __init__(self, name: str, build: Callable[[TurnContext, Any, List[Any]], Any],
                 task: Optional[Callable[[TurnContext], str]] = None, emit_type: str = ""):
        super().__init__(name)
        self.build = build
        self.task = task or (lambda ctx: (ctx.prompt + "\n\n" + common.inputs_as_text(ctx)).strip())
        self.emit_type = emit_type

    def turn(self, ctx: TurnContext) -> Optional[str]:
        from . import require
        require("autogen_agentchat", "autogen")
        runnable = self.build(ctx, weft_model_client(ctx), weft_autogen_tools(ctx))
        result = asyncio.run(runnable.run(task=self.task(ctx)))
        msgs = getattr(result, "messages", []) or []
        final = str(getattr(msgs[-1], "content", "")) if msgs else ""
        if self.emit_type and final:
            ctx.emit(self.emit_type, {"text": final})
        return final[:500] or "autogen turn complete"


def weft_autogen_tools(ctx: TurnContext) -> List[Any]:
    from . import require
    tools_mod = require("autogen_core.tools", "autogen")
    out = []
    for t in common.gateway_tool_functions(ctx):
        def make(gt: common.GatewayTool):
            def run(args_json: str) -> str:
                try:
                    args = json.loads(args_json) if args_json else {}
                except ValueError as e:
                    return f"invalid args_json: {e}"
                return gt.fn(**args)
            return run
        out.append(tools_mod.FunctionTool(
            make(t), name=t.wire_name,
            description=f"{t.description}\nArgument args_json is a JSON object matching: {json.dumps(t.parameters)}"))
    return out


_client_cls: Any = None


def weft_model_client(ctx: TurnContext) -> Any:
    global _client_cls
    if _client_cls is None:
        from . import require
        models = require("autogen_core.models", "autogen")

        class WeftModelClient(models.ChatCompletionClient):  # type: ignore[misc, valid-type]
            def __init__(self, c: TurnContext):
                self._ctx = c
                self._usage = models.RequestUsage(prompt_tokens=0, completion_tokens=0)

            async def create(self, messages, *, tools=(), json_output=None, extra_create_args={},
                             cancellation_token=None, **kw):
                conv: List[ChatMessage] = []
                system = ""
                for m in messages:
                    kind = type(m).__name__
                    content = m.content if isinstance(m.content, str) else json.dumps(
                        [getattr(x, "content", str(x)) for x in m.content], default=str)
                    if kind == "SystemMessage":
                        system += content + "\n"
                    elif kind == "AssistantMessage":
                        conv.append(ChatMessage(role="assistant", content=content))
                    elif kind == "FunctionExecutionResultMessage":
                        conv.append(ChatMessage(role="user", content="Tool results: " + content))
                    else:
                        conv.append(ChatMessage(role="user", content=content))
                resp = await asyncio.to_thread(self._ctx.llm.chat, conv, system or None, bool(json_output))
                u = models.RequestUsage(prompt_tokens=resp.usage.input_tokens,
                                        completion_tokens=resp.usage.output_tokens)
                self._usage = models.RequestUsage(prompt_tokens=self._usage.prompt_tokens + u.prompt_tokens,
                                                  completion_tokens=self._usage.completion_tokens + u.completion_tokens)
                return models.CreateResult(finish_reason="stop", content=resp.text, usage=u, cached=False)

            async def create_stream(self, messages, **kw):
                yield await self.create(messages, **kw)

            async def close(self):
                return None

            def actual_usage(self):
                return self._usage

            def total_usage(self):
                return self._usage

            def count_tokens(self, messages, *, tools=()):
                return sum(len(str(getattr(m, "content", ""))) // 4 for m in messages)

            def remaining_tokens(self, messages, *, tools=()):
                return 100_000 - self.count_tokens(messages)

            @property
            def capabilities(self):
                return self.model_info

            @property
            def model_info(self):
                return {"vision": False, "function_calling": False, "json_output": True,
                        "family": "unknown", "structured_output": False}

        _client_cls = WeftModelClient
    return _client_cls(ctx)
