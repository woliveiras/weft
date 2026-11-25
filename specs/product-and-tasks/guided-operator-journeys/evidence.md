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
