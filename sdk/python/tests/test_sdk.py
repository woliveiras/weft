import json
import threading
import unittest
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from weft import Agent, PermissionDenied, extract_json, make_server
from weft.adapters import FrameworkNotInstalled, LangGraphAgent, available
from weft.testing import FakeBackend, message, run_agent


class FakePlatform:
    """A callback server implementing the gateway side of Runtime Protocol v1."""

    TOKEN = "tok-123"

    def __init__(self):
        self.state = {}
        self.outputs = []
        self.emits = []
        self.tool_calls = []
        self.chats = []
        platform = self

        class H(BaseHTTPRequestHandler):
            def log_message(self, *a):
                pass

            def _send(self, code, obj=None, raw=None):
                body = raw if raw is not None else (json.dumps(obj).encode() if obj is not None else b"")
                self.send_response(code)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def _body(self):
                return self.rfile.read(int(self.headers.get("Content-Length") or 0))

            def _auth(self):
                if self.headers.get("Authorization") != f"Bearer {FakePlatform.TOKEN}":
                    self._send(401, {"error": "bad token"})
                    return False
                return True

            def do_GET(self):
                if not self._auth():
                    return
                path = self.path.split("/executions/exec", 1)[1]
                if path == "/tools":
                    self._send(200, [{"name": "filesystem.read", "description": "read", "parameters": {}}])
                elif path.startswith("/state/"):
                    k = path[len("/state/"):]
                    if k in platform.state:
                        self._send(200, raw=platform.state[k])
                    else:
                        self._send(404, {"error": "not found"})
                else:
                    self._send(404, {"error": "nope"})

            def do_PUT(self):
                if not self._auth():
                    return
                path = self.path.split("/executions/exec", 1)[1]
                platform.state[path[len("/state/"):]] = self._body()
                self._send(204)

            def do_POST(self):
                if not self._auth():
                    return
                path = self.path.split("/executions/exec", 1)[1]
                body = json.loads(self._body() or b"{}")
                if path.startswith("/tools/"):
                    name = path[len("/tools/"):]
                    platform.tool_calls.append((name, body))
                    if name == "shell.exec":
                        self._send(403, {"error": "tool shell.exec not granted", "code": "forbidden"})
                    else:
                        self._send(200, {"content": f"ok {name}", "data": body})
                elif path == "/llm/chat":
                    platform.chats.append(body)
                    self._send(200, {"message": {"role": "assistant", "content": '```json\n{"x": 1}\n```'},
                                     "stopReason": "stop", "model": "m", "usage": {"inputTokens": 3, "outputTokens": 2}})
                elif path == "/emit":
                    platform.emits.append(body)
                    self._send(200, {"queued": True})
                elif path == "/output":
                    platform.outputs.append(body["text"])
                    self._send(204)
                elif path == "/ask":
                    self._send(200, {"answer": "yes: " + body["question"]})
                else:
                    self._send(404, {"error": "nope"})

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), H)
        self.url = f"http://127.0.0.1:{self.server.server_address[1]}/internal/v1/executions/exec"
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    def close(self):
        self.server.shutdown()
        self.server.server_close()


class Everything(Agent):
    name = "everything"

    def turn(self, ctx):
        ctx.output("hello")
        data = ctx.llm.json("give me json", default={})
        r = ctx.tools.call("filesystem.read", path="a.txt")
        try:
            ctx.tools.call("shell.exec", argv=["ls"])
            denied = False
        except PermissionDenied:
            denied = True
        ctx.state.set_json("k", {"v": 1})
        got = ctx.state.get_json("k")
        missing = ctx.state.get("nope")
        answer = ctx.ask_human("ok?")
        ctx.emit_now("now", {"a": 1})
        ctx.emit("out", {"data": data, "tool": r.content, "denied": denied, "got": got,
                         "missing": missing is None, "answer": answer, "inputs": len(ctx.inputs)},
                 target="b")
        ctx.broadcast("did stuff")
        ctx.declare_done()
        return "all done"


class Boom(Agent):
    name = "boom"

    def turn(self, ctx):
        raise RuntimeError("kaput")


def post(url, body, headers=None):
    req = urllib.request.Request(url, data=json.dumps(body).encode(),
                                 headers={"Content-Type": "application/json", **(headers or {})}, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=10) as r:
            return r.status, json.loads(r.read())
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read())


class ProtocolRoundTrip(unittest.TestCase):
    def setUp(self):
        self.platform = FakePlatform()
        self.runtime = make_server([Everything(), Boom()], host="127.0.0.1", port=0, secret="s3cret")
        self.base = f"http://127.0.0.1:{self.runtime.server_address[1]}"
        threading.Thread(target=self.runtime.serve_forever, daemon=True).start()

    def tearDown(self):
        self.runtime.shutdown()
        self.runtime.server_close()
        self.platform.close()

    def turn_input(self, agent):
        return {"executionId": "exec", "graphId": "g", "nodeId": agent, "agent": agent, "replica": 0,
                "replicas": 1, "iteration": 3, "prompt": "p", "system": "s",
                "inputs": [{"id": "m1", "type": "t", "source": "x", "target": "", "payload": {"a": 1},
                            "metadata": {"correlation_id": "c"}}],
                "outputs": [], "tools": [], "model": {}, "workdir": "/w",
                "callback": {"url": self.platform.url, "token": FakePlatform.TOKEN}}

    def test_health(self):
        with urllib.request.urlopen(self.base + "/v1/health") as r:
            h = json.loads(r.read())
        self.assertEqual(h["status"], "ok")
        self.assertEqual(h["agents"], ["boom", "everything"])

    def test_turn(self):
        code, res = post(self.base + "/v1/turn", self.turn_input("everything"),
                         {"X-Weft-Runtime-Secret": "s3cret"})
        self.assertEqual(code, 200, res)
        self.assertEqual(res["summary"], "all done")
        self.assertEqual(res["signal"], "no_further_work")
        self.assertEqual(res["broadcast"], "did stuff")
        self.assertEqual(res["usage"], {"inputTokens": 0, "outputTokens": 0})
        self.assertEqual(res["toolCalls"], 0)
        e = res["emits"][0]
        self.assertEqual(e["type"], "out")
        self.assertEqual(e["target"], "b")
        p = e["payload"]
        self.assertEqual(p["data"], {"x": 1})
        self.assertTrue(p["denied"])
        self.assertEqual(p["got"], {"v": 1})
        self.assertTrue(p["missing"])
        self.assertEqual(p["answer"], "yes: ok?")
        self.assertEqual(p["inputs"], 1)
        self.assertEqual(self.platform.outputs, ["hello"])
        self.assertEqual(self.platform.emits, [{"type": "now", "payload": {"a": 1}}])
        self.assertTrue(self.platform.chats[0]["json"])

    def test_secret_required(self):
        code, res = post(self.base + "/v1/turn", self.turn_input("everything"))
        self.assertEqual(code, 401)

    def test_agent_error_is_500(self):
        code, res = post(self.base + "/v1/turn", self.turn_input("boom"), {"X-Weft-Runtime-Secret": "s3cret"})
        self.assertEqual(code, 500)
        self.assertIn("kaput", res["error"])

    def test_unknown_agent_404(self):
        code, _ = post(self.base + "/v1/turn", self.turn_input("ghost"), {"X-Weft-Runtime-Secret": "s3cret"})
        self.assertEqual(code, 404)


class JSONExtraction(unittest.TestCase):
    def test_cases(self):
        self.assertEqual(extract_json('{"a": 1}'), {"a": 1})
        self.assertEqual(extract_json('Sure!\n```json\n{"a": [1, 2]}\n```\nbye'), {"a": [1, 2]})
        self.assertEqual(extract_json('prefix {"a": "}{"} suffix {"b": 2}'), {"a": "}{"})
        self.assertEqual(extract_json("no json here"), None)
        self.assertEqual(extract_json('broken {"a": } then {"ok": true}'), {"ok": True})
        self.assertEqual(extract_json(""), None)


class Harness(unittest.TestCase):
    def test_fake_backend_grants(self):
        class A(Agent):
            name = "a"

            def turn(self, ctx):
                ctx.tools.call("filesystem.write", path="x", content="1")
                self.denied = ctx.tools.try_call("shell.exec", argv=["ls"]) is None
                ctx.emit("t", {"j": ctx.llm.json("hi", default={"d": 1})})
        fb = FakeBackend(grants=["filesystem.*"])
        a = A()
        res = run_agent(a, fb, inputs=[message("x")])
        self.assertTrue(a.denied)
        self.assertEqual(fb.files["x"], "1")
        # the default fake LLM mirrors the server's mock provider
        self.assertEqual(res.emits[0].payload["j"]["mock"], True)


class Adapters(unittest.TestCase):
    def test_missing_framework_is_clear(self):
        if available("langgraph"):
            self.skipTest("langgraph installed")
        agent = LangGraphAgent("lg", lambda ctx: None)
        with self.assertRaises(FrameworkNotInstalled):
            run_agent(agent, FakeBackend())


class UsageCacheTokens(unittest.TestCase):
    def test_round_trip_and_add(self):
        from weft.types import Usage
        u = Usage.from_json({"inputTokens": 20, "outputTokens": 3, "cacheReadTokens": 8, "cacheWriteTokens": 4})
        self.assertEqual((u.cache_read_tokens, u.cache_write_tokens), (8, 4))
        u.add(Usage(1, 1, 2, 0))
        self.assertEqual(u.to_json(), {"inputTokens": 21, "outputTokens": 4, "cacheReadTokens": 10, "cacheWriteTokens": 4})
        # Providers that report no cache keep the original wire shape.
        self.assertEqual(Usage.from_json({"inputTokens": 1, "outputTokens": 2}).to_json(), {"inputTokens": 1, "outputTokens": 2})

if __name__ == "__main__":
    unittest.main()
