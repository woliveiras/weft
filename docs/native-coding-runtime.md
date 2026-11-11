# Native coding runtime

Status: **in progress** (NAT-01 to NAT-10 in the task list; NAT-01 to NAT-08 done, NAT-09 later). This page describes how
Weft's own native runtime becomes a strong coding agent, especially with
local models, without handing the agent loop to a third-party tool.

## Why native

Weft's value is that every model call and every tool call passes through
its gateways: grants, task consent, approvals, budgets, sandboxing and audit.
The native runtime already runs inside those boundaries by construction. A
good coding agent mostly needs four things the runtime can own directly:
context that never overflows, edit tools that rarely fail, a way to understand
a repository cheaply, and a tight edit → verify loop. Building them natively
keeps one loop, one set of tools, one audit trail, and lets us tune prompts and
tool shapes for the local models we run first.

## What exists today

| Piece | Where | Already does |
|---|---|---|
| Tool-calling loop | `runtime/native` | Rounds, platform tools (`emit`, `state_*`, `finish`), text tool-call recovery, explicit failures |
| System prompt | `engine/prompt.go` | Role, directories, channels, how a turn ends |
| File tools | `tools/builtin_filesystem.go` | `read` (paged), `list`, `search` (RE2 or literal, context lines), `stat`, `write`, `edit` (exact snippet), `move`, `mkdir`, `delete`, path grants |
| Code navigation | `tools/builtin_code.go` | `code.outline` (symbols of one file), `code.map` (token-bounded repository map, `builtin_code_map.go`), `BuildCodeMapFor` for prompts |
| Git and shell | `tools/builtin_git.go`, `builtin_shell.go` | Git read/write, `shell.exec` in the node's sandbox under command grants |
| Delegation | `engine/delegation*.go` | Bounded child agents with their own grants and token ledger |
| Guidance | `instructions` | Opt-in skills and project steering per execution |
| Models | `llm`, plugins | Any enabled plugin, context window per provider, conformance suite, cost and budgets |
| Evidence | tasks, workspace observations | Diff/Git inspection, file-change observations, SCM hand-off |

## Gaps and the plan

1. **Measure first (NAT-01).** An evaluation suite of small, real coding tasks
   (fixture repositories with a verification command each: fix a failing
   test, implement a function, a rename across files, a bug described in an
   issue) runs through the real engine and native runtime. It records
   success, model rounds, tokens, time and tool errors per model. A scripted
   model keeps it deterministic in CI; an opt-in run uses local Ollama models.
   Every later change is judged against this baseline.
2. **Context that never overflows (NAT-02).** Today a turn's conversation only
   grows, and a 32k local window fails the turn with "prompt exceeds the
   context window". The loop budgets tokens against the provider's window:
   older tool results are elided to one-line receipts ("read src/a.go 1-200,
   elided"), single results are capped with a pointer to page further, and when
   still over budget the earlier conversation is compacted into a summary by a
   model call. The task, system prompt, plan and latest results are never
   dropped. Compactions are recorded as events.
3. **Edits that rarely fail (NAT-03).** `filesystem.edit` reports the closest
   matching region when the snippet is not found (whitespace and indentation
   differences explained); `filesystem.multi_edit` applies several snippet
   edits to one file atomically; `filesystem.apply_patch` applies a unified
   diff across files atomically, each path checked against the grants.
4. **Understanding a repository cheaply (NAT-04).** `code.outline` returns a
   file's symbols (Go through `go/parser`; TypeScript/JavaScript, Python and
   Java through bounded heuristics first). `code.map` returns a token-bounded
   map of the repository (directories and top symbols, ranked by size and
   references) and can be placed in the system prompt of planning agents.
   `filesystem.search` gains literal mode, context lines, result limits and
   ignores `.git`, `node_modules` and vendored code by default.
5. **A tight verify loop (NAT-05).** Projects and workflows declare their
   checks (test, lint, build commands). `check.run` runs a declared check in
   the node's sandbox and worktree under the existing shell grants and returns
   the exit status plus concise failures (parsers for Go test, Vitest/Jest,
   pytest and a generic first-errors fallback). Results are recorded as task
   evidence, so a task can show which checks passed instead of "not
   independently verified".
6. **Visible plans (NAT-06).** A `plan.update` platform tool keeps a short
   checklist per execution (pending, in progress, done), shown live on the
   task. Prompts ask agents to plan, edit, verify and update the plan.
7. **Checkpoints (NAT-07).** Before edits in a round, the runtime records a
   checkpoint of the worktree on a hidden Git ref; a person (or the agent,
   with a grant) can restore the last good checkpoint. Failed turns leave the
   workspace and its checkpoints inspectable.
8. **Prompts and tools tuned for local models (NAT-08).** Role templates
   (planner, implementer, reviewer, verifier) ship as built-in skills; tool
   schemas are compact and each node offers only its tools; guidance for
   small models (one tool call per round, argument examples) is enabled per
   provider. Changes are accepted only when the evaluation improves.
9. **Streaming (NAT-09, later).** Token streaming from providers that support
   it (Ollama, OpenAI-compatible) to show progress live and shorten first
   output; accounting and budgets unchanged.
10. **Software Factory v2 (NAT-10).** Plan (read-only, `code.map`) →
    Implement (edit tools, `check.run`) → Review (read-only diff review) →
    Verify (`check.run`) → hand-off (SCM), measured on the evaluation suite
    with a local model.

## Evaluation suite (NAT-01)

`server/internal/eval` runs each case through the real engine, native runtime,
tool gateway and sandbox: the case's repository becomes the graph workspace,
one native agent (file tools, Git read, `go`/`gofmt` commands) receives the
request as a task message, and after the turn the **hidden tests** are copied in
and the case's check runs. The verdict comes from the repository, never from the
agent's claim.

| Case | What it needs |
|---|---|
| `fix-failing-test` | Find a bug from a failing test and fix the code, not the test |
| `implement-function` | Implement a stubbed function (Unicode-correct) and add a test |
| `rename-across-files` | Rename a function and every caller across files |
| `bug-from-issue` | Fix a bug described only in issue text and add a regression test |

Per run it records success, tool calls, tool errors (failed or denied gateway
calls), input/output tokens and time. `TestScriptedSolutionsPassAndLazyAnswersFail`
keeps the harness honest in CI: a scripted model solving every case passes and
a model that does nothing fails every case. Real models are measured with
`make eval-local EVAL_MODELS=llama3.2:latest,qwen3.6:27b-q4_K_M`, which writes
the tables to `server/eval-report.md`.

Hosted models behind an OpenAI-compatible endpoint (Gemini by default) are
measured with `make eval-cloud CLOUD_MODEL=gemini-3.5-flash` (add
`EVAL_WORKFLOW=factory` for Software Factory v2), and the provider itself is
checked first with `make test-llm-cloud`. Both read the key from
`WEFT_CLOUD_API_KEY` in the local `.env` and send one request at a time with
retries, so free-tier rate limits slow a run instead of failing it.

A run counts as solved only when the checks pass **and** the agent finished
its turn: a correct workspace from a turn that never ends is not usable.

### Baseline (2026-11-10, before NAT-02)

Host Ollama on the development Mac, default 32k context, 30 tool calls and 20
minutes per case.

| Model | Solved | Tool calls | Tool errors | Tokens in / out | Time |
|---|---|---|---|---|---|
| `llama3.2:latest` (3B) | 0/4 | 45 | 36 | 61 308 / 2 417 | 40 s |
| `qwen3.6:27b-q4_K_M` | 3/4 | 23 | 5 | 50 441 / 1 825 | 1 h 03 min |

What it shows:

- The 3B model calls tools readily but most calls fail (36 of 45). This run
  did not record why; the harness now keeps the first failed or denied calls
  of each case (`toolFailures`) so the next run shows the causes. Failure
  feedback that says how to fix a call (NAT-03) and smaller, clearer tool
  schemas (NAT-08) are the expected levers.
- The 27B model solves the tasks but spends 12-16 minutes each, almost all of
  it processing the prompt again on every round (15-19k input tokens per
  case for 5-12 tool calls). On `bug-from-issue` it fixed the code and the
  hidden tests passed, but the turn did not end within 20 minutes. Keeping the
  conversation small (NAT-02) is the first lever for both speed and finishing.

### Measurements after each step

Each row is a live `llama3.2:latest` run (qwen 27B runs are paused: they froze
the development Mac on 2026-11-10). Same cases, 32k window.

| After | Solved | Tool calls | Tool errors | Tokens in / out | Time | Notes |
|---|---|---|---|---|---|---|
| Baseline (NAT-01) | 0/4 | 45 | 36 | 61 308 / 2 417 | 40 s | failure causes not recorded |
| NAT-02, NAT-03, NAT-06, bounded answers | 0/4 | 13 | 8 | 16 274 / 535 | 18 s | holds; most failures are argument types (`"false"` for a boolean, a string for `argv`, an array for `path`), one turn wrote a call as text, and turns end after a single error |
| NAT-08 part 1: argument coercion, text-call correction | 0/4 | 7 | 2 | 13 441 / 673 | 17 s | improves tool errors; the 3B model still ends its turn after one or two calls without changing the code |
| NAT-08 small-model guidance (`EVAL_GUIDANCE=small`) | 0/4 | 45 | 20 | 111 247 / 6 572 | 1 min 24 s | holds; turns keep going instead of stopping, but the model guesses file names. A first version with concrete example paths solved 1/4, only because one example named the fixture's own file: examples now use placeholders. Off by default |
| NAT-10 Software Factory v2 (`EVAL_WORKFLOW=factory`) | 0/4 | 6 | 5 | 23 511 / 2 869 | 1 min 44 s | holds; the 3B agents finish their turns without emitting the next message (`task.ready`, `code.ready`), so every run stops after the planner or implementer. Needs a stronger local model |

### Cloud baseline (2026-11-11, `gemini-3.5-flash`)

Measured with `make eval-cloud` on the Gemini API free tier, one request at a
time, with every NAT step in place:

| Workflow | Solved | Tool calls | Tool errors | Tokens in / out | Time |
|---|---|---|---|---|---|
| Single agent | 4/4 | 49 | 0 | 315 085 / 6 985 | 2 min 4 s |
| Software Factory v2 | 4/4 | 105 | 0 | 855 075 / 30 519 | 6 min 2 s |

What it shows:
- The native runtime, the tools and the factory work end to end with a
  capable model. Getting there needed two provider fixes: Gemini's thought
  signatures are now sent back with tool calls, and its hidden thinking
  tokens are counted as output.
- On these four small cases the factory costs about 2.7 times the tokens and
  3 times the time for the same result. Its review and verification steps can
  only pay off on harder tasks.
- The suite is now at its ceiling (4/4), so "improves or holds" can no longer
  show an improvement. The next lever is harder cases, such as multi-package
  changes, a failing check found only by the verifier, or ambiguous issues,
  so that both models and workflows separate again.

## Turn context management (NAT-02)

`runtime/native/context.go` budgets every model call of a native turn
against the provider's context window. The engine reports the window through
the optional `runtime.ContextEnv` extension of `TurnEnv` (`llm.ContextWindowOf`:
Ollama's `num_ctx`, 32 768 by default); a provider that does not report one is
budgeted as 32 768 tokens. The working limit is the window minus an answer
reserve (an eighth, at most 8 192 tokens), capped at 64k tokens so large
hosted windows still keep turns small.

- **Caps.** One tool result is cut to a sixth of the limit, with a hint on
  how to ask for less (`offset/limit` for `filesystem.read`, a narrower
  pattern for search and list, filtered output for commands).
- **Receipts.** Above 75 % of the limit, tool results older than the two
  latest rounds become one line (`[elided] filesystem.read path=src/a.go
  returned 200 lines (6 120 characters); call it again if you need the
  content`) and long arguments of older calls (file contents written) are
  shortened, oldest first, down to 50 %. Eliding in one batch keeps the prompt
  prefix stable between batches, so providers with a prompt cache reuse it.
- **Compaction.** If receipts are not enough, every round but the latest is
  summarised by a model call through `TurnEnv.Chat` (no tools, at most 1 024
  answer tokens, metadata `purpose=context_compaction`), so budgets, cost and
  audit apply. The summary follows the task as a user message marked as a
  record, not instructions. A failed compaction is reported; the turn
  continues only while the conversation still fits, otherwise it fails with
  the cause.
- **Bounded answers.** A request without `maxTokens` gets the same answer
  reserve (`llm.DefaultAnswerTokens`). Ollama sends it as `num_predict`;
  without it, Ollama generates until the window is full, and in a live run a
  degenerate `llama3.2` answer reached 27k tokens over 7 minutes. An answer
  cut by the limit fails the turn (`ErrAnswerCutOff`) instead of counting as
  a final answer.
- **Never dropped.** The system prompt, the task message, `plan.update`
  results and the latest round.
- **Signals.** `agent.context` events (operation, outcome, messages, tokens
  before and after, window), a narration line in the agent log, and the
  metrics `weft_native_context_operations_total{operation,outcome}`
  (`cap`, `elide`, `compact`) and
  `weft_native_context_compaction_duration_seconds`. The evaluation report
  counts them per case ("Context ops").

Measured effect: the scripted suite holds (4/4 scripted, 0/4 lazy). The
baseline cases never come near the budget - their requests were 1.5-3k
tokens against a 21k high-water mark - so the live numbers cannot move from
NAT-02 alone; they will be re-measured with NAT-03. The baseline's reading of
the qwen slowness was also wrong: Ollama reports the whole prompt in
`prompt_eval_count` even when it is served from its prompt cache (the same
1 529-token prompt took 620 ms, then 10 ms), so the 12-16 minutes per case do
not come from reprocessing the prompt. The cause is still to be measured;
memory pressure from a 27B model with a 32k context on the development Mac is
the leading suspect.

## Edit tools (NAT-03)

`tools/builtin_edit.go` makes failed edits actionable and multi-place edits
atomic. All three tools need the `filesystem.write` capability and check every
path with the same grant rules as `filesystem.write`, including the refusal to
write inside `.git`.

- **`filesystem.edit`** still replaces an exact snippet. A file with CRLF line
  endings matches a snippet written with LF. When the snippet is not found,
  the error names the most similar lines, with line numbers and the exact file
  text to copy, and says why they differ: indentation (tabs or spaces in the
  file), trailing whitespace, or the first differing line. A snippet found
  more than once reports the line of the first match.
- **`filesystem.multi_edit`** `{path, edits}` applies up to 50 snippet edits
  in order in memory and writes once. If any edit fails, nothing is written,
  and the error names the edit that failed.
- **`filesystem.apply_patch`** `{patch}` applies a unified diff to one or more
  files.
  - Hunk counts and line numbers in `@@` headers are only hints: the context
    lines decide where a hunk goes (exact match first, then ignoring trailing
    whitespace, nearest to the stated line).
  - `--- /dev/null` creates a file. Deletes and renames are refused with a
    pointer to `filesystem.delete` and `filesystem.move`, which have their own
    grants.
  - Every path is checked and every result computed before anything is
    written; a failed write restores the files already written.
- The file-operations view lists the files of `multi_edit` and `apply_patch`
  calls, reading the `+++` headers of the patch.

## Visible plans (NAT-06)

Native agents get a fifth platform tool, `plan.update` `{items: [{step,
status}]}`, with status `pending`, `in_progress` or `done`. It needs no grant:
like `finish`, it only touches the agent's own turn.

- **Each call replaces the whole plan.** A plan has 1 to 20 steps, and steps
  are cut at 200 characters. At most one step can be `in_progress`; an empty
  step or an unknown status is refused with the reason.
- **What the model sees.** The tool returns the rendered checklist
  (`plan updated (1/3 done):` then `[x]`, `[>]` and `[ ]` lines). Context
  management keeps the latest plan result: it is never elided, and compaction
  copies it into the summary message. Older plans are superseded and can be
  elided.
- **Recording.** The engine records each plan as an `agent.plan` event of the
  execution (`runtime.PlanEnv`). The task page shows the latest plan per
  execution under "Agent plans" and updates it live from the event stream.
  The page says that steps marked done are the agent's claim, not
  verification.
- **Prompting.** The native system prompt has a "Working method" section:
  keep a short plan with `plan.update`; plan, make the change, verify it,
  then update the plan.

## Small-model robustness (NAT-08)

- **Argument coercion.** Before approval, audit and execution, the gateway
  converts argument values to the JSON type the tool schema declares when the
  conversion is unambiguous:
  - `"true"` or `"false"` for a boolean, a numeric string for a number;
  - a one-element array for a string, a number or boolean for a string;
  - a JSON string holding an array or object;
  - a command line for an array of strings (split like `shell.exec`'s
    `command`).

  The tool result tells the model which values were converted.
  `weft_tool_argument_coercions_total{tool}` counts these calls.
- **Readable argument errors.** Values that cannot be converted fail with the
  expected type, e.g. `offset must be an integer (without quotes), got a JSON
  string`, instead of Go decoder messages.
- **Small-model guidance per provider.** A provider's `modelGuidance: small`
  (console: Model guidance → Small model) gives native agents:
  - tool schemas compacted to their first sentence, without parameter
    descriptions;
  - a "How to call tools" section with call shapes for the tools they hold,
    with placeholders and never real-looking paths, because models copy
    examples verbatim;
  - one tool call per reply: extra calls are answered "not run".

  It did not improve the `llama3.2` solve rate, so it stays opt-in.
- **Role skills.** `weft-planner`, `weft-implementer`,
  `weft-reviewer` and `weft-verifier` ship as read-only built-in skills
  (see [Skills and steering](instructions.md)); graphs select them per agent.
- **Text tool-call correction.** A reply that looks like a tool call but
  names a tool that was not offered, or has arguments that are not an object,
  gets a correction that lists the offered tools, at most twice per turn. The
  turn then fails as before.

## Checkpoints (NAT-07)

Before the agent changes its workspace in a round, the engine records a
checkpoint of the replica's directory on a hidden Git ref, so a bad round can
be undone without touching the branch.

- **When.** Every model call starts a round. The first call of the round to a
  tool with capability `filesystem.write`, `filesystem.delete`, `git.write` or
  `shell.exec` records a checkpoint, after the gateway authorized (and, if
  required, a person approved) the call and before it runs. The hook is
  `tools.Principal.BeforeMutation`, set by the engine's turn environment, so no
  tool knows about checkpoints and new tools with those capabilities (NAT-03's
  edit tools) are covered. External runtimes get one checkpoint per turn (they
  call tools without model calls through the engine).
- **What.** A commit on `refs/weft/checkpoints/<graph>/<node>/r<n>/<seq>`
  (id `<node>/r<n>/<seq>`) whose tree is HEAD with the replica's directory
  replaced by its current files: tracked and untracked, without ignored files,
  nested repositories and the scratch folder `.weft/`. Its parent is HEAD;
  trailers record the execution, reason and tool. The tree is built in a
  temporary index (`GIT_INDEX_FILE`) and committed with `commit-tree`, so the
  index, HEAD and the branch never change. Worktree replicas share their refs
  with the graph repository; a node with a `subdir` only covers its subdirectory.
- **Only on change, bounded.** Nothing is written when the tree equals the
  newest checkpoint of the replica; at most 20 checkpoints per replica are kept
  (`workspace.Manager.MaxCheckpoints`), older refs are deleted. A replica scope
  above 50 000 files or 512 MiB is not checkpointed (reported as a failure).
- **No Git repository, no checkpoint.** A plain folder is skipped (metric
  outcome `unavailable`); a folder inside some other repository is never
  checkpointed as that repository.
- **Restore.** A person restores from the task's *Checkpoints* panel
  (`POST /api/v1/graphs/{id}/checkpoints/restore`, administrators, explicit
  confirmation; preselected is the newest checkpoint recorded before an edit,
  "the last good one"). The agent restores through `checkpoint.restore` (and
  sees `checkpoint.list`) only with a grant naming the tool; wildcard grants
  never match, and read-only workspaces deny it. A restore first records the
  current state (reason `before_restore`), so it can be undone; then it deletes
  files the checkpoint lacks, prunes emptied folders and writes the files that
  differ. Ignored files, `.weft/`, the index, HEAD and branches are left
  alone, and a person's restore is refused while the replica runs a turn on
  this instance.
- **Failed turns** never restore anything: the workspace and its checkpoints
  stay as they are for inspection.
- **Safety.** Git runs on content agents influence, so checkpoints use only
  plumbing that runs no filters, hooks or programs: Go reads and hashes the
  files through `os.Root` (links are recorded as links, never followed), Git
  stores blobs through `fast-import`, builds the tree and moves refs, with
  `core.hooksPath=/dev/null`, `core.fsmonitor=false`, signing, automatic
  maintenance and lazy fetches disabled and no system or global
  configuration. Restores read blobs with `cat-file --batch` (no smudge
  filters) and write through `os.Root`, so a planted link cannot redirect a
  write outside the repository. Unlike read-only Git inspection, repository
  configuration is not rejected: the plumbing used never consults filter,
  diff or hook configuration.
- **Signals.** `checkpoint.created`, `checkpoint.restored` and
  `checkpoint.failed` events (actor, node, replica, execution), spans
  `checkpoint.record` / `checkpoint.restore`, and the metrics
  `weft_workspace_checkpoints_total{operation,outcome}` (`record`:
  `created`, `unchanged`, `unavailable`, `failed`; `restore`: `restored`,
  `rejected`, `failed`) and `weft_workspace_checkpoint_duration_seconds{operation}`.
  A failed recording is reported and the edit still runs.

## Code navigation (NAT-04)

`tools/builtin_code.go` and `builtin_code_map.go` add two read-only tools
under the `filesystem.read` capability, so they follow the same path grants
as `filesystem.read` and `filesystem.search` and need no new permission.

- **`code.outline {path}`** lists the symbols of one source file with line
  numbers: `L21-23 method Store.Get | func (s *Store) Get(key string) (int, error)`.
  Go is parsed with `go/parser` (functions, methods with receiver, structs,
  interfaces and their methods, other types, constants and variables, with end
  lines; a syntax error is reported with the symbols of the part that parsed).
  TypeScript/JavaScript, Python and Java use bounded line heuristics:
  top-level declarations and the direct members of classes. When counting
  blocks the scan skips string literals and comments, and JavaScript template
  and regular-expression literals and Java text blocks as far as a line scan
  can tell. It can still lose track (a regular expression it reads as a
  division, for example): a top-level declaration at column 0 then resets
  the depth, and the outline carries a `warning: heuristic desync near line
  N` instead of silently misplacing symbols. A negative depth, an
  unbalanced end or an over-long line also warn. Files that are not regular
  (FIFOs, devices), above 1 MiB (read through a bounded reader, so a growing
  file cannot pass), in other languages, or directories are tool errors that
  point to `filesystem.read`, `code.map` or `filesystem.list`. At most 400
  symbols and 20 000 lines per file.
- **`code.map {path?, maxTokens?}`** maps the source files under `path`
  (default the workspace): notices first (files left out by the budget, scan
  limits, skipped files by reason, approximate outlines), then a
  `Directories:` line with source file counts, then per directory the most
  important files with their top symbols
  (`store.go (7 lines): struct Store L3, method Store.Save L5`). Files are
  ranked by size plus how many other files mention their symbols; symbols by
  those mentions, size and kind. Mentions of a name defined in several files
  are divided among them, very short names and Python dunder names do not
  count, and test and generated (`// Code generated … DO NOT EDIT.`,
  `@generated`) files rank lower. Mentions are counted only for known symbol
  names, so memory follows the symbols, not every token. The text stays within
  `maxTokens` (four characters per token, default 1 000 so it fits the
  smallest native result cap, at most 8 000). The scan skips hidden
  directories, `.git`, `node_modules`, `vendor`, `.venv`, `__pycache__`,
  `build`, `dist`, `target`, `out`, `coverage`, `bower_components`, Python
  virtual environments (a `pyvenv.cfg`), `*.min.js`, and the simple name
  patterns of the root `.gitignore` (`dist/`, `*.gen.go`; path patterns and
  negations are not interpreted). It does not follow symlinks and stops at
  50 000 entries, 5 000 files or 48 MiB. Unreadable and over-512 KiB files are
  counted and reported, and set `truncated`; an unreadable root is an error.
  The native context manager's hint for a cut `code.map` result is "use a
  narrower path or a smaller maxTokens".
- **For prompts.** `tools.BuildCodeMapFor(ctx, gateway, principal, CodeMapOptions{MaxTokens: n})`
  returns the same map for a planning agent's system prompt. It is a
  `code.map` call through the gateway, so grants, task consent, rate limits,
  workspace preparation, audit events, metrics and traces apply. It runs
  under `tools.WithoutApproval(ctx)`: a call that would need a person's
  approval fails with `ErrDenied` instead of blocking prompt construction
  (the agent can still call `code.map` and have it approved).
  `BuildCodeMap` takes an explicit grant for internal use. NAT-10 decides
  where the planner prompt uses it.
- **`filesystem.search`** gains `literal` (exact text, no regexp escaping),
  `context` (0-10 lines before and after, grep-style `path-12- text` lines and
  `--` between groups) and a clearer `maxResults` (default 200). An empty
  pattern is refused, an invalid regexp suggests `literal`, and `vendor` is
  skipped like `.git` and `node_modules` unless `path` points inside it; an
  empty result names the directories it did not search. A file with a line
  above 1 MiB is reported as searched only up to that line.

Tool descriptions and argument names are short so small local models can
call them; the scripted evaluation is unchanged (4/4 scripted, 0/4 lazy).

## Verification loop (NAT-05)

Projects and workflows declare **checks**: a name (`test`, `lint`, `build`
or a custom lowercase name such as `web-test`), a command, an optional
workspace-relative `cwd` and `timeoutSec` (default 600, at most 1 800).
Workflows declare them in `spec.checks`; projects through
`PUT /api/v1/projects/{id}/checks` (admin, `If-Match` revision, event
`project.checks_requested`). At most 20 checks each, validated by
`domain.ValidateChecks`. Commands are visible to every viewer of the
workflow, the task review, approvals and the audit log: never put secrets
in a check command; read them from the sandbox environment instead.

```yaml
spec:
  checks:
    - name: test
      command: go test ./...
      cwd: server
    - name: lint
      command: go vet ./...
```

When a task is prepared, the project's checks are merged into the task's
execution copy (`domain.MergeChecks`: a workflow check replaces a project
check of the same name). The copy is a snapshot: editing the project later
does not change a running task. A combination over the bound, or a project
that no longer exists, is a review blocker.

**`check.run {"name": "test"}`** runs one declared check in the node's
sandbox and worktree. Only a grant matching the `shell.exec` capability
authorises it (`shell.exec`, `shell.*`, `*`): a grant named `check.run` or
`check.*` grants nothing, so the task authorization and delegation rules that
look for shell grants still see every agent that can run a check. It goes
through `shellRequest`, the same path as `shell.exec`: a command-restricted
grant runs the split argv without a shell and refuses shell operators, so a
check outside the grant is denied. It shares `shell.exec`'s rate-limit
allowance, and approval prompts and `tool.invoked`/`tool.completed`/
`tool.denied` audit entries show the resolved `{name, command, cwd,
timeoutSec}`, not just the name. It adds no privilege. The first and the
last 128 KiB of each stream are kept (test runners print failures and their
summaries at the end), and parsed into at most 20 failures (test, file, line,
message; messages cut at 300 bytes). A format is used only when it found
failures, so mixed output (Go passes, then pytest fails) falls through to the
parser that explains the failure:

| Format | Recognised |
|---|---|
| `go` | `--- FAIL` tests with their indented `file.go:N:` logs (with `-v`, logs are buffered per test and kept only when it fails, so passing tests' `t.Log` lines are not failures), compiler/vet `file.go:N:C:` errors, panics |
| `vitest` | `FAIL file > suite > test`, the first error line, `❯ file:N:C` |
| `jest` | `FAIL file`, `● suite › test`, the first error line, the stack location |
| `pytest` | `FAILED file::test[id] - message` (ids may contain spaces), `ERROR file`; the line comes from the traceback in that test's own `____ test ____` section |
| `generic` | the first lines containing error/fail/fatal/panic, ignoring counts such as "0 failed" (with `file:N[:C]` when present), else the last 15 lines |

The model reads one compact block, e.g.

```
check "test" failed (exit 1, 2.3s): 1 failure(s) [go]
- TestAdd calc_test.go:9: got 4, want 3
```

A passing check is one line. A failing check is a successful tool call (not
`isError`); an unknown check name returns the declared names. The model only
sees `check.run` when it holds `shell.exec` and the task declares checks; the
schema enumerates the declared names, which keeps it small for local models.
The tool output cap of NAT-02 applies to it like to `shell.exec`. When a
docker sandbox fails to run the command at all (`docker exec` exit 125), the
run is an infrastructure error ("could not run"), not a test failure.

**Workspace fingerprint.** Before the command runs, `check.run` records the
code state in the result's `workspace`: `head` (the commit), `state` (a hash
of `git status`, the diff against HEAD and the contents of untracked files),
and `checkFilesChanged`: test and check-definition files (`Makefile`,
`package.json`, `pyproject.toml`, `conftest.py`, `*_test.go`,
`*.test.ts`, `test_*.py`, runner configs, …) changed since the task's base
commit (graph label `weft.base`, or HEAD outside project tasks),
committed or not. A pass with changed test files says so to the model and
on the task. The git probes run read-only in the sandbox with hooks,
fsmonitor, external diff and textconv disabled; what they cannot establish
(not a repository, an oversized diff, over 500 untracked files) is marked
`partial`. Check output that is not git-ignored changes the fingerprint of
the next run, so ignore build output.

**Evidence.** The gateway records each check that executed as a
`check.completed` event (graph, node, replica, execution, trace, and the
structured result: check, command, passed, exit code, timed out,
infrastructure error, duration, format, failures, failure count, truncated,
workspace). Denied or unknown checks are not evidence; a result that cannot
be decoded is logged and counted (`weft_check_evidence_dropped_total`).
The task view loads the task's declared checks and lists every one: "not
run", or the latest run per agent replica (filtered to the selected agent
workspace), passed, failed, timed out, could not run, or **stale** when the
same worktree changed after the run (a `workspace.observed` change by another
tool or from outside, or a later check that saw a different fingerprint). It
says "All N declared checks passed on the current workspace" only when every
declared check has a passing, non-stale run, warns about changed test files,
and refreshes every 15 seconds or on request.

**Signals.** `weft_check_runs_total{kind,outcome}` (kind `test`, `lint`,
`build` or `custom`, so custom names never become label values; outcome
`passed`, `failed`, `timed_out`, `infra_error`, `denied` by the command
grant, `unknown`, `error`; a call refused by the gateway before the tool runs is counted only
in `tool_invocations_total`) and
`weft_check_run_duration_seconds{kind}` for checks that executed, next to
the existing `tool_invocations_total{tool="check.run"}` and the `tool.check.run`
span. A check's workspace changes (build output) are observed like
`shell.exec`'s.

Not done here: a web editor for project checks (the API and workflow YAML
are the ways to declare them), and role prompts that tell agents to run
checks before finishing (NAT-08).

## Software Factory v2 (NAT-10)

A local install seeds `software-factory-v2` next to the original workflow
(`localinstall/software-factory-v2.yaml`, its own seed marker, so existing
installs get it once and a deleted copy is not recreated):

| Agent | Role skill | Tools | Emits |
|---|---|---|---|
| Planner | `weft-planner` | `filesystem.read` (also `code.map`, `code.outline`, search), `git.read` | `task.ready` |
| Implementer | `weft-implementer` | read, write (snippet and patch edits), `git.read`, `shell.exec` (so `check.run`) | `code.ready` |
| Reviewer | `weft-reviewer` | read, `git.read` | `review.approved` or `review.changes_requested` |
| Verifier | `weft-verifier` | read, `git.read`, `shell.exec` (so `check.run`) | `verify.passed` or `verify.failed` |
| Review handoff | none | read, `git.read` | final summary; SCM hand-off when configured |

- Only the implementer edits. The rework loops (review → implementer and
  verify → implementer) stop after 30 hops.
- `verify.passed` reaches the hand-off only through a human approval.
- Checks come from the project or from the workflow's `spec.checks`. Without
  declared checks, `check.run` is not offered and the verifier can only
  report that nothing was verified.

**Measured on the evaluation suite.** `make eval-local EVAL_WORKFLOW=factory`
runs each case through the factory:
- every agent uses the measured model;
- `go test ./...` is the declared `test` check;
- shell grants are limited to `go` and `gofmt`;
- the hand-off gate is open.

A run counts only when the hidden tests pass and the hand-off agent finished.
It stops early when every agent has been idle for 15 seconds without
reaching the hand-off. `TestSoftwareFactoryV2SolvesTheScriptedCases` runs the
same workflow with a scripted model that plays each role in CI. It solves 4/4
cases, and both test checks of each case are recorded as `check.completed`
evidence.

## Boundaries that do not change

- Every tool still goes through the tool gateway with explicit grants;
  `check.run` is a shell call under the node's command grants, not a new
  privilege.
- Repository guidance stays opt-in (skills and project steering); nothing
  reads `AGENTS.md` automatically.
- Compaction summaries are model output: they are kept as conversation, never
  as instructions with more authority than the task.
- Budgets, approvals and task authorization apply to compaction calls and
  checks like any other call.
- Checkpoints are platform records, not agent privileges: an agent restores
  only its own workspace and only with an explicit `checkpoint.restore` grant;
  a person's restore is an audited administrator action.
