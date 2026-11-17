"""QA: runs the test suite on approved work.

Consumes ``review.approved``; emits ``qa.passed`` (towards the release
gate) or ``qa.failed`` (back to the developer). Tests run through the
``shell.exec`` gateway tool, so the node's command allow-list and sandbox
(process or container) apply.
"""
from __future__ import annotations

import re
import shlex
from typing import Any, Optional, Tuple

from weft import Agent, PermissionDenied, TurnContext

from .common import payload

DEFAULT_TEST_COMMAND = "python -m unittest discover -s tests -v"


class QA(Agent):
    name = "qa"

    def turn(self, ctx: TurnContext) -> str:
        if not ctx.inputs:
            return "no input"
        done = []
        for m in ctx.inputs:
            if m.type != "review.approved":
                done.append(f"ignored {m.type}")
                continue
            item = payload(m)
            cmd = str(ctx.config.get("test_command") or DEFAULT_TEST_COMMAND)
            timeout = int(ctx.config.get("test_timeout_sec") or 600)
            ctx.output(f"{item.get('task_id')}: running {cmd}")
            try:
                res = ctx.tools.call("shell.exec", argv=shlex.split(cmd), timeoutSec=timeout)
            except PermissionDenied:
                ctx.emit("qa.failed", dict(item, reason="shell.exec not granted",
                                           failures=["shell.exec not granted"]))
                done.append(f"{item.get('task_id')}: cannot run tests (shell.exec not granted)")
                continue
            passed, code, report = interpret(res.content, res.data, res.is_error)
            if passed:
                ctx.emit("qa.passed", dict(item, report=report, exit_code=code))
            else:
                ctx.emit("qa.failed", dict(item, report=report, exit_code=code, failures=failures(report)))
            done.append(f"{item.get('task_id')}: {'passed' if passed else 'failed'}")
        return "; ".join(done)


def interpret(content: str, data: Any, is_error: bool) -> Tuple[bool, Optional[int], str]:
    """Work out pass/fail from a shell.exec result, whatever its exact shape."""
    code: Optional[int] = None
    report = content or ""
    if isinstance(data, dict):
        for k in ("exitCode", "exit_code", "code"):
            if isinstance(data.get(k), int):
                code = data[k]
                break
        streams = [str(data.get(k) or "") for k in ("stdout", "stderr")]
        if any(streams):
            report = "\n".join(s for s in streams if s)
    if code is None:
        m = re.search(r"exit(?:\s*code|Code)?\W+(\d+)", content or "", re.IGNORECASE)
        if m:
            code = int(m.group(1))
    if code is not None:
        passed = code == 0
    else:
        passed = not is_error and "FAILED" not in report and "Traceback" not in report
    return passed, code, report[-8000:]


def failures(report: str) -> list:
    lines = [l for l in report.splitlines() if l.startswith(("FAIL:", "ERROR:")) or "Error" in l]
    return lines[:20] or [report.strip()[-500:] or "tests failed"]
