# TASK-002 historical evidence

These results were recorded at the time of the run. The documentation migration did not rerun them.

- 2026-10-17. Navigation and visual consolidation were activated locally under historical labels b0f7670 and 7ef1178. The rebuilt service retained three graphs, nine saved versions and its four mounts. Browser checks covered links, help, drafts and the Factory's ten rendered edges before and after reload.
- 2026-10-17. The developer reached agent settings and supplied graph and event screenshots. The round was partial; UX-01–05 later passed automated checks and were activated.
- 2026-10-17. UX-01–05 recorded 241 frontend tests, lint/build and 48 main Chromium scenarios plus one hosted CIMD case. Historical activation 322f3bd verified click targets, return links, event wrapping and automatic discovery of three DeepWiki tools. An unrelated sandbox journal supervision check failed twice before passing the diagnostic and final runs.
- 2026-10-18. The onboarding implementation had previously been activated as 9b8d1e8. Removal a2b33b3 rebuilt web assets and recreated only weftd; the backend binary hash, volume mounts, schema versions 1–2 and existing graphs were preserved. Browser inspection covered Models, Graphs, agent configuration and Connections.
- Before withdrawal on 2026-10-18. The original installed guide was collapsed initially and preserved its reviewed count and topic across navigation and reload. The record cites 51 automated E2E scenarios. This is historical evidence of a withdrawn feature.
- 2026-10-18. Removal verification recorded Go/Python and 272 frontend tests, lint, build and seven targeted Chromium journeys as passing. Historical commit labels before the flattened root are descriptive references.

- Field help passed 467 web tests and 14 Chromium scenarios covering workflow
  and agent configuration, MCP connections, and installation policy. Keyboard
  activation and Escape preserved a workflow draft. Viewers could read
  installation policy help without gaining write access. The local console
  also confirmed the Runtime popup fits the viewport and returns focus to its
  help control when closed.

## Approval inbox and fixed Configuration — 2026-11-19

- `make test-web`: 494 tests passed across 93 files, with TypeScript checking.
- Docker Chromium runner: 18 scenarios passed across `approvals.spec.ts`,
  `work-navigation.spec.ts`, `environments.spec.ts`, `delegation.spec.ts`,
  `delegation-external.spec.ts` and `release-one-journey.spec.ts`.
- Inbox coverage includes combined filters, request-time descending order,
  dialog dismissal with Escape and focus return, persisted decision feedback,
  read-only viewer inspection and API rejection of viewer decisions, narrow
  viewport containment, existing environment approval/rejection and native and
  external delegation approval flows. Component tests cover pagination,
  missing historical dates, invalid answers, decision conflicts and independent
  source failure/recovery.
- `docker compose up -d --build --no-deps weftd` completed using the standard
  Dockerfile. The service reported healthy and `/healthz` returned `ok`.
- Read-only local browser inspection confirmed a fixed Configuration heading
  and divider, the approved history with 48 records split into three pages,
  the latest request first and aligned filter controls. No live request was
  approved or rejected during that inspection.
- These checks do not claim assistive-technology or cross-browser conformance.

- Follow-up navigation refinement: Projects moved from Work to Configuration;
  the 18 Chromium scenarios passed again, including section membership, route
  navigation and the active Projects marker.
- Additional real-preview browser checks could not reach the review gate:
  `fixture-preview` stayed in `starting` with no execution or review after
  startup. Read-only inspection found the installation's two code-review
  containers using 1 CPU and 1 GiB each on the shared dedicated daemon; the
  disposable preview fixture admits at most two containers, 1 CPU and 512 MiB.
  This is consistent with capacity contention before execution. Live workflow
  containers were left running. Real-preview lifecycle coverage remains pending
  in an environment with available capacity; the regular 18 browser scenarios
  above use a disposable backend and do not claim real preview verification.

## Workflow catalogue — 2026-11-19

- `make test-web`: 501 tests passed across 95 files, including TypeScript
  checking. Presentation tests cover update ordering, stable ties, missing dates,
  search/status combination and execution-copy exclusion. Component tests cover
  paging, agent expansion, loading/error/empty states, read-only controls,
  in-flight action locking, retry and deletion cancellation.
- Docker Chromium runner: 16 functional scenarios passed across
  `workflows.spec.ts`, `ux-feedback.spec.ts`, `work-navigation.spec.ts`,
  `graph-navigation.spec.ts`, `agent-catalog.spec.ts`, `field-help.spec.ts` and
  `onboarding.spec.ts`. The workflow scenarios exercised the real disposable
  backend for Start/Pause/Resume/Stop, persisted versions, viewer API denial,
  deletion confirmation and recovery from action/list failures. Existing draft
  navigation, agent reuse and deferred-cleanup assertions remain intact.
- A narrow-screen check found offscreen assistive text escaping its table scroll
  container. Giving that container a positioning context fixed the overflow.
  All 16 scenarios passed after that correction; the three workflow scenarios
  passed again after the final header-help alignment refinement.
- Workflows visual baselines were regenerated and visually inspected in light
  and dark themes. Both baseline comparisons then passed against another fresh
  backend. Dynamic workflow update times are masked in visual comparisons;
  semantic date ordering remains covered separately by presentation tests.
- `docker compose up -d --build --no-deps weftd` completed with the standard
  Dockerfile. The service was healthy and `/healthz` returned `ok`. Read-only
  local browser inspection confirmed four workflows, newest updated first,
  aligned filters and the Status help icon, and expandable code-review agents.
  No live workflow was started, stopped, paused or deleted for visual inspection.
- The change is confined to frontend presentation, associated tests and docs.
  Go/Python test suites and real-preview lifecycle tests were not rerun; the
  standard local image build compiled the Go binaries. These observations do not
  establish cross-browser or assistive-technology conformance.


## 2026-11-19 — Canvas and compact navigation refinements (AC-001, AC-002, AC-004)

- The workflow catalogue work was committed separately as `b901a82`.
- `make test-web` passed type checking and 510 tests in 96 files, including
  named creation, whitespace rejection, retained form values, cancellation/focus,
  deferred activity requests, output loading/error/retry, draft-only nodes,
  read-only inspection, contextual channel removal, icon-rail persistence and
  browser-storage failure. Final log: `/tmp/weft-canvas-all-web-final.log`.
- Docker Chromium covered 25 distinct functional scenarios across
  `canvas-panel.spec.ts`, `graph-connections.spec.ts`, `graph-navigation.spec.ts`,
  `agent-catalog.spec.ts`, `agent-configuration.spec.ts`, `recovery.spec.ts`,
  `ux-feedback.spec.ts`, `work-navigation.spec.ts` and
  `visual-consolidation.spec.ts`. The combined run passed 24 scenarios and exposed
  an overlapping-handle test arrangement in the remaining drag scenario. The
  arrangement now leaves a visible gap and asserts separation before connecting;
  no forced pointer action was introduced. Both connection/removal scenarios then
  passed twice (four checks), including the real API persistence boundary. A
  repeated fixture name was made unique for repeat runs. Logs:
  `/tmp/weft-canvas-functional-final.log` and
  `/tmp/weft-canvas-connections-verified.log`.
- The existing suspension/recovery journey was updated to expand Replicas and
  select Messages before using their controls. Its five scenarios passed. A
  separate eight-scenario run covered compact navigation, draft retention,
  accessible names and hints, legacy routes, narrow layouts and canvas help.
- Ten light/dark visual baselines were regenerated, inspected and compared
  successfully without snapshot updates in a fresh backend run. Log:
  `/tmp/weft-canvas-navigation-visual-verified.log`.
- `docker compose up -d --build --no-deps weftd` completed with the standard
  Dockerfile; the final service was healthy and `/healthz` returned `ok`.
  Local browser inspection confirmed the compact menu, separate draft/execution
  controls, contextual Remove, sectioned inspector and aligned creation form.
  Creation was cancelled without editing the live graph; no lifecycle controls
  were used during browser inspection. The layout preference was changed only
  in the inspection browser. Screenshots: `/tmp/weft-canvas-compact.png` and
  `/tmp/weft-canvas-add-agent.png`.
- The implementation changes frontend presentation only. Go/Python suites were
  not rerun; the standard image build compiled the Go binaries. Chromium checks
  and keyboard inspection do not establish cross-browser or assistive-technology
  conformance.

## 2026-11-19 — Models configuration pages (AC-001, AC-002, AC-004)

- `make test-web` passed TypeScript and 517 tests in 97 files. Coverage includes
  section route round trips, provider filters, direct editor loading/retry,
  viewer denial, unavailable plugin guidance, retained credential semantics,
  usage ordering/search/pagination and disabled writes after pricing load errors.
  Log: `/tmp/weft-models-web-final-pass.log`. One concurrent run encountered an
  unrelated five-second SharedEnvironments cleanup timeout; the unchanged suite
  passed on rerun with its existing timeout and assertions.
- Docker Chromium passed 14 functional scenarios across costs, navigation,
  work navigation and visual consolidation. Existing real-backend budget
  raise/recovery and permission assertions remain intact. Log:
  `/tmp/weft-models-browser-final.log`.
- The isolated provider-management suite passed all three scenarios: full-page
  creation/editing, refresh/back navigation, combined filters, narrow-screen
  geometry, viewer API denial, real synthetic upstream success/failure,
  sanitized responses, retained edits after a 503 save failure and successful
  persistence without resupplying the stored key. Provider activation is kept
  in this separate disposable backend because it reloads the model registry.
  Log: `/tmp/weft-models-provider-final.log`.
- Both Models visual baselines were regenerated, inspected in light/dark themes
  and compared successfully without updates against another fresh backend.
  Log: `/tmp/weft-models-visual-verified.log`.
- `docker compose up -d --build --no-deps weftd` completed using the standard
  Dockerfile. The container is healthy and `/healthz` returned `ok`. Read-only
  local browser inspection confirmed the provider-first landing page, dedicated
  creation page, aligned limits, usage summary/table and separate budget/price
  forms. Creation was cancelled; no live credentials, plugins or budgets were
  changed, and no paid model test was invoked. Screenshots:
  `/tmp/weft-models-overview.png` and `/tmp/weft-models-provider-form.png`.
- No backend code changed. Go/Python suites were not rerun; the image build
  compiled Go binaries. Chromium checks do not establish cross-browser or
  assistive-technology conformance.


## 2026-11-19 — Explain and edit active budgets (AC-004)

- Monthly cards show the UTC period and separate limit/spent/remaining values.
  Administrators open an inline definition editor; cancellation makes no write
  and restores focus. Default per-task ceilings and inherited warning thresholds
  save independently. Unavailable balances/definitions disable writes without
  claiming zero spend; recoverable save errors retain input. Removal preserves
  history and has an explicit confirmation step.
- `make test-web` passed TypeScript and 524 tests in 97 files. After the final
  focus-restoration adjustment, TypeScript and all 19 cost/component tests passed
  again. Logs: `/tmp/weft-budget-web-final.log` and
  `/tmp/weft-budget-web-focused.log`.
- Docker Chromium passed nine costs/task-journey/task-outcome scenarios. The new
  scenario executes a deterministic in-process model in a task execution copy,
  creates a real monthly ledger, edits its definition through the UI and checks
  persisted values, unchanged spend, no policy write, retry after 503, keyboard
  focus, reload, viewer API denial and narrow-screen geometry. Existing paused
  resume, insufficient raise and invalid-ledger assertions remain intact.
  Log: `/tmp/weft-budget-browser-final.log`. Three separate provider/navigation
  scenarios also passed: `/tmp/weft-budget-model-navigation.log`.
- The live-backend test exposed a stale healthy-ledger read after saving a new
  definition. The read path now applies existing admission precedence, including
  explicit/legacy Raise floors, without rewriting accounting. Cost, API and
  engine tests passed in Docker; see the INT-004 evidence and
  `/tmp/weft-budget-go.log`. `make lint` passed (`gofmt` and `go vet ./...`).
- The standard `docker compose up -d --build --no-deps weftd` build completed;
  the container is healthy and `/healthz` returned `ok`. Local browser inspection
  confirmed the balance facts, prefilled inline editor and independent defaults.
  Editing was cancelled without changing live limits. Screenshots:
  `/tmp/weft-budgets-overview.png` and `/tmp/weft-budgets-inline-editor.png`.
- Other Go packages and Python suites were not rerun. Browser verification was
  Chromium; no cross-browser or assistive-technology conformance is claimed.
