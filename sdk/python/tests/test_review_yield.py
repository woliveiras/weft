"""Durable review signals survive the Python callback boundary."""
import json
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from weft.callback import CallbackClient, PermissionDenied
from weft.context import TurnContext
from weft.types import ToolResult, TurnInput


class ReviewYieldTests(unittest.TestCase):
    def test_legacy_result_does_not_yield(self):
        result = ToolResult.from_json({"content": "ordinary result"})
        self.assertFalse(result.yield_turn)
        self.assertNotIn("yield", result.to_json())

    def test_yield_round_trip(self):
        wire = {"yield": True, "content": "Waiting for review", "data": {"id": "review-fixture"}}
        result = ToolResult.from_json(wire)
        self.assertTrue(result.yield_turn)
        self.assertFalse(result.is_error)
        self.assertEqual(result.to_json(), {**wire, "isError": False, "truncated": False})

    def test_http_callback_preserves_yield_and_denial(self):
        requests = []

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_POST(self):
                body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                requests.append((self.path, self.headers.get("Authorization"), body))
                if self.path.endswith("/review.request"):
                    status, payload = 200, {"content": "Review persisted", "yield": True}
                else:
                    status, payload = 403, {"error": "Turn already yielded"}
                encoded = json.dumps(payload).encode()
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(encoded)))
                self.end_headers()
                self.wfile.write(encoded)

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            callback = CallbackClient(
                f"http://127.0.0.1:{server.server_port}/internal/v1/executions/fixture",
                "artificial-callback-token", retries=1,
            )
            ctx = TurnContext(TurnInput.from_json({}), callback)
            result = ctx.tools.call("review.request", title="Inspect candidate")
            self.assertTrue(result.yield_turn)
            self.assertEqual(result.content, "Review persisted")
            with self.assertRaises(PermissionDenied):
                ctx.tools.call("filesystem.write", path="blocked")
            self.assertEqual(requests[0], (
                "/internal/v1/executions/fixture/tools/review.request",
                "Bearer artificial-callback-token", {"title": "Inspect candidate"},
            ))
        finally:
            server.shutdown()
            server.server_close()
            thread.join()
