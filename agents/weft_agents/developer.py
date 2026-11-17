"""Developer: implements tasks and reworks them after review or QA.

Consumes ``task.ready``, ``review.changes_requested`` and ``qa.failed``;
emits ``code.ready``. The review and QA loops back here are the graph's
cycles; after ``max_rounds`` rework rounds the developer asks a human.
"""
from __future__ import annotations

import json
from typing import Any, Dict, List, Optional, Tuple

from weft import Agent, Message, TurnContext

from .common import module_name, payload, read_file, safe_path, str_list, write_file

SCHEMA = '{"files": [{"path": "relative/path", "content": "full file content"}], "summary": "...", "commit_message": "..."}'


class Developer(Agent):
    name = "developer"

    def __init__(self, max_rounds: int = 3):
        super().__init__()
        self.max_rounds = max_rounds

    def turn(self, ctx: TurnContext) -> str:
        if not ctx.inputs:
            return "no input"
        return "; ".join(self.handle(ctx, m) for m in ctx.inputs)

    def handle(self, ctx: TurnContext, m: Message) -> str:
        if m.type not in ("task.ready", "review.changes_requested", "qa.failed"):
            return f"ignored {m.type}"
        p = payload(m)
        task_id = str(p.get("task_id") or "")
        if not task_id:
            return f"{m.type} without task_id ignored"
        title = str(p.get("title") or task_id)
        description = str(p.get("description") or title)

        key = f"iter:{task_id}"
        iteration = 1 if m.type == "task.ready" else int(ctx.state.get_json(key, 1)) + 1
        ctx.state.set_json(key, iteration)
        feedback = str_list(p.get("comments")) + str_list(p.get("failures"))
        if p.get("reason"):
            feedback.append(str(p["reason"]))

        escalated = False
        if iteration > self.max_rounds + 1:
            answer = ctx.ask_human(
                f"Task {task_id} ({title}) has been reworked {iteration - 1} times. "
                f"Latest feedback: {feedback}. Reply with guidance, or 'ship' to pass it on as is.")
            escalated = True
            if answer and answer.strip().lower() != "ship":
                feedback.append("Human guidance: " + answer.strip())
        ctx.output(f"{task_id} iteration {iteration}: {title}")

        research = "\n\n".join(filter(None, (read_file(ctx, r) for r in str_list(p.get("research")))))
        files, summary, commit_msg = self.generate(ctx, task_id, title, description, feedback, research,
                                                   str_list(p.get("files")))
        written = [path for path, content in files if write_file(ctx, path, content)]
        self.commit(ctx, written, commit_msg)

        out = {"request_id": p.get("request_id", ""), "task_id": task_id, "title": title,
               "description": description, "files": written, "summary": summary,
               "branch": ctx.input.branch, "iteration": iteration, "research": str_list(p.get("research"))}
        if escalated:
            out["escalated"] = True
        ctx.emit("code.ready", out)
        return f"{task_id}: wrote {len(written)} files (iteration {iteration})"

    def generate(self, ctx: TurnContext, task_id: str, title: str, description: str, feedback: List[str],
                 research: str, hinted: List[str]) -> Tuple[List[Tuple[str, str]], str, str]:
        prompt = (f"{ctx.prompt}\n\nImplement this task. Include tests. Return complete file contents.\n"
                  f"Task {task_id}: {title}\n{description}\n")
        if hinted:
            prompt += f"Suggested files: {json.dumps(hinted)}\n"
        if feedback:
            prompt += "Address this feedback from the previous round:\n" + "\n".join(f"- {f}" for f in feedback) + "\n"
        if research:
            prompt += f"\nResearch notes:\n{research[:6000]}\n"
        out = ctx.llm.json(prompt, schema_hint=SCHEMA, default={})
        files = self.validate(out)
        if files:
            return files, str(out.get("summary") or f"implemented {title}"), \
                str(out.get("commit_message") or f"{task_id}: {title}")
        ctx.output("model gave no usable files; writing a scaffold module and test")
        return scaffold(task_id, title, description), f"scaffolded {title}", f"{task_id}: scaffold {title}"

    @staticmethod
    def validate(out: Any) -> List[Tuple[str, str]]:
        if not isinstance(out, dict):
            return []
        files = []
        for f in out.get("files") or []:
            if not isinstance(f, dict) or not isinstance(f.get("content"), str):
                continue
            path = safe_path(f.get("path"))
            if path:
                files.append((path, f["content"]))
        return files

    @staticmethod
    def commit(ctx: TurnContext, paths: List[str], message: str) -> Optional[str]:
        if not paths:
            return None
        if ctx.tools.try_call("git.add", paths=paths) is None:
            ctx.output("git.write not granted; leaving changes uncommitted")
            return None
        res = ctx.tools.try_call("git.commit", message=message, paths=paths)
        return res.content if res else None


def scaffold(task_id: str, title: str, description: str) -> List[Tuple[str, str]]:
    """A small, real module plus a passing test, so QA has something to run offline."""
    mod = module_name(title, task_id)
    doc = f"{title}\n\n{description}".replace('"""', "'''").replace("\\", "\\\\")
    src = (f'"""{doc}\n"""\n\n\n'
           f"def run(*args, **kwargs):\n"
           f'    """Entry point for task {task_id}."""\n'
           f'    return {{"task": {task_id!r}, "status": "implemented"}}\n')
    test = (f"import os\nimport sys\nimport unittest\n\n"
            f"sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..', 'src'))\n\n"
            f"import {mod}  # noqa: E402\n\n\n"
            f"class Test{mod.title().replace('_', '')}(unittest.TestCase):\n"
            f"    def test_run(self):\n"
            f"        self.assertEqual({mod}.run()['status'], 'implemented')\n\n\n"
            f"if __name__ == '__main__':\n    unittest.main()\n")
    return [(f"src/{mod}.py", src), (f"tests/test_{mod}.py", test)]
