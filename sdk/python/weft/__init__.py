"""Weft Python Agent SDK (Runtime Protocol v1).

    from weft import Agent, serve

    class Echo(Agent):
        name = "echo"
        def turn(self, ctx):
            for m in ctx.inputs:
                ctx.emit("echo", m.payload)
            return f"echoed {len(ctx.inputs)}"

    serve([Echo()], port=8000)
"""
__version__ = "0.1.0"

from .agent import Agent, FunctionAgent, agent  # noqa: E402
from .callback import CallbackClient, CallbackError, NotFound, PermissionDenied  # noqa: E402
from .context import TurnContext  # noqa: E402
from .jsonutil import extract_json  # noqa: E402
from .server import RuntimeApp, make_server, serve  # noqa: E402
from .types import (  # noqa: E402
    ChatMessage,
    ChatRequest,
    ChatResponse,
    Emit,
    Message,
    ToolCall,
    ToolResult,
    ToolSchema,
    TurnInput,
    TurnResult,
)

__all__ = [
    "Agent", "FunctionAgent", "agent", "serve", "make_server", "RuntimeApp",
    "TurnContext", "CallbackClient", "CallbackError", "PermissionDenied", "NotFound",
    "extract_json", "ChatMessage", "ChatRequest", "ChatResponse", "Emit", "Message",
    "ToolCall", "ToolResult", "ToolSchema", "TurnInput", "TurnResult",
]
