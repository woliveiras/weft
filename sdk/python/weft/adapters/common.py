"""Framework-neutral helpers shared by the adapters."""
from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Any, Callable, Dict, List, Optional

from ..callback import PermissionDenied
from ..context import TurnContext
from ..types import ToolSchema


@dataclass
class GatewayTool:
    """A granted gateway tool as a plain callable plus its schema."""

    name: str
    wire_name: str  # dots replaced: frameworks want ^[A-Za-z0-9_-]+$
    description: str
    parameters: Dict[str, Any]
    fn: Callable[..., str]


def gateway_tool_functions(ctx: TurnContext, schemas: Optional[List[ToolSchema]] = None) -> List[GatewayTool]:
    """Wrap each granted tool (from the turn input) as ``fn(**args) -> str``.

    Denials and tool errors are returned as text rather than raised, so a
    framework's agent loop sees them as observations and can recover.
    """
    out: List[GatewayTool] = []
    for s in schemas if schemas is not None else ctx.input.tools:
        out.append(GatewayTool(s.name, s.name.replace(".", "__"), s.description or s.name,
                               s.parameters or {"type": "object", "properties": {}}, _make_fn(ctx, s.name)))
    return out


def _make_fn(ctx: TurnContext, name: str) -> Callable[..., str]:
    def call(**args: Any) -> str:
        try:
            res = ctx.tools.call(name, **args)
        except PermissionDenied as e:
            return f"permission denied: {e}"
        text = res.content
        if res.is_error:
            text = "error: " + text
        return text

    call.__name__ = name.replace(".", "__")
    return call


def emit_from_state(ctx: TurnContext, items: Any) -> int:
    """Emit a list of ``{"type", "payload", "target"?, "channel"?}`` dicts."""
    n = 0
    for it in items or []:
        if isinstance(it, dict) and it.get("type"):
            ctx.emit(it["type"], it.get("payload") or {}, target=it.get("target", ""),
                     channel=it.get("channel", ""), metadata=it.get("metadata"))
            n += 1
    return n


def inputs_as_text(ctx: TurnContext) -> str:
    return "\n".join(f"[{m.type}] {json.dumps(m.payload)}" for m in ctx.inputs)
