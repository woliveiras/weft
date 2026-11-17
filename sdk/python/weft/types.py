"""Wire types of Runtime Protocol v1.

Field names on the wire are camelCase and mirror the Go structs in
``server/internal/runtime``, ``server/internal/llm`` and
``server/internal/tools``. Python attributes are snake_case.
"""
from __future__ import annotations

import json
from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional


def _drop_empty(d: Dict[str, Any]) -> Dict[str, Any]:
    return {k: v for k, v in d.items() if v not in (None, "", [], {})}


@dataclass
class Message:
    id: str = ""
    type: str = ""
    source: str = ""
    target: str = ""
    payload: Any = field(default_factory=dict)
    metadata: Dict[str, Any] = field(default_factory=dict)

    @classmethod
    def from_json(cls, d: Optional[Dict[str, Any]]) -> "Message":
        d = d or {}
        payload = d.get("payload")
        if payload is None:
            payload = {}
        return cls(
            id=d.get("id", ""),
            type=d.get("type", ""),
            source=d.get("source", ""),
            target=d.get("target", ""),
            payload=payload,
            metadata=d.get("metadata") or {},
        )

    def to_json(self) -> Dict[str, Any]:
        return {
            "id": self.id,
            "type": self.type,
            "source": self.source,
            "target": self.target,
            "payload": self.payload,
            "metadata": self.metadata,
        }

    @property
    def correlation_id(self) -> str:
        return str(self.metadata.get("correlation_id") or self.id)


@dataclass
class ToolCall:
    id: str = ""
    name: str = ""
    arguments: Any = field(default_factory=dict)

    @classmethod
    def from_json(cls, d: Dict[str, Any]) -> "ToolCall":
        args = d.get("arguments")
        if isinstance(args, str):
            try:
                args = json.loads(args)
            except ValueError:
                pass
        return cls(id=d.get("id", ""), name=d.get("name", ""), arguments=args if args is not None else {})

    def to_json(self) -> Dict[str, Any]:
        return {"id": self.id, "name": self.name, "arguments": self.arguments}


@dataclass
class ChatMessage:
    role: str = "user"
    content: str = ""
    tool_calls: List[ToolCall] = field(default_factory=list)
    tool_call_id: str = ""
    name: str = ""

    @classmethod
    def from_json(cls, d: Dict[str, Any]) -> "ChatMessage":
        return cls(
            role=d.get("role", "assistant"),
            content=d.get("content") or "",
            tool_calls=[ToolCall.from_json(t) for t in d.get("toolCalls") or []],
            tool_call_id=d.get("toolCallId", ""),
            name=d.get("name", ""),
        )

    def to_json(self) -> Dict[str, Any]:
        d: Dict[str, Any] = {"role": self.role, "content": self.content}
        if self.tool_calls:
            d["toolCalls"] = [t.to_json() for t in self.tool_calls]
        if self.tool_call_id:
            d["toolCallId"] = self.tool_call_id
        if self.name:
            d["name"] = self.name
        return d

    @staticmethod
    def coerce(m: Any) -> "ChatMessage":
        if isinstance(m, ChatMessage):
            return m
        if isinstance(m, str):
            return ChatMessage(role="user", content=m)
        if isinstance(m, dict):
            # Accept both our camelCase and OpenAI-ish snake_case.
            d = dict(m)
            if "tool_calls" in d and "toolCalls" not in d:
                d["toolCalls"] = d.pop("tool_calls")
            if "tool_call_id" in d and "toolCallId" not in d:
                d["toolCallId"] = d.pop("tool_call_id")
            return ChatMessage.from_json(d)
        raise TypeError(f"cannot convert {type(m).__name__} to ChatMessage")


@dataclass
class ToolSchema:
    name: str = ""
    description: str = ""
    parameters: Dict[str, Any] = field(default_factory=dict)

    @classmethod
    def from_json(cls, d: Dict[str, Any]) -> "ToolSchema":
        return cls(name=d.get("name", ""), description=d.get("description", ""), parameters=d.get("parameters") or {})

    def to_json(self) -> Dict[str, Any]:
        return {"name": self.name, "description": self.description, "parameters": self.parameters}


@dataclass
class Usage:
    input_tokens: int = 0
    output_tokens: int = 0
    # Parts of input_tokens read from / written to the provider's prompt
    # cache, when the provider reports them.
    cache_read_tokens: int = 0
    cache_write_tokens: int = 0

    @classmethod
    def from_json(cls, d: Optional[Dict[str, Any]]) -> "Usage":
        d = d or {}
        return cls(int(d.get("inputTokens") or 0), int(d.get("outputTokens") or 0),
                   int(d.get("cacheReadTokens") or 0), int(d.get("cacheWriteTokens") or 0))

    def to_json(self) -> Dict[str, Any]:
        out: Dict[str, Any] = {"inputTokens": self.input_tokens, "outputTokens": self.output_tokens}
        if self.cache_read_tokens:
            out["cacheReadTokens"] = self.cache_read_tokens
        if self.cache_write_tokens:
            out["cacheWriteTokens"] = self.cache_write_tokens
        return out

    def add(self, other: "Usage") -> None:
        self.input_tokens += other.input_tokens
        self.output_tokens += other.output_tokens
        self.cache_read_tokens += other.cache_read_tokens
        self.cache_write_tokens += other.cache_write_tokens


@dataclass
class ChatRequest:
    messages: List[ChatMessage] = field(default_factory=list)
    model: str = ""
    system: str = ""
    tools: List[ToolSchema] = field(default_factory=list)
    temperature: Optional[float] = None
    max_tokens: int = 0
    json: bool = False
    stop: List[str] = field(default_factory=list)
    metadata: Dict[str, str] = field(default_factory=dict)

    def to_json(self) -> Dict[str, Any]:
        d: Dict[str, Any] = {
            "model": self.model,
            "system": self.system,
            "messages": [m.to_json() for m in self.messages],
            "tools": [t.to_json() for t in self.tools],
            "temperature": self.temperature,
            "maxTokens": self.max_tokens,
            "json": self.json,
            "stop": self.stop,
            "metadata": self.metadata,
        }
        out = _drop_empty(d)
        out["messages"] = d["messages"]
        out.setdefault("model", "")
        return out


@dataclass
class ChatResponse:
    message: ChatMessage = field(default_factory=ChatMessage)
    stop_reason: str = ""
    model: str = ""
    usage: Usage = field(default_factory=Usage)

    @classmethod
    def from_json(cls, d: Dict[str, Any]) -> "ChatResponse":
        return cls(
            message=ChatMessage.from_json(d.get("message") or {}),
            stop_reason=d.get("stopReason", ""),
            model=d.get("model", ""),
            usage=Usage.from_json(d.get("usage")),
        )

    @property
    def text(self) -> str:
        return self.message.content


@dataclass
class ToolResult:
    content: str = ""
    data: Any = None
    is_error: bool = False
    truncated: bool = False
    yield_turn: bool = False  # Wire field "yield" is a Python keyword.

    @classmethod
    def from_json(cls, d: Dict[str, Any]) -> "ToolResult":
        return cls(
            content=d.get("content") or "",
            data=d.get("data"),
            is_error=bool(d.get("isError")),
            truncated=bool(d.get("truncated")),
            yield_turn=bool(d.get("yield")),
        )

    def to_json(self) -> Dict[str, Any]:
        out = _drop_empty({"content": self.content, "data": self.data, "isError": self.is_error,
                           "truncated": self.truncated}) or {"content": ""}
        if self.yield_turn:
            out["yield"] = True
        return out


@dataclass
class Emit:
    type: str
    payload: Any = field(default_factory=dict)
    target: str = ""
    channel: str = ""
    metadata: Dict[str, Any] = field(default_factory=dict)

    def to_json(self) -> Dict[str, Any]:
        d: Dict[str, Any] = {"type": self.type, "payload": self.payload if self.payload is not None else {}}
        if self.target:
            d["target"] = self.target
        if self.channel:
            d["channel"] = self.channel
        if self.metadata:
            d["metadata"] = self.metadata
        return d


@dataclass
class NeighborNote:
    replica: int = 0
    text: str = ""
    at: str = ""

    @classmethod
    def from_json(cls, d: Dict[str, Any]) -> "NeighborNote":
        return cls(int(d.get("replica") or 0), d.get("text", ""), d.get("at", ""))


@dataclass
class OutputInfo:
    channel: str = ""
    type: str = ""
    targets: List[str] = field(default_factory=list)
    types: List[str] = field(default_factory=list)
    when: str = ""

    @classmethod
    def from_json(cls, d: Dict[str, Any]) -> "OutputInfo":
        return cls(d.get("channel", ""), d.get("type", ""), d.get("targets") or [], d.get("types") or [],
                   d.get("when", ""))


@dataclass
class Callback:
    url: str = ""
    token: str = ""


@dataclass
class TurnInput:
    execution_id: str = ""
    graph_id: str = ""
    graph_name: str = ""
    node_id: str = ""
    node_name: str = ""
    replica: int = 0
    replicas: int = 1
    iteration: int = 0
    agent: str = ""
    prompt: str = ""
    system: str = ""
    inputs: List[Message] = field(default_factory=list)
    operator_messages: List[str] = field(default_factory=list)
    neighbors: List[NeighborNote] = field(default_factory=list)
    outputs: List[OutputInfo] = field(default_factory=list)
    tools: List[ToolSchema] = field(default_factory=list)
    model: Dict[str, Any] = field(default_factory=dict)
    pending_stop: bool = False
    workdir: str = ""
    branch: str = ""
    parameters: Dict[str, str] = field(default_factory=dict)
    config: Dict[str, Any] = field(default_factory=dict)
    callback: Optional[Callback] = None
    instructions: Optional[Dict[str, Any]] = None

    @classmethod
    def from_json(cls, d: Dict[str, Any]) -> "TurnInput":
        cb = d.get("callback")
        return cls(
            instructions=d.get("instructions"),
            execution_id=d.get("executionId", ""),
            graph_id=d.get("graphId", ""),
            graph_name=d.get("graphName", ""),
            node_id=d.get("nodeId", ""),
            node_name=d.get("nodeName", ""),
            replica=int(d.get("replica") or 0),
            replicas=int(d.get("replicas") or 1),
            iteration=int(d.get("iteration") or 0),
            agent=d.get("agent", ""),
            prompt=d.get("prompt", ""),
            system=d.get("system", ""),
            inputs=[Message.from_json(m) for m in d.get("inputs") or []],
            operator_messages=list(d.get("operatorMessages") or []),
            neighbors=[NeighborNote.from_json(n) for n in d.get("neighbors") or []],
            outputs=[OutputInfo.from_json(o) for o in d.get("outputs") or []],
            tools=[ToolSchema.from_json(t) for t in d.get("tools") or []],
            model=d.get("model") or {},
            pending_stop=bool(d.get("pendingStop")),
            workdir=d.get("workdir", ""),
            branch=d.get("branch", ""),
            parameters=d.get("parameters") or {},
            config=d.get("config") or {},
            callback=Callback(cb.get("url", ""), cb.get("token", "")) if cb else None,
        )


SIGNAL_NONE = ""
SIGNAL_NO_FURTHER_WORK = "no_further_work"
SIGNAL_CONFIRM_STOP = "confirm_stop"


@dataclass
class TurnResult:
    emits: List[Emit] = field(default_factory=list)
    summary: str = ""
    broadcast: str = ""
    signal: str = SIGNAL_NONE
    usage: Usage = field(default_factory=Usage)
    tool_calls: int = 0
    retry: bool = False

    def to_json(self) -> Dict[str, Any]:
        d: Dict[str, Any] = {
            "emits": [e.to_json() for e in self.emits],
            "summary": self.summary,
            "usage": self.usage.to_json(),
            "toolCalls": self.tool_calls,
        }
        if self.broadcast:
            d["broadcast"] = self.broadcast
        if self.signal:
            d["signal"] = self.signal
        if self.retry:
            d["retry"] = True
        return d
