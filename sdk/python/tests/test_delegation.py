import unittest
from unittest.mock import Mock

from weft.callback import CallbackClient, PermissionDenied, CallbackError
from weft.context import TurnContext
from weft.types import TurnInput, Usage
from test_sdk import FakePlatform


class DelegationTests(unittest.TestCase):
    def test_result_and_error_contract(self):
        backend = Mock()
        ctx = TurnContext(TurnInput(), backend)
        for error in (False, True):
            backend.invoke_tool.return_value = {"content": "child outcome", "isError": error}
            result = ctx.delegate("Inspect supplied text", "review")
            self.assertEqual(result.content, "child outcome")
            self.assertEqual(result.is_error, error)
            backend.invoke_tool.assert_called_with("delegation.request", {"template": "review", "objective": "Inspect supplied text"})
        for error in (PermissionDenied("denied", 403), CallbackError("unavailable")):
            backend.invoke_tool.side_effect = error
            with self.assertRaises(type(error)):
                ctx.delegate("work")
        self.assertEqual(ctx.result.tool_calls, 0)

    def test_http_callback_and_local_accounting_remain_separate(self):
        platform = FakePlatform()
        try:
            ctx = TurnContext(TurnInput(), CallbackClient(platform.url, platform.TOKEN))
            result = ctx.delegate("Inspect supplied text")
            self.assertEqual(result.data, {"template": "analysis", "objective": "Inspect supplied text"})
            self.assertEqual(platform.tool_calls, [("delegation.request", result.data)])
            reply = ctx.llm.chat([{"role": "user", "content": "hello"}])
            self.assertEqual(reply.usage.input_tokens, 3)
            self.assertEqual(ctx.result.usage.input_tokens, 0)
            self.assertEqual(ctx.result.tool_calls, 0)
            ctx.add_usage(Usage(input_tokens=7, output_tokens=2))
            self.assertEqual(ctx.result.usage.input_tokens, 7)
        finally:
            platform.close()
