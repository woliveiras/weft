"""Planner: turns a feature request into tasks, researching open questions first.

Consumes ``feature.request`` and ``research.done``; emits
``research.question`` and ``task.ready``. The request/research cycle is the
graph's first loop: the plan waits in state until every question it raised
has come back.
"""
from __future__ import annotations

import json
import re
from typing import Any, Dict, List, Tuple

from weft import Agent, Message, TurnContext

from .common import payload, str_list, write_file

SCHEMA = """{"tasks": [{"id": "t1", "title": "...", "description": "...", "files": ["optional/paths"]}],
 "questions": [{"id": "q1", "question": "..."}]}"""


class Planner(Agent):
    name = "planner"

    def turn(self, ctx: TurnContext) -> str:
        done: List[str] = []
        for m in ctx.inputs:
            if m.type == "feature.request":
                done.append(self.plan(ctx, m))
            elif m.type == "research.done":
                done.append(self.research_done(ctx, payload(m)))
            else:
                done.append(f"ignored {m.type or 'untyped message'}")
        if not ctx.inputs:
            return "no input"
        return "; ".join(done)

    # -- feature.request -----------------------------------------------------------
    def plan(self, ctx: TurnContext, m: Message) -> str:
        p = payload(m)
        rid = str(p.get("id") or m.id)
        title = str(p.get("title") or "untitled feature")
        description = str(p.get("description") or p.get("text") or "")
        acceptance = str_list(p.get("acceptance"))
        ctx.output(f"planning {rid}: {title}")

        prompt = (
            f"{ctx.prompt}\n\nBreak this feature into small, independently implementable tasks. "
            "List open questions that need research before implementation (may be empty).\n\n"
            f"Title: {title}\nDescription: {description}\n"
            + (f"Acceptance criteria: {json.dumps(acceptance)}\n" if acceptance else "")
        )
        out = ctx.llm.json(prompt, schema_hint=SCHEMA, default={})
        tasks, questions = self.validate(out)
        if not tasks:
            ctx.output("model gave no usable plan; using heuristic split")
            tasks, questions = heuristic_plan(description or title)
        for i, t in enumerate(tasks, 1):
            t["task_id"] = f"{rid}-{t.get('id') or 't%d' % i}"

        write_file(ctx, f"plans/{rid}.md", render_plan(rid, title, description, acceptance, tasks, questions))

        if questions:
            qids = []
            for i, q in enumerate(questions, 1):
                qid = f"{rid}-{q.get('id') or 'q%d' % i}"
                qids.append(qid)
                ctx.emit("research.question", {"request_id": rid, "question_id": qid, "question": q["question"]})
            ctx.state.set_json(f"plan:{rid}", {"request_id": rid, "title": title, "tasks": tasks,
                                               "pending": qids, "research": []})
            return f"{rid}: {len(tasks)} tasks waiting on {len(qids)} research questions"
        self.emit_tasks(ctx, rid, tasks, [])
        return f"{rid}: emitted {len(tasks)} tasks"

    @staticmethod
    def validate(out: Any) -> Tuple[List[Dict[str, Any]], List[Dict[str, Any]]]:
        if not isinstance(out, dict):
            return [], []
        tasks = [t for t in out.get("tasks") or [] if isinstance(t, dict) and t.get("title")]
        tasks = [{"id": str(t.get("id") or ""), "title": str(t["title"]),
                  "description": str(t.get("description") or t["title"]),
                  "files": str_list(t.get("files"))} for t in tasks]
        qs = [q for q in out.get("questions") or [] if isinstance(q, dict) and q.get("question")]
        qs = [{"id": str(q.get("id") or ""), "question": str(q["question"])} for q in qs]
        return tasks, qs

    # -- research.done -------------------------------------------------------------
    def research_done(self, ctx: TurnContext, p: Dict[str, Any]) -> str:
        rid, qid = str(p.get("request_id") or ""), str(p.get("question_id") or "")
        plan = ctx.state.get_json(f"plan:{rid}")
        if not plan or plan.get("done"):
            return f"research {qid} for unknown or finished plan {rid}"
        if qid in plan["pending"]:
            plan["pending"].remove(qid)
        if p.get("findings_path"):
            plan["research"].append(str(p["findings_path"]))
        if plan["pending"]:
            ctx.state.set_json(f"plan:{rid}", plan)
            return f"{rid}: still waiting on {len(plan['pending'])} questions"
        self.emit_tasks(ctx, rid, plan["tasks"], plan["research"])
        ctx.state.set_json(f"plan:{rid}", {"request_id": rid, "done": True})
        return f"{rid}: research complete, emitted {len(plan['tasks'])} tasks"

    @staticmethod
    def emit_tasks(ctx: TurnContext, rid: str, tasks: List[Dict[str, Any]], research: List[str]) -> None:
        for t in tasks:
            ctx.emit("task.ready", {"request_id": rid, "task_id": t["task_id"], "title": t["title"],
                                    "description": t["description"], "files": t.get("files", []),
                                    "research": research})


def heuristic_plan(text: str) -> Tuple[List[Dict[str, Any]], List[Dict[str, Any]]]:
    """Bullets become tasks; otherwise sentences. Sentences ending in '?' become questions."""
    lines = [l.strip() for l in text.splitlines()]
    bullets = [re.sub(r"^([-*•]|\d+[.)])\s*", "", l) for l in lines if re.match(r"^([-*•]|\d+[.)])\s+", l)]
    parts = bullets or [s.strip() for s in re.split(r"(?<=[.!?])\s+", text.replace("\n", " ")) if s.strip()]
    tasks, questions = [], []
    for part in parts:
        if part.endswith("?"):
            questions.append({"id": f"q{len(questions) + 1}", "question": part})
        else:
            title = part.rstrip(".!")[:80]
            tasks.append({"id": f"t{len(tasks) + 1}", "title": title, "description": part, "files": []})
    if not tasks:
        tasks = [{"id": "t1", "title": (text.strip() or "implement feature")[:80],
                  "description": text.strip() or "implement feature", "files": []}]
    return tasks, questions


def render_plan(rid, title, description, acceptance, tasks, questions) -> str:
    out = [f"# Plan {rid}: {title}", "", description, ""]
    if acceptance:
        out += ["## Acceptance", ""] + [f"- {a}" for a in acceptance] + [""]
    out += ["## Tasks", ""] + [f"- [ ] `{t['task_id']}` {t['title']}" for t in tasks] + [""]
    if questions:
        out += ["## Open questions", ""] + [f"- {q['question']}" for q in questions] + [""]
    return "\n".join(out)
