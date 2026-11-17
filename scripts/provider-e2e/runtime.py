"""Real LangGraph runtime fixture; no provider SDK, key or direct model URL."""
import json
import sys
from http.server import ThreadingHTTPServer
from pathlib import Path
from threading import Lock

from langchain_core.messages import HumanMessage, SystemMessage
from langgraph.graph import END, START, StateGraph
from weft.adapters.langgraph import LangGraphAgent, weft_chat_model
from weft.server import RuntimeApp, make_handler, setup_logging


def build(ctx):
    model = weft_chat_model(ctx)
    graph = StateGraph(dict)

    def infer(state):
        reply = model.invoke([SystemMessage(content=ctx.prompt), HumanMessage(content="provider journey")])
        return {"summary": reply.content}

    graph.add_node("infer", infer)
    graph.add_edge(START, "infer")
    graph.add_edge("infer", END)
    return graph.compile()


class AuditedApp(RuntimeApp):
    def __init__(self, audit):
        super().__init__([LangGraphAgent("writer", build)])
        self.audit = audit
        self.lock = Lock()

    def turn(self, body):
        code, result = super().turn(body)
        # Only the disposable harness reads this file. It includes callback
        # capabilities, so it is never served to the browser or committed.
        with self.lock, self.audit.open("a") as output:
            output.write(json.dumps({"input": body, "status": code, "result": result}) + "\n")
        return code, result


if __name__ == "__main__":
    setup_logging()
    root = Path(sys.argv[1])
    server = ThreadingHTTPServer(("127.0.0.1", 0), make_handler(AuditedApp(root / "turns.jsonl")))
    server.daemon_threads = True
    (root / "runtime-url").write_text(f"http://127.0.0.1:{server.server_port}")
    server.serve_forever()
