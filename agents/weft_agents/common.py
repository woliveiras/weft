"""Helpers shared by the example agents."""
from __future__ import annotations

import hashlib
import json
import posixpath
import re
from typing import Any, Dict, List, Optional

from weft import Message, PermissionDenied, TurnContext


def payload(m: Message) -> Dict[str, Any]:
    """The payload as a dict, whatever arrived (agents must not crash on junk)."""
    p = m.payload
    if isinstance(p, dict):
        return p
    if isinstance(p, str):
        try:
            v = json.loads(p)
            if isinstance(v, dict):
                return v
        except ValueError:
            pass
        return {"text": p}
    return {"value": p}


def slugify(text: str, max_len: int = 40) -> str:
    s = re.sub(r"[^a-z0-9]+", "_", (text or "").lower()).strip("_")[:max_len].strip("_")
    return s or "item"


def module_name(title: str, key: str) -> str:
    """A valid, stable Python module name for a task."""
    h = hashlib.sha1(key.encode()).hexdigest()[:6]
    s = f"{slugify(title, 30)}_{h}"
    return s if s[0].isalpha() else "m_" + s


def safe_path(p: Any) -> Optional[str]:
    """Normalise a model-proposed relative path; None if it escapes the workspace."""
    if not isinstance(p, str) or not p.strip():
        return None
    p = posixpath.normpath(p.strip().lstrip("/").replace("\\", "/"))
    if p.startswith("..") or p == "." or p.startswith(".git/") or p == ".git":
        return None
    return p


def write_file(ctx: TurnContext, path: str, content: str) -> bool:
    res = ctx.tools.try_call("filesystem.write", path=path, content=content)
    if res is None:
        ctx.output(f"filesystem.write not granted; could not write {path}")
        return False
    if res.is_error:
        ctx.output(f"write {path} failed: {res.content}")
        return False
    return True


def read_file(ctx: TurnContext, path: str) -> Optional[str]:
    try:
        res = ctx.tools.call("filesystem.read", path=path)
    except PermissionDenied:
        return None
    if res.is_error:
        return None
    if isinstance(res.data, dict) and isinstance(res.data.get("content"), str):
        return res.data["content"]
    return res.content


def is_mock(text: str) -> bool:
    return not text or text.startswith("MOCK:") or '"mock": true' in text or '"mock":true' in text


def str_list(v: Any) -> List[str]:
    if isinstance(v, list):
        return [str(x) for x in v if x is not None and str(x).strip()]
    if isinstance(v, str) and v.strip():
        return [v]
    return []
