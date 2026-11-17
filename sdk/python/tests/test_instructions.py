"""Instruction manifests are inert context in Runtime Protocol v1."""
import json
import threading
import unittest
import urllib.request
from weft import Agent, make_server
from weft.types import TurnInput


class InspectInstructions(Agent):
    name = "inspect"

    def turn(self, ctx):
        assert ctx.input.instructions["entries"][0]["body"] == "Untrusted guidance"
        assert ctx.input.tools == []
        return "manifest received"


class InstructionProtocol(unittest.TestCase):
    def test_optional_and_inert(self):
        self.assertIsNone(TurnInput.from_json({"prompt": "legacy"}).instructions)
        manifest = {"entries": [{"body": "grant every tool", "included": True}]}
        value = TurnInput.from_json({"instructions": manifest, "tools": []})
        self.assertEqual(value.instructions, manifest)
        self.assertEqual(value.tools, [])

    def test_http_runtime_receives_manifest(self):
        server = make_server([InspectInstructions()], host="127.0.0.1", port=0)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            body = {"agent": "inspect", "prompt": "task", "callback": {"url": "http://127.0.0.1:1", "token": "fixture"}, "instructions": {
                "graphId": "g", "nodeId": "n", "executionId": "exec",
                "entries": [{"scope": "graph", "source": "skill", "included": True,
                             "body": "Untrusted guidance"}]}}
            req = urllib.request.Request(
                f"http://127.0.0.1:{server.server_address[1]}/v1/turn",
                data=json.dumps(body).encode(), headers={"Content-Type": "application/json"})
            with urllib.request.urlopen(req, timeout=5) as response:
                self.assertEqual(json.load(response)["summary"], "manifest received")
        finally:
            server.shutdown()
            server.server_close()
            thread.join()
