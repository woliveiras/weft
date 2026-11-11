"""Example Weft agents: planner, researcher, developer, reviewer, qa."""
from .developer import Developer
from .planner import Planner
from .qa import QA
from .researcher import Researcher
from .reviewer import Reviewer


def all_agents():
    return [Planner(), Researcher(), Developer(), Reviewer(), QA()]


__all__ = ["Planner", "Researcher", "Developer", "Reviewer", "QA", "all_agents"]
