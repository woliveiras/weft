"""HTTP client for the per-execution callback API (Runtime Protocol v1, section 2).

Every request carries ``Authorization: Bearer <callback.token>``. The token is
scoped to one execution and stops working when the turn ends.
"""
from __future__ import annotations

import json
import time
import urllib.error
import urllib.parse
import urllib.request
from typing import Any, Dict, List, Optional


class CallbackError(Exception):
    """A callback request failed."""

    def __init__(self, message: str, status: int = 0, body: str = ""):
        super().__init__(message)
        self.status = status
        self.body = body


class PermissionDenied(CallbackError):
    """The gateway refused a tool call (HTTP 403): the node lacks the grant."""


class NotFound(CallbackError):
    """HTTP 404 (e.g. a state key that was never written)."""


class CallbackClient:
    def __init__(self, url: str, token: str, timeout: float = 120.0, retries: int = 3,
                 backoff: float = 0.5, ask_timeout: float = 24 * 3600.0):
        self.url = url.rstrip("/")
        self.token = token
        self.timeout = timeout
        self.retries = retries
        self.backoff = backoff
        self.ask_timeout = ask_timeout

    # -- low level ---------------------------------------------------------
    def request(self, method: str, path: str, body: Any = None, *, raw: Optional[bytes] = None,
                idempotent: bool = False, timeout: Optional[float] = None) -> bytes:
        data: Optional[bytes]
        headers = {"Authorization": f"Bearer {self.token}", "Accept": "application/json"}
        if raw is not None:
            data = raw
            headers["Content-Type"] = "application/octet-stream"
        elif body is not None:
            data = json.dumps(body).encode()
            headers["Content-Type"] = "application/json"
        else:
            data = None
        attempts = self.retries if idempotent else 1
        last: Optional[Exception] = None
        for attempt in range(max(1, attempts)):
            req = urllib.request.Request(self.url + path, data=data, headers=headers, method=method)
            try:
                with urllib.request.urlopen(req, timeout=timeout or self.timeout) as resp:
                    return resp.read()
            except urllib.error.HTTPError as e:
                text = e.read().decode("utf-8", "replace")
                msg = _error_message(text) or f"HTTP {e.code}"
                if e.code == 403:
                    raise PermissionDenied(msg, e.code, text) from None
                if e.code == 404:
                    raise NotFound(msg, e.code, text) from None
                if e.code >= 500 and attempt + 1 < attempts:
                    last = CallbackError(msg, e.code, text)
                    time.sleep(self.backoff * (2 ** attempt))
                    continue
                raise CallbackError(f"{method} {path}: {msg}", e.code, text) from None
            except (urllib.error.URLError, TimeoutError, ConnectionError) as e:
                last = e
                if attempt + 1 < attempts:
                    time.sleep(self.backoff * (2 ** attempt))
                    continue
                raise CallbackError(f"{method} {path}: {e}") from None
        raise CallbackError(f"{method} {path}: {last}")

    def _json(self, method: str, path: str, body: Any = None, **kw: Any) -> Any:
        out = self.request(method, path, body, **kw)
        if not out:
            return None
        return json.loads(out)

    # -- protocol endpoints -------------------------------------------------
    def invoke_tool(self, name: str, args: Dict[str, Any]) -> Dict[str, Any]:
        return self._json("POST", "/tools/" + urllib.parse.quote(name, safe="._-"), args or {}) or {}

    def list_tools(self) -> List[Dict[str, Any]]:
        return self._json("GET", "/tools", idempotent=True) or []

    def chat(self, request: Dict[str, Any]) -> Dict[str, Any]:
        return self._json("POST", "/llm/chat", request) or {}

    def emit(self, emit: Dict[str, Any]) -> Dict[str, Any]:
        return self._json("POST", "/emit", emit) or {}

    def state_get(self, key: str) -> Optional[bytes]:
        try:
            return self.request("GET", "/state/" + urllib.parse.quote(key, safe=""), idempotent=True)
        except NotFound:
            return None

    def state_put(self, key: str, value: bytes) -> None:
        self.request("PUT", "/state/" + urllib.parse.quote(key, safe=""), raw=value, idempotent=True)

    def output(self, text: str) -> None:
        self.request("POST", "/output", {"text": text})

    def ask(self, question: str) -> str:
        res = self._json("POST", "/ask", {"question": question}, timeout=self.ask_timeout) or {}
        return str(res.get("answer", ""))


def _error_message(text: str) -> str:
    try:
        d = json.loads(text)
        if isinstance(d, dict):
            return str(d.get("error") or d.get("message") or "")
    except ValueError:
        pass
    return text.strip()[:500]
