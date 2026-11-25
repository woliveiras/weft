# Guided operator journeys evidence

## Basis and review

- 2026-11-25: maintainer approved specification and implementation of the UX audit,
  with one commit per stage. Source audit covered all twelve docs/features journeys
  and console/configuration code. No new usability study or timing baseline exists.
- Specification and matrix created before the new implementation. Tests extended
  after implementation are implementation-aware checks of the pre-existing
  spec-derived oracles; they are not an independent blind implementation.
- Independent specification review: workflow_creation identified that run mode
  belongs to the workflow instance, not the shared catalogue definition. AC-005
  was clarified before implementation. agent_experience reviewed AC-006/007 and
  found them implementable with explicit browser stay/discard confirmation.

## Implementation and independent review

- `e4661a0`: specification and matrix, before implementation.
- `b79a40c`: optional graph task-default contract, validation, schema/serialization,
  API admission and workflow editor. Existing documents retain legacy inference.
- `96b5639`: shared/instance/origin scope, explicit version adoption, unpublished
  edit guards and reviewed skill-association saves. Independent review identified
  a new-agent ID race during publication; the field is now locked while saving,
  with a pending-publication regression test.
- `628cb83`: guided workflow creation with editable proposals and advanced import.
  Independent review found asynchronous file reads could replace a newer edit;
  generation checks and a delayed-file integration regression protect the draft.
- `6767596`: contextual task setup, stable save-and-review action, readiness facts,
  goal guidance and save-before-configuration handoff. Independent review found
  late discovery could discard project context after typing or override explicit
  blank SCM/environment choices. Four failing regressions were observed before
  the correction; discovery retries now preserve both context and explicit edits.
- Review also identified configuration links that bypassed the save handoff and a
  new-to-saved route transition that lost the review display. Editor repair paths
  now await save acknowledgement; App-level integration verifies that returning
  to a saved review never interprets or starts the task.
- `9d4f104`: browser keyboard validation identified review focus scheduled before the DOM
  commit. A React effect now focuses the mounted review heading; component and
  browser assertions inspect the actual focused element.

## Verification

All commands run on 2026-11-25. Go and web execution used Docker; Python used the
repository's stdlib test commands.

| Check | Result and scope |
| --- | --- |
| `./scripts/go.sh test ./...` | Passed: default Go unit/integration suite, including task-default validation, graph round-trip and API/CAS/role coverage |
| `make lint` | Passed: gofmt check and Go vet |
| `make build` | Passed: repository binary build targets |
| `make test-python` | Passed: 16 SDK, 14 example-agent and 10 script tests |
| `tsc --noEmit` and `pnpm test` in `node:22-bookworm` | Passed: 760 web tests, including helper units, React/API-client integration and navigation tests |
| Focus correction: `WorkflowCreate.test.tsx` | Passed after the final focus change: 7 tests |
| Chromium workflow/task-prompt regression subset | Passed after the focus change: 9 tests, including guided creation, saved task review, keyboard focus and desktop/390px layouts |
| `tsc --project tsconfig.e2e.json` and Chromium seven-file regression pack | Passed on the final tree: 34/34 tests in 54.6 seconds |
| Web production build (`tsc` + Vite), disposable E2E Go fixture build | Passed |
| `docker compose up -d --build --no-deps weftd` | Passed using the standard Dockerfile; local service healthy |
| Local HTTP smoke | `/healthz` returned `{"status":"ok"}`; index served current hashed JS/CSS; JS includes the new task summary, review and return actions |
| Documentation link check and `git diff --check` | Passed |

The web commands mount `leforge-web-node-modules` and use
`npm_config_registry=https://registry.npmjs.org`. Browser tests use the local
`weft-cloud-api-token-e2e` image, disposable backend and deterministic fixtures;
they exercise UI/HTTP collaboration without real provider credentials. The final
browser command ran `tsc --project tsconfig.e2e.json && playwright test
workflows.spec.ts agent-catalog.spec.ts field-help.spec.ts instructions.spec.ts
task-prompts.spec.ts task-drafts.spec.ts task-request-layout.spec.ts` with
`WEFT_E2E_PROMPT_MODEL=1` and a disposable backend port.

Earlier browser rounds exposed stale button/link selectors and two test waits
that observed hidden controls before save acknowledgement. Selectors now assert
the intended status, and workflow creation waits for the saved destination before
navigation. One earlier lifecycle run did not dispatch its Stop click; the
unchanged lifecycle assertion passed subsequent runs, including the final pack.
This is recorded as an intermittent test observation, not proof of a production
Stop defect or universal flake-free execution. The production build retains its
pre-existing bundle-size warning.

### Oracle coverage map

| Matrix rows | Evidence anchors |
| --- | --- |
| J01–J02 | `taskEntryContext.test.ts`, `TaskEntryFlow.test.tsx`, `routes.test.ts`, `App.navigation.test.tsx` |
| J03–J04 | `TasksPage.test.tsx`, `TaskEntryFlow.test.tsx`, `App.navigation.test.tsx`, `e2e/task-prompts.spec.ts` |
| J05–J06 | `server/internal/graph/task_defaults_test.go`, `server/internal/api/task_defaults_test.go`, `taskDefaults.test.ts`, `WorkflowTaskDefaults.test.tsx` |
| J07–J09 | `TaskReadiness.test.tsx`, `TaskEntryFlow.test.tsx`, `TaskRequirementRepairs.test.tsx`, `taskEntryContext.test.ts` |
| J10–J11 | `workflowCreation.test.ts`, `WorkflowCreate.test.tsx`, `e2e/workflows.spec.ts` |
| J12–J13 | `agentJourneyModel.test.ts`, `AgentJourneys.test.tsx`, `App.navigation.test.tsx`, `e2e/agent-catalog.spec.ts` |
| J14 | `SkillsView.test.tsx`, `e2e/instructions.spec.ts` |
| J15 | TypeScript, full regression suites, actual keyboard focus and desktop/390px browser assertions |

## Documentation decision

- Updated task, workflow, agent, skill, project and environment journeys and their
  source/test anchors in docs/features. Added `setup-and-readiness/README.md` for
  configuration facts, goals, MCP/SCM/Jira boundaries and safe repair/return.
- The guides record concrete pins, explicit/default precedence, separate saves,
  preparation and consent, and remaining configuration limits. No claim that a
  configured resource or an agent's completed checklist proves runtime success.

## Residual limitations

### Local follow-up — 2026-11-25 10:28–10:30 UTC

The operator reported unavailable Activity sections and an offline event stream.
An authenticated request reproduced all five section errors. Server logs showed
PostgreSQL refusing connections in recovery mode; PostgreSQL logs identified
`No space left on device` during its recovery checkpoint. The Docker/Colima
filesystem was 100% full despite free host disk space. The preceding liveness
and asset smoke checks were insufficient to establish ongoing store readiness.

Using the operator's prior authorization for cache/image cleanup, unused build
cache was pruned (5.529 GB reported), with dangling images reclaiming a further
4.876 MB. No volume or database/workspace data was deleted, no credentials changed
and no schema or application code changed. PostgreSQL recovered automatically.
Verification: database container healthy, `pg_isready` accepting connections,
`/readyz` HTTP 200 with storage/queue/sandbox ok, authenticated Activity with
`errors: []`, and event-stream HTTP 200 with `text/event-stream`. About 5.5 GB
was free afterward; VM capacity remains an operational consideration before
another large build. The recovery procedure is recorded in `docs/operations.md`.

Production qualification and real-account execution are outside this UI refactor.
No human usability study, completion-time comparison or training reduction was
measured. Task goals provide guidance; they do not provision credentials or
silently combine integrations. Historical inline origins are not migrated into
live-linked definitions. Live broker, kernel, sandbox-hardening and paid-model
qualification targets were not rerun: their implementation boundaries did not
change. Existing admission, required approvals and gateway policies still apply.

## General task launch follow-up — 2026-11-25

### Intent and review provenance

The maintainer clarified that starting tasks is confusing in general, including
new tasks, saved drafts and retries. Specification commit `0120f45` changed
AC-002/004 and rows J03–J04/J16–J18 before implementation. The normal editor now
keeps request, workflow and repository visible, with one primary **Run task** and
an alternative **Save draft**. Advanced configuration remains available and opens
when required settings are missing. Optional model interpretation is separate.

Run saves exact input, checks the acknowledged revision and compares fresh
requirements with previously displayed scope. Newly discovered scope requires
another explicit Run click. Saved complete drafts inspect readiness using GETs;
loading a page, saving a draft or completing an environment build never starts
agents. Ask remains the default without manufacturing an authorization record;
Full/Workspace still require separate current consent, and mandatory approvals
are unchanged. Preparing or retrying a build retains an explicit build decision.

Independent editor/environment reviews found and corrected: mismatched task
identity/input in inspection or save acknowledgements; empty authorization scope
without actionable feedback; budget holds that left an inert enabled Run button;
prepared build retries incorrectly gated on the draft-only preparation flag;
and an inherited repository URL missing from the prepared-task review. The last
case now reads and verifies the pinned workflow version before offering Run,
and workspace changes invalidate the displayed launch identity. Authorization
editing is unavailable during a launch operation. No reviewer found an
outstanding authorization bypass after these corrections.

Helper unit tests cover input/scope/budget/environment/workspace identity.
React/API-client integration covers saves, failures, stale revisions and scopes,
late responses/edits, duplicate clicks, budget failures and read-only build-ready
inspection. Browser oracles exercise a disposable backend through real HTTP,
including mandatory approvals, all three authorization modes, fresh retry drafts,
imports, prompt recovery and explicit build/start separation. These tests were
written with implementation awareness against the previously recorded criteria.

### Verification observations

An initial browser pass ran concurrently with another Vite build targeting the
same generated directory; one page loaded without its generated assets. The
serialized 33-test browser pack subsequently passed. A full web suite started
while final source/test edits were still being applied reported two transient
assertion mismatches and one unrelated 5-second shared-resource test timeout
under simultaneous browser load. Final checks use a frozen tree and bounded
workers; test expectations, timeouts and security checks were not weakened to
hide those observations.

Before the local build, unused Docker build cache was pruned under the operator's
existing cleanup authorization (703.2 MB, then 49.16 GB reported). About 49.5 GB
was available afterward. Volumes, databases and task/workspace data were not
removed. `/readyz` reported storage, queue and sandbox ready before deployment.

The operator's dependency-security task is outside this UI validation: no
prepare/start or external repository action was dispatched for it. No new human
usability study or measured reduction in task completion time is claimed.

### Final checks and implementation

- Implementation commit: `1d4000a` (`feat(console): coordinate task launch
  through a single run action`).
- Docker Node 22: `pnpm exec tsc --noEmit` and `pnpm test --maxWorkers=1`
  passed, 130 files / 817 tests. A preceding two-worker attempt reported an
  intermittent SkillsView assertion and a task-authorization copy assertion;
  the latter was corrected to preserve actionable field guidance. The serial
  run passed without changing SkillsView or its checks.
- The final channel-JSON validation refinement and guidance copy were checked
  with `tsc --noEmit` and the `taskDefaults`, `TaskLaunchFlow` and `TasksPage`
  suites: 56 tests passed. Invalid channel JSON opens Advanced settings,
  preserves input and dispatches neither save nor start; direct instruction
  does not require a residual payload to parse.
- Docker Chromium / real disposable backend: `tsc --project tsconfig.e2e.json`
  and Playwright's `task-launch`, `task-retry`, `task-authorization`,
  `task-request-layout`, `task-drafts`, `task-prompts`, `repository-imports`
  and `projects` pack passed, 33 tests. This run preceded the final local JSON
  validation refinement, which has focused helper and React/API-client coverage.
- The 1440 px and 390 px launch review captures were inspected. Scope, budget,
  permissions and access controls remain readable without horizontal overflow.
- Independent final read-only review approved the changes, including the JSON
  refinement, with no outstanding findings; it did not claim additional runs.
- Documentation relative links and `git diff --check` passed.

No Go source or server contract changed in this follow-up. Full Go/Python and
live sandbox qualification suites were not rerun; the real-HTTP browser fixture
and standard local deployment compile the server. The standard Docker build also
type-checks and bundles the console. Vite reports the existing main-chunk size
warning (over 1200 kB); this is not a build failure or a measured UX performance
claim.

Local deployment completed using `docker compose up -d --build --no-deps weftd`
with the standard Dockerfile and no override or copied binary. `/healthz`
returned `status: ok`; `/readyz` returned storage, queue and sandbox `ok`.
The generated JS/CSS assets returned HTTP 200, and the served main bundle
contains the coordinated launch UI. Deployment replaced only `weftd` and did
not submit the operator's task.
