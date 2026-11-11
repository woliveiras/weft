"""In-memory harness for unit testing agents without a platform.

    from weft.testing import FakeBackend, run_agent, message

    fb = FakeBackend(grants=["filesystem.*"], llm=['{"tasks": []}'])
    result = run_agent(MyAgent(), fb, inputs=[message("feature.request", {...})])
    assert result.emits[0].type == "task.ready"
    assert "plans/x.md" in fb.files

FakeBackend implements the callback API: a dict filesystem behind the
``filesystem.*`` tools, recorded ``git.*`` calls, scripted ``shell.exec``
results, scripted LLM replies, a KV state, operator answers - and grant
enforcement that raises PermissionDenied exactly like the gateway's 403.
"""
from __future__ import annotations

import fnmatch
import json
from typing import Any, Callable, Dict, Iterable, List, Optional, Union

from .agent import Agent
from .callback import PermissionDenied
from .context import TurnContext
from .types import Message, ToolSchema, TurnInput, TurnResult

LLMScript = Union[None, List[Any], Callable[[Dict[str, Any]], Any]]


def mock_reply(request: Dict[str, Any]) -> str:
    """Mirror the server's ``mock`` provider so fallbacks get exercised."""
    msgs = request.get("messages") or []
    last = msgs[-1]["content"] if msgs else ""
    if request.get("json"):
        return json.dumps({"mock": True, "summary": last[:200]})
    return "MOCK: " + last[:200]


def message(type: str, payload: Any = None, id: str = "", source: str = "test", **metadata: Any) -> Message:
    mid = id or f"msg_{type.replace('.', '_')}"
    md = {"correlation_id": mid}
    md.update(metadata)
    return Message(id=mid, type=type, source=source, payload=payload if payload is not None else {}, metadata=md)


class FakeBackend:
    def __init__(self, grants: Iterable[str] = ("*",), llm: LLMScript = None,
                 files: Optional[Dict[str, str]] = None, shell: Optional[Callable[[Dict[str, Any]], Dict[str, Any]]] = None,
                 answers: Optional[List[str]] = None, state: Optional[Dict[str, bytes]] = None):
        self.grants = list(grants)
        self.llm_script = llm
        self.files: Dict[str, str] = dict(files or {})
        self.shell = shell
        self.answers = list(answers or [])
        self.state: Dict[str, bytes] = dict(state or {})
        self.outputs: List[str] = []
        self.emitted: List[Dict[str, Any]] = []
        self.tool_calls: List[tuple] = []
        self.chat_requests: List[Dict[str, Any]] = []
        self.questions: List[str] = []

    # -- grants ----------------------------------------------------------------
    CAPABILITY = {
        "filesystem.list": "filesystem.read", "filesystem.search": "filesystem.read",
        "filesystem.stat": "filesystem.read", "filesystem.edit": "filesystem.write",
        "filesystem.move": "filesystem.write", "filesystem.mkdir": "filesystem.write",
        "git.status": "git.read", "git.diff": "git.read", "git.log": "git.read", "git.show": "git.read",
        "git.add": "git.write", "git.commit": "git.write", "git.branch": "git.write",
        "git.checkout": "git.write", "git.merge": "git.write",
    }

    def allowed(self, name: str) -> bool:
        cap = self.CAPABILITY.get(name, name)
        return any(fnmatch.fnmatchcase(name, g) or fnmatch.fnmatchcase(cap, g) for g in self.grants)

    # -- Backend protocol ----------------------------------------------------------
    def invoke_tool(self, name: str, args: Dict[str, Any]) -> Dict[str, Any]:
        self.tool_calls.append((name, args))
        if not self.allowed(name):
            raise PermissionDenied(f"tool {name} not granted", 403)
        if name == "filesystem.read":
            p = args.get("path", "")
            if p not in self.files:
                return {"content": f"no such file: {p}", "isError": True}
            return {"content": self.files[p]}
        if name == "filesystem.write":
            p = args["path"]
            self.files[p] = (self.files.get(p, "") if args.get("append") else "") + args.get("content", "")
            return {"content": f"wrote {len(args.get('content', ''))} bytes to {p}"}
        if name == "filesystem.list":
            prefix = (args.get("path") or "").strip("/")
            names = sorted(f for f in self.files if not prefix or f.startswith(prefix + "/"))
            return {"content": "\n".join(names)}
        if name == "shell.exec":
            if self.shell:
                return self.shell(args)
            return {"content": "exit code 0", "data": {"exitCode": 0, "stdout": "", "stderr": ""}}
        if name.startswith("git."):
            return {"content": f"{name} ok"}
        if name == "http.fetch":
            return {"content": f"fetched {args.get('url')}"}
        return {"content": f"{name} ok"}

    def list_tools(self) -> List[Dict[str, Any]]:
        return [{"name": g, "description": "", "parameters": {}} for g in self.grants]

    def chat(self, request: Dict[str, Any]) -> Dict[str, Any]:
        self.chat_requests.append(request)
        script = self.llm_script
        if callable(script):
            reply = script(request)
        elif isinstance(script, list) and script:
            reply = script.pop(0)
        else:
            reply = mock_reply(request)
        if isinstance(reply, (dict, list)):
            reply = json.dumps(reply)
        return {"message": {"role": "assistant", "content": reply}, "stopReason": "stop", "model": "fake",
                "usage": {"inputTokens": 10, "outputTokens": 5}}

    def emit(self, emit: Dict[str, Any]) -> Dict[str, Any]:
        self.emitted.append(emit)
        return {"queued": True}

    def state_get(self, key: str) -> Optional[bytes]:
        return self.state.get(key)

    def state_put(self, key: str, value: bytes) -> None:
        self.state[key] = value

    def output(self, text: str) -> None:
        self.outputs.append(text)

    def ask(self, question: str) -> str:
        self.questions.append(question)
        return self.answers.pop(0) if self.answers else "proceed"


def make_input(inputs: Iterable[Message] = (), agent: str = "", grants: Iterable[str] = (), **kw: Any) -> TurnInput:
    ti = TurnInput(agent=agent, node_id=agent or "agent", execution_id="exec_test",
                   graph_id="test", inputs=list(inputs), **kw)
    ti.tools = [ToolSchema(name=g) for g in grants]
    return ti


def fake_context(backend: Optional[FakeBackend] = None, inputs: Iterable[Message] = (), agent: str = "",
                 **kw: Any) -> TurnContext:
    backend = backend or FakeBackend()
    return TurnContext(make_input(inputs, agent=agent, grants=backend.grants, **kw), backend)


def run_agent(agent: Agent, backend: Optional[FakeBackend] = None, inputs: Iterable[Message] = (),
              **kw: Any) -> TurnResult:
    """Run one turn of ``agent`` against a FakeBackend and return the result."""
    ctx = fake_context(backend, inputs, agent=agent.name, **kw)
    return agent.run_turn(ctx)


FakeContext = fake_context
