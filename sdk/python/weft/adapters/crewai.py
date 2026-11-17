"""CrewAI adapter.

Assumes ``crewai>=0.80``: ``crewai.tools.BaseTool`` (with ``_run``) and
the custom-LLM base class ``crewai.BaseLLM`` (exported from the top-level
package in recent releases; older releases expose it as
``crewai.llms.base_llm.BaseLLM``). The LLM wrapper sends CrewAI's messages
to the platform LLM gateway; the tool wrappers call the platform tool
gateway.

Usage::

    def build(ctx, llm, tools):
        a = Agent(role="Researcher", goal="...", backstory="...", llm=llm, tools=tools)
        t = Task(description="{request}", expected_output="notes", agent=a)
        return Crew(agents=[a], tasks=[t])

    research = CrewAIAgent("researcher", build, emit_type="research.done")
"""
from __future__ import annotations

import json
from typing import Any, Callable, Dict, List, Optional

from ..agent import Agent
from ..context import TurnContext
from ..types import ChatMessage
from . import common


class CrewAIAgent(Agent):
    framework = "crewai"

    def __init__(self, name: str, build_crew: Callable[[TurnContext, Any, List[Any]], Any],
                 inputs: Optional[Callable[[TurnContext], Dict[str, Any]]] = None, emit_type: str = ""):
        super().__init__(name)
        self.build_crew = build_crew
        self.inputs = inputs or (lambda ctx: {"prompt": ctx.prompt, "request": common.inputs_as_text(ctx)})
        self.emit_type = emit_type

    def turn(self, ctx: TurnContext) -> Optional[str]:
        from . import require
        require("crewai", "crewai")
        crew = self.build_crew(ctx, weft_crewai_llm(ctx), weft_crewai_tools(ctx))
        out = crew.kickoff(inputs=self.inputs(ctx))
        text = str(getattr(out, "raw", out))
        if self.emit_type and text:
            ctx.emit(self.emit_type, {"text": text})
        return text[:500] or "crewai turn complete"


def _base_llm():
    from . import require
    crewai = require("crewai", "crewai")
    base = getattr(crewai, "BaseLLM", None)
    if base is None:
        base = require("crewai.llms.base_llm", "crewai").BaseLLM
    return base


_llm_cls: Any = None


def weft_crewai_llm(ctx: TurnContext) -> Any:
    global _llm_cls
    if _llm_cls is None:
        Base = _base_llm()

        class WeftLLM(Base):  # type: ignore[misc, valid-type]
            def __init__(self, c: TurnContext):
                super().__init__(model=str(c.input.model.get("name") or "weft"))
                self._ctx = c

            def call(self, messages, tools=None, callbacks=None, available_functions=None, **kw):
                if isinstance(messages, str):
                    messages = [{"role": "user", "content": messages}]
                system = "\n".join(m["content"] for m in messages if m.get("role") == "system")
                conv = [ChatMessage(role=m.get("role", "user"), content=str(m.get("content", "")))
                        for m in messages if m.get("role") != "system"]
                return self._ctx.llm.chat(conv, system=system or None).text

            def supports_function_calling(self) -> bool:
                return False

            def supports_stop_words(self) -> bool:
                return False

            def get_context_window_size(self) -> int:
                return 32_000

        _llm_cls = WeftLLM
    return _llm_cls(ctx)


def weft_crewai_tools(ctx: TurnContext) -> List[Any]:
    from . import require
    tools_mod = require("crewai.tools", "crewai")
    out = []
    for t in common.gateway_tool_functions(ctx):
        def make(gt: common.GatewayTool):
            class GatewayCrewTool(tools_mod.BaseTool):  # type: ignore[misc, valid-type]
                name: str = gt.wire_name
                description: str = f"{gt.description}. Arguments (JSON schema): {json.dumps(gt.parameters)}"

                def _run(self, **kwargs: Any) -> str:
                    return gt.fn(**kwargs)
            return GatewayCrewTool()
        out.append(make(t))
    return out
