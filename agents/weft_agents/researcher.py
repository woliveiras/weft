"""Researcher: answers ``research.question`` and reports ``research.done``.

Uses the model, plus ``http.fetch`` when the node is granted it (a URL in
the question, or ``config.search_url`` with ``{q}``). Findings land in
``research/<question_id>.md`` so the developer can read them later.
"""
from __future__ import annotations

import re
import urllib.parse

from weft import Agent, PermissionDenied, TurnContext

from .common import is_mock, payload, write_file


class Researcher(Agent):
    name = "researcher"

    def turn(self, ctx: TurnContext) -> str:
        if not ctx.inputs:
            return "no input"
        done = []
        for m in ctx.inputs:
            if m.type != "research.question":
                done.append(f"ignored {m.type}")
                continue
            p = payload(m)
            qid = str(p.get("question_id") or m.id)
            rid = str(p.get("request_id") or "")
            question = str(p.get("question") or "").strip()
            if not question:
                done.append(f"{qid}: empty question")
                continue
            ctx.output(f"researching {qid}: {question}")
            sources = self.fetch(ctx, question)
            context = "\n\n".join(f"Source {u}:\n{t[:4000]}" for u, t in sources)
            answer = ctx.llm.complete(
                f"{ctx.prompt}\n\nAnswer this research question concisely, citing sources when given.\n"
                f"Question: {question}\n\n{context}".strip())
            if is_mock(answer):
                answer = ("No model answer available (offline/mock provider). "
                          "Question recorded for a human or a later run.")
            path = f"research/{qid}.md"
            sources_md = "\n".join(f"- {u}" for u, _ in sources) or "- (none)"
            write_file(ctx, path, f"# {question}\n\n{answer}\n\n## Sources\n\n{sources_md}\n")
            ctx.emit("research.done", {"request_id": rid, "question_id": qid, "findings_path": path,
                                       "summary": answer[:300]})
            done.append(f"{qid} answered")
        return "; ".join(done)

    @staticmethod
    def fetch(ctx: TurnContext, question: str):
        urls = re.findall(r"https?://\S+", question)
        tmpl = ctx.config.get("search_url")
        if not urls and tmpl:
            urls = [str(tmpl).replace("{q}", urllib.parse.quote(question))]
        out = []
        for u in urls[:3]:
            try:
                res = ctx.tools.call("http.fetch", url=u)
            except PermissionDenied:
                ctx.output("http.fetch not granted; answering from the model only")
                break
            if not res.is_error:
                out.append((u, res.content))
        return out
