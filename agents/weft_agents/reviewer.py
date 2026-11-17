"""Reviewer: approves code or sends it back. The LangGraph showcase.

Consumes ``code.ready``; emits ``review.approved`` or
``review.changes_requested``. The graph routes them with ``when`` (dynamic
branching) - approved work goes on to QA, the rest cycles back to the
developer.

When ``langgraph`` is importable the review runs as a two-node StateGraph
(analyse -> decide) through :class:`weft.adapters.LangGraphAgent`;
otherwise the same two steps run as plain functions. Either way model and
tool calls go through the platform gateways.
"""
from __future__ import annotations

from typing import Any, Dict, List, Optional, TypedDict

from weft import Agent, TurnContext
from weft.adapters import available

from .common import payload, read_file, str_list

SCHEMA = '{"verdict": "approved" | "changes_requested", "comments": ["..."]}'


class ReviewState(TypedDict, total=False):
    item: Dict[str, Any]
    files: Dict[str, Optional[str]]
    model_verdict: Any
    verdict: str
    comments: List[str]


def analyse(ctx: TurnContext, state: ReviewState) -> ReviewState:
    item = state["item"]
    files = {path: read_file(ctx, path) for path in str_list(item.get("files"))}
    listing = "\n\n".join(f"--- {p}\n{(c or '<unreadable>')[:4000]}" for p, c in files.items())
    out = ctx.llm.json(
        f"{ctx.prompt}\n\nReview this change for task {item.get('task_id')}: {item.get('title')}.\n"
        f"{item.get('description', '')}\n\n{listing}",
        schema_hint=SCHEMA, default={})
    return {"files": files, "model_verdict": out}


def decide(state: ReviewState) -> ReviewState:
    files = state.get("files") or {}
    problems = [f"{p} is empty or unreadable" for p, c in files.items() if not c or not c.strip()]
    problems += [f"{p} contains TODO" for p, c in files.items() if c and "TODO" in c]
    if not files:
        problems.append("no files were changed")
    mv = state.get("model_verdict")
    if isinstance(mv, dict) and mv.get("verdict") in ("approved", "changes_requested"):
        verdict = mv["verdict"]
        comments = str_list(mv.get("comments"))
        if problems:  # objective problems always win over an optimistic model
            verdict, comments = "changes_requested", comments + problems
    else:
        verdict = "changes_requested" if problems else "approved"
        comments = problems or ["heuristic review: files present, non-empty, no TODOs"]
    return {"verdict": verdict, "comments": comments}


def build_graph(ctx: TurnContext):
    from langgraph.graph import END, StateGraph

    g = StateGraph(ReviewState)
    g.add_node("analyse", lambda s: analyse(ctx, s))
    g.add_node("decide", decide)
    g.set_entry_point("analyse")
    g.add_edge("analyse", "decide")
    g.add_edge("decide", END)
    return g.compile()


def review_plain(ctx: TurnContext, item: Dict[str, Any]) -> ReviewState:
    state: ReviewState = {"item": item}
    state.update(analyse(ctx, state))
    state.update(decide(state))
    return state


class Reviewer(Agent):
    name = "reviewer"

    def __init__(self, use_langgraph: Optional[bool] = None):
        super().__init__()
        self.use_langgraph = available("langgraph") if use_langgraph is None else use_langgraph
        self.framework = "langgraph" if self.use_langgraph else "custom"

    def turn(self, ctx: TurnContext) -> str:
        if not ctx.inputs:
            return "no input"
        done = []
        for m in ctx.inputs:
            if m.type != "code.ready":
                done.append(f"ignored {m.type}")
                continue
            item = payload(m)
            if self.use_langgraph:
                state = build_graph(ctx).invoke({"item": item})
            else:
                state = review_plain(ctx, item)
            verdict = state.get("verdict", "changes_requested")
            comments = state.get("comments", [])
            ctx.output(f"{item.get('task_id')}: {verdict} ({'; '.join(comments)[:200]})")
            out = dict(item, verdict=verdict, comments=comments)
            ctx.emit("review.approved" if verdict == "approved" else "review.changes_requested", out)
            done.append(f"{item.get('task_id')}: {verdict}")
        return "; ".join(done)
