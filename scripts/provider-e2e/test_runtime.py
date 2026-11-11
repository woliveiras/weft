"""Focused adapter checks using real LangGraph and an isolated SDK backend."""
import unittest

from weft.adapters.langgraph import LangGraphAgent
from weft.testing import FakeBackend, run_agent
from runtime import build


class RealAdapterTests(unittest.TestCase):
    def test_graph_returns_gateway_reply_and_usage(self):
        backend = FakeBackend(llm=["deterministic answer"])
        result = run_agent(LangGraphAgent("writer", build), backend, prompt="system instruction")
        self.assertEqual(result.summary, "deterministic answer")
        self.assertEqual(result.usage.input_tokens, 10)
        self.assertEqual(len(backend.chat_requests), 1)
        request = backend.chat_requests[0]
        self.assertEqual(request["system"], "system instruction\n")
        self.assertEqual(request["messages"], [{"role": "user", "content": "provider journey"}])
        # Model selection belongs to the gateway execution context.
        self.assertFalse(request.get("model"))

    def test_gateway_failure_is_propagated(self):
        def fail(request):
            raise RuntimeError("gateway unavailable")

        backend = FakeBackend(llm=fail)
        with self.assertRaisesRegex(RuntimeError, "gateway unavailable"):
            run_agent(LangGraphAgent("writer", build), backend)
        self.assertEqual(len(backend.chat_requests), 1)
        self.assertEqual(backend.emitted, [])


if __name__ == "__main__":
    unittest.main()
