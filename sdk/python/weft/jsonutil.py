"""Robust JSON extraction from model output."""
from __future__ import annotations

import json
import re
from typing import Any, Optional

_FENCE = re.compile(r"```(?:json|JSON)?\s*\n?(.*?)```", re.DOTALL)


def extract_json(text: str) -> Optional[Any]:
    """Return the first JSON object (or array) found in ``text``, else None.

    Tries, in order: the whole text, fenced code blocks, then the first
    balanced ``{...}`` (or ``[...]``) span, scanning with string awareness so
    braces inside strings do not confuse it.
    """
    if not text:
        return None
    candidates = [text.strip()]
    candidates += [m.group(1).strip() for m in _FENCE.finditer(text)]
    for c in candidates:
        try:
            return json.loads(c)
        except ValueError:
            pass
    for opener in ("{", "["):
        found = _balanced(text, opener)
        if found is not None:
            return found
    return None


def _balanced(text: str, opener: str) -> Optional[Any]:
    closer = "}" if opener == "{" else "]"
    start = text.find(opener)
    while start != -1:
        depth, in_str, esc = 0, False, False
        for i in range(start, len(text)):
            ch = text[i]
            if in_str:
                if esc:
                    esc = False
                elif ch == "\\":
                    esc = True
                elif ch == '"':
                    in_str = False
                continue
            if ch == '"':
                in_str = True
            elif ch == opener:
                depth += 1
            elif ch == closer:
                depth -= 1
                if depth == 0:
                    try:
                        return json.loads(text[start:i + 1])
                    except ValueError:
                        break
        start = text.find(opener, start + 1)
    return None
