"""Runtime Protocol v1 HTTP server.

    GET  /v1/health  -> {"status": "ok", "agents": [...], "framework": "..."}
    POST /v1/turn    TurnInput -> TurnResult

Turns run concurrently (one thread per request). An agent that raises
produces HTTP 500 with ``{"error": ...}``; the engine then nacks the inputs
and they are redelivered with backoff.
"""
from __future__ import annotations

import hmac
import json
import logging
import os
import sys
import time
import traceback
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Dict, Iterable, Mapping, Optional, Union

from . import __version__
from .agent import Agent
from .callback import CallbackClient
from .context import Backend, TurnContext
from .types import TurnInput

log = logging.getLogger("weft.server")

MAX_BODY = int(os.environ.get("WEFT_MAX_BODY", str(32 * 1024 * 1024)))
SECRET_HEADER = "X-Weft-Runtime-Secret"


class JSONFormatter(logging.Formatter):
    def format(self, record: logging.LogRecord) -> str:
        d = {
            "time": time.strftime("%Y-%m-%dT%H:%M:%S", time.gmtime(record.created)) + "Z",
            "level": record.levelname.lower(),
            "logger": record.name,
            "msg": record.getMessage(),
        }
        for k in ("agent", "execution", "graph", "node", "replica", "duration_ms", "status"):
            if hasattr(record, k):
                d[k] = getattr(record, k)
        if record.exc_info:
            d["exc"] = self.formatException(record.exc_info)
        return json.dumps(d)


def setup_logging(level: Optional[str] = None) -> None:
    root = logging.getLogger()
    if any(getattr(h, "_weft", False) for h in root.handlers):
        return
    h = logging.StreamHandler(sys.stdout)
    h.setFormatter(JSONFormatter())
    h._weft = True  # type: ignore[attr-defined]
    root.addHandler(h)
    root.setLevel((level or os.environ.get("WEFT_LOG_LEVEL", "INFO")).upper())


def _agent_map(agents: Union[Mapping[str, Agent], Iterable[Agent]]) -> Dict[str, Agent]:
    if isinstance(agents, Mapping):
        return dict(agents)
    return {a.name: a for a in agents}


class RuntimeApp:
    """Transport-independent request handling, so it can be tested directly."""

    def __init__(self, agents: Union[Mapping[str, Agent], Iterable[Agent]], secret: Optional[str] = None,
                 backend_factory=None):
        self.agents = _agent_map(agents)
        self.secret = secret if secret is not None else os.environ.get("WEFT_RUNTIME_SECRET", "")
        self.backend_factory = backend_factory

    def health(self) -> dict:
        frameworks = sorted({a.framework for a in self.agents.values()})
        return {"status": "ok", "agents": sorted(self.agents), "framework": ",".join(frameworks),
                "sdk": "weft-python/" + __version__}

    def authorised(self, header_value: Optional[str]) -> bool:
        if not self.secret:
            return True
        return bool(header_value) and hmac.compare_digest(header_value, self.secret)

    def turn(self, body: dict) -> tuple:
        ti = TurnInput.from_json(body)
        name = ti.agent or ti.node_id
        agent = self.agents.get(name)
        if agent is None and len(self.agents) == 1 and not ti.agent:
            agent = next(iter(self.agents.values()))
        if agent is None:
            return 404, {"error": f"unknown agent {name!r}; serving {sorted(self.agents)}"}
        backend: Backend
        if self.backend_factory is not None:
            backend = self.backend_factory(ti)
        elif ti.callback is not None and ti.callback.url:
            backend = CallbackClient(ti.callback.url, ti.callback.token)
        else:
            return 400, {"error": "turn input has no callback"}
        ctx = TurnContext(ti, backend)
        extra = {"agent": agent.name, "execution": ti.execution_id, "graph": ti.graph_id,
                 "node": ti.node_id, "replica": ti.replica}
        start = time.monotonic()
        try:
            res = agent.run_turn(ctx)
        except Exception as e:
            log.error("turn failed: %s", e, exc_info=True,
                      extra={**extra, "duration_ms": int((time.monotonic() - start) * 1000), "status": "failed"})
            return 500, {"error": f"{type(e).__name__}: {e}", "trace": traceback.format_exc(limit=5)}
        log.info("turn done: %s", res.summary[:200], extra={**extra, "status": "ok",
                                                           "duration_ms": int((time.monotonic() - start) * 1000)})
        return 200, res.to_json()


def make_handler(app: RuntimeApp):
    class Handler(BaseHTTPRequestHandler):
        server_version = "weft-python/" + __version__
        protocol_version = "HTTP/1.1"

        def log_message(self, fmt: str, *args) -> None:  # silence default stderr access log
            log.debug("%s - " + fmt, self.address_string(), *args)

        def _send(self, code: int, obj: dict) -> None:
            body = json.dumps(obj).encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self) -> None:
            if self.path.rstrip("/") in ("/v1/health", "/healthz"):
                self._send(200, app.health())
            else:
                self._send(404, {"error": "not found"})

        def do_POST(self) -> None:
            if self.path.rstrip("/") != "/v1/turn":
                self._send(404, {"error": "not found"})
                return
            if not app.authorised(self.headers.get(SECRET_HEADER)):
                self._send(401, {"error": "missing or invalid runtime secret"})
                return
            try:
                length = int(self.headers.get("Content-Length") or 0)
            except ValueError:
                length = -1
            if length < 0 or length > MAX_BODY:
                self._send(413, {"error": f"body too large (max {MAX_BODY} bytes)"})
                return
            try:
                body = json.loads(self.rfile.read(length) or b"{}")
            except ValueError as e:
                self._send(400, {"error": f"invalid JSON: {e}"})
                return
            code, out = app.turn(body)
            self._send(code, out)

    return Handler


def make_server(agents, host: str = "0.0.0.0", port: int = 8000, secret: Optional[str] = None) -> ThreadingHTTPServer:
    app = RuntimeApp(agents, secret=secret)
    srv = ThreadingHTTPServer((host, port), make_handler(app))
    srv.daemon_threads = True
    return srv


def serve(agents, host: str = "0.0.0.0", port: int = 8000, secret: Optional[str] = None) -> None:
    """Serve agents over Runtime Protocol v1 until interrupted."""
    setup_logging()
    srv = make_server(agents, host, port, secret)
    log.info("weft runtime listening on %s:%d agents=%s", host, port, sorted(_agent_map(agents)))
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        srv.server_close()
