"""Agent base class and the @agent decorator."""
from __future__ import annotations

from typing import Callable, Optional, Union

from .context import TurnContext
from .types import TurnResult


class Agent:
    """Subclass and implement :meth:`turn`.

    ``turn`` may return a string, which becomes the turn summary. Emits,
    signals and broadcasts are made through the context.
    """

    name: str = ""
    framework: str = "custom"

    def __init__(self, name: Optional[str] = None):
        if name:
            self.name = name
        if not self.name:
            self.name = type(self).__name__.lower()

    def turn(self, ctx: TurnContext) -> Optional[str]:  # pragma: no cover - abstract
        raise NotImplementedError

    def run_turn(self, ctx: TurnContext) -> TurnResult:
        """Run one turn and return the protocol result (used by the server)."""
        summary = self.turn(ctx)
        res = ctx.result
        if isinstance(summary, str) and summary and not res.summary:
            res.summary = summary
        return res


class FunctionAgent(Agent):
    def __init__(self, name: str, fn: Callable[[TurnContext], Union[None, str]], framework: str = "custom"):
        super().__init__(name)
        self._fn = fn
        self.framework = framework

    def turn(self, ctx: TurnContext) -> Optional[str]:
        return self._fn(ctx)


def agent(name: str, framework: str = "custom") -> Callable[[Callable[[TurnContext], Union[None, str]]], FunctionAgent]:
    """Turn a function ``f(ctx) -> str | None`` into an Agent."""

    def wrap(fn: Callable[[TurnContext], Union[None, str]]) -> FunctionAgent:
        return FunctionAgent(name, fn, framework)

    return wrap
