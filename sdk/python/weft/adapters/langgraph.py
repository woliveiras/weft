"""LangGraph adapter.

Assumes ``langgraph>=0.2`` and ``langchain-core>=0.3`` (``BaseChatModel``,
``StructuredTool.from_function`` accepting a JSON-schema dict as
``args_schema``). Older versions may need small changes; the adapter only
touches those two APIs plus ``CompiledGraph.invoke``.

Usage::

    def build(ctx):
        g = StateGraph(dict)
        g.add_node("think", lambda s: {...})
        ...
        return g.compile()

    reviewer = LangGraphAgent("reviewer", build)

The compiled graph is invoked with ``initial_state(ctx)`` (default:
``{"inputs": [...messages as dicts], "prompt": ctx.prompt}``). From the
final state, ``emits`` (a list of ``{"type", "payload", ...}``) are emitted
and ``summary`` becomes the turn summary. Nodes may also close over ``ctx``
(``build`` receives it) and call ``ctx.emit`` / ``ctx.tools`` directly.
"""
from __future__ import annotations

from typing import Any, Callable, Dict, List, Optional

from ..agent import Agent
from ..context import TurnContext
from ..types import ChatMessage
from . import common


class LangGraphAgent(Agent):
    framework = "langgraph"

    def __init__(self, name: str, build_graph: Callable[[TurnContext], Any],
                 initial_state: Optional[Callable[[TurnContext], Dict[str, Any]]] = None,
                 emits_key: str = "emits", summary_key: str = "summary", recursion_limit: int = 50):
        super().__init__(name)
        self.build_graph = build_graph
        self.initial_state = initial_state or (lambda ctx: {"inputs": [m.to_json() for m in ctx.inputs],
                                                            "prompt": ctx.prompt})
        self.emits_key = emits_key
        self.summary_key = summary_key
        self.recursion_limit = recursion_limit

    def turn(self, ctx: TurnContext) -> Optional[str]:
        from . import require
        require("langgraph", "langgraph")
        graph = self.build_graph(ctx)
        final = graph.invoke(self.initial_state(ctx), {"recursion_limit": self.recursion_limit})
        if not isinstance(final, dict):
            return f"langgraph finished ({type(final).__name__})"
        common.emit_from_state(ctx, final.get(self.emits_key))
        summary = final.get(self.summary_key)
        return str(summary) if summary else "langgraph turn complete"


def weft_tools_for_langchain(ctx: TurnContext) -> List[Any]:
    """Granted gateway tools as LangChain ``StructuredTool`` objects."""
    from . import require
    tools_mod = require("langchain_core.tools", "langgraph")
    out = []
    for t in common.gateway_tool_functions(ctx):
        out.append(tools_mod.StructuredTool.from_function(
            func=t.fn, name=t.wire_name, description=t.description, args_schema=t.parameters))
    return out


_chat_model_cls: Any = None


def weft_chat_model(ctx: TurnContext, json_mode: bool = False) -> Any:
    """A minimal LangChain chat model that calls the platform LLM gateway."""
    global _chat_model_cls
    if _chat_model_cls is None:
        from . import require
        lc_models = require("langchain_core.language_models.chat_models", "langgraph")
        lc_messages = require("langchain_core.messages", "langgraph")
        lc_outputs = require("langchain_core.outputs", "langgraph")

        class WeftChatModel(lc_models.BaseChatModel):  # type: ignore[misc, valid-type]
            weft_ctx: Any = None
            json_mode: bool = False

            @property
            def _llm_type(self) -> str:
                return "weft"

            def _generate(self, messages, stop=None, run_manager=None, **kwargs):
                conv: List[ChatMessage] = []
                system = ""
                for m in messages:
                    role = {"system": "system", "human": "user", "ai": "assistant", "tool": "tool"}.get(m.type, "user")
                    if role == "system":
                        system += str(m.content) + "\n"
                        continue
                    conv.append(ChatMessage(role=role, content=str(m.content),
                                            tool_call_id=getattr(m, "tool_call_id", "") or ""))
                resp = self.weft_ctx.llm.chat(conv, system=system or None, json=self.json_mode)
                ai = lc_messages.AIMessage(content=resp.text)
                return lc_outputs.ChatResult(generations=[lc_outputs.ChatGeneration(message=ai)])

        _chat_model_cls = WeftChatModel
    return _chat_model_cls(weft_ctx=ctx, json_mode=json_mode)
