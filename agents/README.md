# Example agents

Five agents built with the `weft` Python SDK and served from one
process (`python -m weft_agents`, port 8000). Together they make the
software-factory graph in `examples/graphs/software-factory.yaml`.

| Agent | Consumes | Emits | Tools used |
|---|---|---|---|
| `planner` | `feature.request`, `research.done` | `research.question`, `task.ready` | `filesystem.write` |
| `researcher` | `research.question` | `research.done` | `filesystem.write`, `http.fetch` (optional) |
| `developer` | `task.ready`, `review.changes_requested`, `qa.failed` | `code.ready` | `filesystem.read/write`, `git.add`, `git.commit` (optional) |
| `reviewer` | `code.ready` | `review.approved` / `review.changes_requested` | `filesystem.read` |
| `qa` | `review.approved` | `qa.passed` / `qa.failed` | `shell.exec` |

The flow contains two kinds of cycle. Planning waits on research: the
planner keeps the plan in state until every `research.done` for that request
has arrived. Rework loops back: review and QA failures return to the
developer, which counts rounds per task and asks a human (`ask_human`) after
three.

## Message payloads

* `feature.request` — `{id?, title, description, acceptance?: [...]}`. The `request_id` is `payload.id`,
  or the message id when there is none.
* `research.question` — `{request_id, question_id, question}`
* `research.done` — `{request_id, question_id, findings_path, summary}`
* `task.ready` — `{request_id, task_id, title, description, files, research: [paths]}`
* `code.ready` — `{request_id, task_id, title, description, files: [paths], summary, branch, iteration, research, escalated?}`
* `review.approved` / `review.changes_requested` — the `code.ready` payload plus `{verdict, comments}`
* `qa.passed` — plus `{report, exit_code}`; `qa.failed` — plus `{report, exit_code, failures}` or `{reason, failures}`

## Without a model

Every agent asks the model for JSON and checks the answer. If the reply is
unusable (for example the platform's `mock` provider returns
`{"mock": true, ...}`), the agent falls back to a deterministic heuristic:

* the planner splits the description into tasks, one per bullet or sentence;
* the developer writes a real module (`src/<name>.py`) with a passing test (`tests/test_<name>.py`);
* the reviewer approves unless a file is empty or contains `TODO`;
* the researcher records that no answer was available.

The whole pipeline therefore runs end to end offline, and QA really
executes the generated tests.

## Configuration (node `config`)

* `qa.test_command` — default `python -m unittest discover -s tests -v`; it runs through `shell.exec` in argv form.
* `qa.test_timeout_sec` — default 600.
* `researcher.search_url` — a URL template containing `{q}`, fetched with `http.fetch` when that tool is granted.

## LangGraph

The reviewer runs as a two-node LangGraph `StateGraph` (analyse → decide)
when `langgraph` is installed. Without it, the same two steps run as plain
functions. Build the image with LangGraph included:

```bash
docker build -f agents/Dockerfile --build-arg EXTRAS=langgraph -t weft/agents:dev .
```

## Run and test

```bash
docker build -f agents/Dockerfile -t weft/agents:dev .        # from the repo root
PYTHONPATH=sdk/python:agents python3 -m weft_agents           # or locally
cd agents && PYTHONPATH=../sdk/python:. python3 -m unittest discover -s tests -v
```
