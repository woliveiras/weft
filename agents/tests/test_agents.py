import json
import unittest

from weft.testing import FakeBackend, message, run_agent
from weft_agents import QA, Developer, Planner, Researcher, Reviewer
from weft_agents.qa import interpret

ALL = ["filesystem.*", "git.*", "shell.exec", "http.fetch"]


class PlannerTest(unittest.TestCase):
    def test_mock_llm_falls_back_to_heuristic(self):
        fb = FakeBackend(grants=ALL)
        res = run_agent(Planner(), fb, inputs=[message("feature.request", {
            "title": "Greeter", "description": "- add a greet function\n- add a CLI"}, id="req1")])
        types = [e.type for e in res.emits]
        self.assertEqual(types, ["task.ready", "task.ready"])
        self.assertEqual(res.emits[0].payload["request_id"], "req1")
        self.assertEqual(res.emits[0].payload["task_id"], "req1-t1")
        self.assertIn("plans/req1.md", fb.files)

    def test_questions_wait_for_research(self):
        plan = {"tasks": [{"id": "t1", "title": "Do it", "description": "d"}],
                "questions": [{"id": "q1", "question": "Which lib?"}]}
        fb = FakeBackend(grants=ALL, llm=[plan])
        res = run_agent(Planner(), fb, inputs=[message("feature.request", {"title": "X", "description": "y"}, id="r")])
        self.assertEqual([e.type for e in res.emits], ["research.question"])
        q = res.emits[0].payload
        self.assertEqual(q["question_id"], "r-q1")
        # research comes back -> tasks go out, with the findings attached
        res2 = run_agent(Planner(), fb, inputs=[message("research.done", {
            "request_id": "r", "question_id": "r-q1", "findings_path": "research/r-q1.md"})])
        self.assertEqual([e.type for e in res2.emits], ["task.ready"])
        self.assertEqual(res2.emits[0].payload["research"], ["research/r-q1.md"])
        # duplicate research.done is harmless
        res3 = run_agent(Planner(), fb, inputs=[message("research.done", {"request_id": "r", "question_id": "r-q1"})])
        self.assertEqual(res3.emits, [])

    def test_junk_payload(self):
        res = run_agent(Planner(), FakeBackend(grants=ALL), inputs=[message("feature.request", "just text")])
        self.assertEqual(res.emits[0].type, "task.ready")
        res = run_agent(Planner(), FakeBackend(grants=ALL), inputs=[message("weird.type", 42)])
        self.assertEqual(res.emits, [])


class ResearcherTest(unittest.TestCase):
    def test_without_http(self):
        fb = FakeBackend(grants=["filesystem.*"])
        res = run_agent(Researcher(), fb, inputs=[message("research.question", {
            "request_id": "r", "question_id": "r-q1", "question": "See https://example.com ?"})])
        self.assertEqual(res.emits[0].type, "research.done")
        self.assertEqual(res.emits[0].payload["findings_path"], "research/r-q1.md")
        self.assertIn("research/r-q1.md", fb.files)
        self.assertTrue(any("http.fetch not granted" in o for o in fb.outputs))

    def test_with_http_and_model(self):
        fb = FakeBackend(grants=ALL, llm=["Use requests."])
        res = run_agent(Researcher(), fb, inputs=[message("research.question", {
            "request_id": "r", "question_id": "q", "question": "Read https://example.com/doc"})])
        self.assertIn("Use requests.", fb.files["research/q.md"])
        self.assertIn("https://example.com/doc", fb.files["research/q.md"])
        self.assertEqual(res.emits[0].payload["summary"], "Use requests.")


class DeveloperTest(unittest.TestCase):
    task = {"request_id": "r", "task_id": "r-t1", "title": "Add greet", "description": "greet people"}

    def test_scaffold_offline(self):
        fb = FakeBackend(grants=ALL)
        res = run_agent(Developer(), fb, inputs=[message("task.ready", self.task)])
        e = res.emits[0]
        self.assertEqual(e.type, "code.ready")
        self.assertEqual(e.payload["iteration"], 1)
        self.assertEqual(len(e.payload["files"]), 2)
        src = [f for f in e.payload["files"] if f.startswith("src/")][0]
        compile(fb.files[src], src, "exec")
        self.assertIn(("git.commit", {"message": "r-t1: scaffold Add greet", "paths": e.payload["files"]}),
                      fb.tool_calls)

    def test_model_files_and_unsafe_paths(self):
        fb = FakeBackend(grants=["filesystem.*"], llm=[{"files": [
            {"path": "src/a.py", "content": "x = 1\n"}, {"path": "../../etc/passwd", "content": "no"}],
            "summary": "s"}])
        res = run_agent(Developer(), fb, inputs=[message("task.ready", self.task)])
        self.assertEqual(res.emits[0].payload["files"], ["src/a.py"])
        self.assertNotIn("../../etc/passwd", fb.files)

    def test_rework_iterations_and_escalation(self):
        fb = FakeBackend(grants=ALL, answers=["ship"])
        run_agent(Developer(max_rounds=1), fb, inputs=[message("task.ready", self.task)])
        r = run_agent(Developer(max_rounds=1), fb, inputs=[message("review.changes_requested",
                                                                   dict(self.task, comments=["fix"]))])
        self.assertEqual(r.emits[0].payload["iteration"], 2)
        self.assertNotIn("escalated", r.emits[0].payload)
        r = run_agent(Developer(max_rounds=1), fb, inputs=[message("qa.failed", dict(self.task, failures=["x"]))])
        self.assertEqual(r.emits[0].payload["iteration"], 3)
        self.assertTrue(r.emits[0].payload["escalated"])
        self.assertEqual(len(fb.questions), 1)


class ReviewerTest(unittest.TestCase):
    def test_heuristic_approves_and_rejects(self):
        fb = FakeBackend(grants=ALL, files={"src/a.py": "x = 1\n", "src/b.py": "# TODO\n"})
        rev = Reviewer(use_langgraph=False)
        res = run_agent(rev, fb, inputs=[message("code.ready", {"task_id": "t", "files": ["src/a.py"]})])
        self.assertEqual(res.emits[0].type, "review.approved")
        res = run_agent(rev, fb, inputs=[message("code.ready", {"task_id": "t", "files": ["src/b.py"]})])
        self.assertEqual(res.emits[0].type, "review.changes_requested")
        self.assertIn("src/b.py contains TODO", res.emits[0].payload["comments"])

    def test_model_verdict(self):
        fb = FakeBackend(grants=ALL, files={"a": "ok"}, llm=[{"verdict": "changes_requested", "comments": ["naming"]}])
        res = run_agent(Reviewer(use_langgraph=False), fb, inputs=[message("code.ready", {"task_id": "t", "files": ["a"]})])
        self.assertEqual(res.emits[0].payload["verdict"], "changes_requested")
        self.assertEqual(res.emits[0].payload["comments"], ["naming"])


class QATest(unittest.TestCase):
    item = {"task_id": "t", "files": ["src/a.py"]}

    def test_pass_and_fail(self):
        fb = FakeBackend(grants=ALL, shell=lambda a: {"content": "ok", "data": {"exitCode": 0, "stdout": "OK"}})
        res = run_agent(QA(), fb, inputs=[message("review.approved", self.item)])
        self.assertEqual(res.emits[0].type, "qa.passed")
        self.assertEqual(fb.tool_calls[0][1]["argv"][:3], ["python", "-m", "unittest"])
        fb = FakeBackend(grants=ALL, shell=lambda a: {"content": "exit code 1\nFAIL: test_x", "isError": True})
        res = run_agent(QA(), fb, inputs=[message("review.approved", self.item)])
        self.assertEqual(res.emits[0].type, "qa.failed")
        self.assertIn("FAIL: test_x", res.emits[0].payload["failures"])

    def test_denied(self):
        res = run_agent(QA(), FakeBackend(grants=["filesystem.*"]), inputs=[message("review.approved", self.item)])
        self.assertEqual(res.emits[0].type, "qa.failed")
        self.assertEqual(res.emits[0].payload["reason"], "shell.exec not granted")

    def test_interpret(self):
        self.assertEqual(interpret("", {"exitCode": 2, "stderr": "boom"}, False)[:2], (False, 2))
        self.assertEqual(interpret("exitCode: 0", None, False)[:2], (True, 0))
        self.assertFalse(interpret("Traceback ...", None, False)[0])


class EndToEnd(unittest.TestCase):
    """Drive a request through all five agents with the mock LLM, like the graph would."""

    def test_pipeline(self):
        fb = FakeBackend(grants=ALL, shell=lambda a: {"content": "", "data": {"exitCode": 0}})
        out = run_agent(Planner(), fb, inputs=[message("feature.request", {
            "title": "Calc", "description": "Add an adder. Add a subtractor."}, id="req")]).emits
        self.assertEqual(len(out), 2)
        code = run_agent(Developer(), fb, inputs=[message(e.type, e.payload) for e in out]).emits
        reviewed = run_agent(Reviewer(use_langgraph=False), fb, inputs=[message(e.type, e.payload) for e in code]).emits
        self.assertTrue(all(e.type == "review.approved" for e in reviewed))
        qa = run_agent(QA(), fb, inputs=[message(e.type, e.payload) for e in reviewed]).emits
        self.assertEqual([e.type for e in qa], ["qa.passed", "qa.passed"])
        json.dumps([e.to_json() for e in qa])  # serialisable on the wire


if __name__ == "__main__":
    unittest.main()
