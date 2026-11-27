# Example agents

Five agents built with Eino run in the bundled Go service on port 8000. Together they make the
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
* the developer writes Go source and a matching test under `src/<task>/`;
* the reviewer approves unless a file is empty or contains `TODO`;
* the researcher records that no answer was available.

The whole pipeline therefore runs end to end offline, and QA really
executes the generated tests.

## Configuration (node `config`)

* `qa.test_command` — default `go test ./...`; it runs through `shell.exec` in argv form.
* `qa.test_timeout_sec` — default 600.
* `researcher.search_url` — a URL template containing `{q}`, fetched with `http.fetch` when that tool is granted.

## Run and test

The reviewer compiles its two stages, `analyse` and `decide`, into an Eino graph at startup. The same binary registers all five roles and the document summary agent.

```sh
docker build -t weft/agents:dev agents/eino
cd agents/eino
go test -race ./...
WEFT_URL=http://127.0.0.1:8080 go run ./cmd/weft-eino-agent
```

For controller origins, authentication and graph wiring, see the [Go agent guide](eino/README.md).
