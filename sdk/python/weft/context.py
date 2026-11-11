"""TurnContext: everything an agent can see and do during one turn.

The context is the agent's only door to the platform. Tools, models, state
and human operators are reached through the callback API, which the gateway
authorises against the node's grants - the agent never holds credentials.
"""
from __future__ import annotations

import json as _json
import logging
from typing import Any, Dict, Iterable, List, Optional, Protocol

from .callback import PermissionDenied
from .jsonutil import extract_json
from .types import (
    SIGNAL_CONFIRM_STOP,
    SIGNAL_NO_FURTHER_WORK,
    ChatMessage,
    ChatRequest,
    ChatResponse,
    Emit,
    Message,
    ToolResult,
    ToolSchema,
    TurnInput,
    TurnResult,
    Usage,
)

log = logging.getLogger("weft")


class Backend(Protocol):
    """Transport behind a context: CallbackClient in production, a fake in tests."""

    def invoke_tool(self, name: str, args: Dict[str, Any]) -> Dict[str, Any]: ...
    def list_tools(self) -> List[Dict[str, Any]]: ...
    def chat(self, request: Dict[str, Any]) -> Dict[str, Any]: ...
    def emit(self, emit: Dict[str, Any]) -> Dict[str, Any]: ...
    def state_get(self, key: str) -> Optional[bytes]: ...
    def state_put(self, key: str, value: bytes) -> None: ...
    def output(self, text: str) -> None: ...
    def ask(self, question: str) -> str: ...


class LLM:
    """Model access through the platform's LLM gateway."""

    def __init__(self, ctx: "TurnContext"):
        self._ctx = ctx

    def chat(self, messages: Iterable[Any], system: Optional[str] = None, json: bool = False,
             tools: Optional[List[Any]] = None, model: Optional[str] = None,
             temperature: Optional[float] = None, max_tokens: int = 0,
             metadata: Optional[Dict[str, str]] = None) -> ChatResponse:
        req = ChatRequest(
            messages=[ChatMessage.coerce(m) for m in messages],
            model=model or "",
            system=system or "",
            tools=[t if isinstance(t, ToolSchema) else ToolSchema.from_json(t) for t in tools or []],
            temperature=temperature,
            max_tokens=max_tokens,
            json=json,
            metadata=metadata or {},
        )
        resp = ChatResponse.from_json(self._ctx._backend.chat(req.to_json()))
        # Gateway usage is already recorded by the server, not runtime-local usage.
        return resp

    def complete(self, prompt: str, **kw: Any) -> str:
        return self.chat([ChatMessage(role="user", content=prompt)], **kw).text

    def json(self, prompt: str, schema_hint: Optional[str] = None, default: Any = None, **kw: Any) -> Any:
        """Ask for a JSON object; return the parsed value or ``default``.

        Never raises on malformed model output - agents decide what a usable
        answer looks like and fall back otherwise.
        """
        text = prompt
        if schema_hint:
            text += "\n\nRespond with a single JSON object matching:\n" + schema_hint
        try:
            out = self.chat([ChatMessage(role="user", content=text)], json=True, **kw).text
        except Exception as e:  # transport failure is not a parse failure, but agents want one path
            log.warning("llm.json failed: %s", e)
            return default
        parsed = extract_json(out)
        return default if parsed is None else parsed


class Tools:
    """Gateway tools. Calls the node is not granted raise PermissionDenied."""

    def __init__(self, ctx: "TurnContext"):
        self._ctx = ctx

    def call(self, name: str, **args: Any) -> ToolResult:
        # Gateway attempts are counted by the server, including denied calls.
        res = ToolResult.from_json(self._ctx._backend.invoke_tool(name, args))
        return res

    def try_call(self, name: str, **args: Any) -> Optional[ToolResult]:
        """Like call, but returns None when the tool is not granted."""
        try:
            return self.call(name, **args)
        except PermissionDenied:
            return None

    def list(self) -> List[ToolSchema]:
        return [ToolSchema.from_json(t) for t in self._ctx._backend.list_tools()]

    def granted(self, name: str) -> bool:
        return any(t.name == name for t in self._ctx.input.tools)


class State:
    """Durable agent memory (namespaced by the platform per node)."""

    def __init__(self, ctx: "TurnContext"):
        self._ctx = ctx

    def get(self, key: str, default: Optional[bytes] = None) -> Optional[bytes]:
        v = self._ctx._backend.state_get(key)
        return default if v is None else v

    def set(self, key: str, value: Any) -> None:
        if isinstance(value, str):
            value = value.encode()
        self._ctx._backend.state_put(key, bytes(value))

    def get_json(self, key: str, default: Any = None) -> Any:
        v = self.get(key)
        if v is None:
            return default
        try:
            return _json.loads(v)
        except ValueError:
            return default

    def set_json(self, key: str, value: Any) -> None:
        self.set(key, _json.dumps(value).encode())


class TurnContext:
    def __init__(self, turn_input: TurnInput, backend: Backend):
        self.input = turn_input
        self._backend = backend
        self._result = TurnResult()
        self.llm = LLM(self)
        self.tools = Tools(self)
        self.state = State(self)
        self.log = logging.getLogger(f"weft.agent.{turn_input.agent or turn_input.node_id}")

    # -- read-only views -----------------------------------------------------
    @property
    def inputs(self) -> List[Message]:
        return self.input.inputs

    @property
    def prompt(self) -> str:
        return self.input.prompt

    @property
    def system(self) -> str:
        return self.input.system

    @property
    def operator_messages(self) -> List[str]:
        return self.input.operator_messages

    @property
    def neighbors(self):
        return self.input.neighbors

    @property
    def pending_stop(self) -> bool:
        return self.input.pending_stop

    @property
    def parameters(self) -> Dict[str, str]:
        return self.input.parameters

    @property
    def config(self) -> Dict[str, Any]:
        return self.input.config

    @property
    def replica(self) -> int:
        return self.input.replica

    @property
    def replicas(self) -> int:
        return self.input.replicas

    @property
    def iteration(self) -> int:
        return self.input.iteration

    @property
    def node_id(self) -> str:
        return self.input.node_id

    @property
    def result(self) -> TurnResult:
        return self._result

    # -- outputs ---------------------------------------------------------------
    def emit(self, type: str, payload: Any = None, target: str = "", channel: str = "",
             metadata: Optional[Dict[str, Any]] = None) -> None:
        """Queue an output message; it is published when the turn succeeds."""
        self._result.emits.append(Emit(type=type, payload=payload if payload is not None else {},
                                       target=target, channel=channel, metadata=metadata or {}))

    def emit_now(self, type: str, payload: Any = None, target: str = "", channel: str = "",
                 metadata: Optional[Dict[str, Any]] = None) -> None:
        """Hand an output to the engine immediately (long turns). Still only
        published once the turn succeeds; this just avoids holding it here."""
        e = Emit(type=type, payload=payload if payload is not None else {}, target=target,
                 channel=channel, metadata=metadata or {})
        self._backend.emit(e.to_json())

    def output(self, text: str) -> None:
        """Narrate to observers (UI log, event log). Best effort."""
        try:
            self._backend.output(text)
        except Exception as e:
            self.log.debug("output failed: %s", e)

    def delegate(self, objective: str, template: str = "analysis") -> ToolResult:
        """Wait for a server-managed native child through delegation.request.

        Requires an exact grant. Installation approval, inherited permissions,
        duration and shared token admission remain server-enforced. Operational
        child failures return an error ToolResult; authorization errors propagate.
        Calls are never retried automatically. Configure the callback timeout for
        the installation's approval/child deadline when using long-running work.
        """
        return self.tools.call("delegation.request", template=template, objective=objective)

    def ask_human(self, question: str) -> str:
        """Block until an operator answers (human-in-the-loop)."""
        return self._backend.ask(question)

    # -- signals ---------------------------------------------------------------
    def declare_done(self) -> None:
        self._result.signal = SIGNAL_NO_FURTHER_WORK

    def confirm_stop(self) -> None:
        self._result.signal = SIGNAL_CONFIRM_STOP

    def broadcast(self, text: str) -> None:
        self._result.broadcast = text

    def retry_later(self) -> None:
        """Nack the inputs instead of acking them; they will be redelivered."""
        self._result.retry = True

    def set_summary(self, text: str) -> None:
        self._result.summary = text

    def add_usage(self, usage: Usage) -> None:
        self._result.usage.add(usage)
