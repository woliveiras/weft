# TASK-005 historical evidence

These results were recorded at the time of each run. The documentation migration did not rerun the historical checks.

- 2026-10-19. The durable draft increment passed SQLite/filesystem reopen, API, four Chromium cases and the historical full test/lint/build checks with 300 frontend tests.
- 2026-10-19. Final PROMPT-07 recorded 364 Vitest tests in 73 files, TypeScript and production web build, 30 Chromium journeys and nine disposable native journeys. Seven Chromium cases were rerun after consent display fixes. Docker-backed make targets could not run; the record reports passing host Go alternatives and Python suites.
- 2026-11-18. The Chromium regression confirmed that Tasks exposes the standard New task link and opens a previously saved echo draft. The draft recorded no executions before reviewed admission and retained its successful outcome after reload.
- 2026-11-18. The Tasks table checks distinguished saved drafts with identical titles in Chromium, verified the workflow and status filters, and opened the row selected by task ID at a narrow viewport. The component test checked observed status, issue search, and cursor navigation with active filters.

## Incomplete interpretation recovery — 2026-11-24

- Observed local draft `tsk_mwufd0rk4ba84df1caa658cbb97d` retained
  `invalid_proposal`, workflow version 0 and no entry agent. The old authorization
  panel translated its 409 into a misleading workflow-version-change explanation.
- TASK-005 AC-002 regression repair names missing bindings and opens/focuses
  Review settings without changing inputs. Unsaved/incomplete configuration
  withholds consent controls. A manually saved draft is reviewed without another
  model submission. Requirements from another saved revision are ignored, including
  a late initial review arriving after repair. Incomplete requirements do not claim
  a default runtime or isolation as reviewed facts.
- Unit checks cover missing bindings, review errors/loading and confirmation reset.
  Component/client integration exercises manual recovery with HTTP status handling,
  CAS saving, preserved request/project/base/environment, no submission/start/consent
  writes, and a delayed stale review response.
- Docker `web-source`: TypeScript check and final focused Vitest suite passed
  (39 tests); `pnpm run build` passed. Full console suite passed (117 files,
  681 tests) before the final requirements/race guard; focused tests and build
  passed again after those changes. One initial full-suite run had an unrelated
  Skills history timing failure; it passed both isolated and in the repeated full
  suite. No test or check was weakened.
- Independent subagent review found no remaining blocking findings. Standard
  Compose rebuild updated local weftd; no authorization, task execution or remote
  remediation was performed as part of the repair. Backend contracts were unchanged;
  Go/Python suites and the complete browser E2E suite were not rerun.

## Visible task setup and draft deletion — 2026-11-24

- AC-002 now presents three visible sections: Describe the work, Workflow &
  repository, and Input & delivery. Required choices remain visible after an
  invalid interpretation. Project choices distinguish repository, reference and
  pinned commit; failed discovery can be retried without dropping saved input.
  Desktop fields align in two columns and stack at narrow widths. Manual recovery
  has 16 px of space above and below its configuration button.
- Follow-up formatting checks align the optional issue-source legend and service
  field with input delivery, and keep at least 12 px between save/run/review
  actions, including wrapped rows. The focused Chromium geometry case passed at
  desktop and 390 px widths, alongside TypeScript and the production build.
- AC-004 deletion requires operator/admin access, explicit confirmation and the
  current positive revision. Only drafts without an execution copy or environment
  approval are eligible. A minimal deletion marker prevents stale saves and late
  interpretation/submission responses from recreating the draft. Other projects,
  imports, workspaces, approvals and execution history are retained.
- Unit and API/storage integration tests cover eligibility, permissions, strict
  confirmation, stale revisions, audit failure, races against save/preparation,
  replay prevention, pagination and filesystem/SQLite reopen. UI/client checks
  cover cancel/focus, pending requests, conflicts and uncertain network outcomes.
- Docker validation passed: full Go suite (`./scripts/go.sh test ./...`),
  `make lint build test-python`, TypeScript, all 694 Vitest tests in 118 files,
  and the production web build. After the final title alignment refinement,
  TypeScript and 40 focused tests passed again. An initial unrelated engine
  cancellation race passed three focused reruns and a repeated full Go suite;
  initial web timing failures passed in the repeated full suite. No checks were
  weakened.
- Chromium passed all 23 task draft, prompt, journey and layout cases with
  `WEFT_E2E_PROMPT_MODEL=1`, using a deterministic provider and disposable backend.
  Cases include explicit admission, completed-copy shutdown, active cancellation,
  approval decisions, draft deletion/cancel, manual repair and responsive geometry.
- Independent subagent review found no blocking findings. The standard Compose
  rebuild updated local weftd and it reported healthy. Read-only browser validation
  confirmed the visible configuration and recovery spacing on the existing local
  draft; no user draft was deleted and no remediation task was started.

## Continue-task feedback regression — 2026-11-24

- AC-003 regression: Continue could return an unchanged invalid draft while the
  action card still displayed only Draft saved. It now announces pending
  submission, disables repeat clicks, and explicitly reports that a returned draft
  has not started. Missing bindings or server blockers appear beside the action;
  Complete task setup focuses visible configuration. Editing, saving, resolving
  or reviewing clears old feedback. Transport failure ends loading, keeps the
  request/replay key and presents the error. Admission and consent are unchanged.
- TypeScript and 34 focused helper/component-client tests passed in Docker,
  including a delayed response, blocked setup, focus repair and HTTP 503 retry.
  The full console suite also passed: 696 tests in 118 files. Production build
  and E2E TypeScript checks passed.
  All 21 Chromium draft, prompt and layout scenarios passed against a disposable
  backend with deterministic interpretation enabled. Repeating a blocked Continue
  kept the same task and recorded no execution before clarification.
- Independent subagent review found no blocking findings. Standard local Compose
  rebuild completed and weftd reported healthy; no user task was started or edited
  as part of validation. Backend code was unchanged in this follow-up, so the
  earlier full Go/lint results above were not rerun for this UI correction.

## Missing task configuration fields — 2026-11-24

- AC-003: the existing missing-configuration helper also identifies individual
  workflow, pinned version, entry agent, channel message type and payload fields.
  A returned incomplete draft shows the border, associated explanation and
  aria-invalid on the affected controls. Correcting one value clears its own
  error. A new untouched task stays neutral; Project remains optional and an
  unselected version is described as Version not selected. Continue feedback,
  configuration repair, authorization and admission retain their existing gates.
- TypeScript and 38 focused helper/component tests passed first; the complete
  final console run passed all 705 tests in 119 files and the production build.
  Task prompt/layout Chromium cases passed with a disposable backend. See the
  shared form-feedback validation logs and review recorded in TASK-002's
  Configuration field feedback entry.
- Standard local Compose rebuild is healthy. Read-only inspection of the
  existing incomplete draft confirmed workflow version and entry-agent messages
  and borders; no task values were saved, deleted or submitted for execution.


### Workflow version dropdown and retained manual repairs — 2026-11-25

- Maintainer regression: correcting missing workflow version and entry agent,
  then pressing Continue task, restored the incomplete proposal. The console
  resubmitted PromptInput, whose interpretation path intentionally clears the
  old bindings. Existing draft continuation now saves complete TaskInput through
  the revision-checked draft endpoint and reviews the saved revision. Incomplete
  input stays editable; reinterpretation is an explicit Resolve request action.
- The native version dropdown defaults an absent selection to the available
  current version. It retains positive saved pins, including after publication;
  a current version change requires explicit selection and renewed review.
- Unit/component regressions cover missing input, asynchronous workflow loading,
  retained pins, complete bindings (including custom channel JSON and SCM), PUT
  conflict, pending feedback, review failure after a successful PUT, and retry
  without a second save, model submission or task start.
- Verification in Docker: TypeScript and production build passed; the full web
  suite passed 709 tests across 119 files. After the final feedback refinement,
  the two affected test files passed all 39 tests and the build passed again.
  The existing large-bundle advisory remains.
- Browser integration: 21 Chromium scenarios passed against a disposable real
  backend, covering persisted repairs across reload, conflict without mutation,
  review failure after persistence, explicit version upgrade following workflow
  publication, draft lifecycle, permissions, authorization and task form layout.
  Fault injection replaces only the failing HTTP boundary; successful writes,
  persistence, review and reload use the real API. No user task was edited for verification.
- Independent subagent review found no new blocking findings. Backend code and
  its admission/authorization contracts were unchanged by these two fixes;
  backend suites were not rerun for this frontend-only delta.
- Local deployment: rebuilt with `docker compose up -d --build --no-deps
  weftd`; container is healthy, `/healthz` returned `{"status":"ok"}`, and
  localhost serves the verified `index-C4ono25D.js` bundle. No commit or push.

### Task environment preparation guidance — 2026-11-25

- AC-003: when the server permits environment preparation, the task action area
  explains the build, optional approval and separate reviewed start. A keyboard
  accessible shortcut focuses the build review and does not mutate the task.
  The build definition, execution-copy confirmation and preparation action are
  grouped together. Pending feedback explicitly states that agents are not
  starting; operators can inspect the path while preparation remains admin-only.
- The known graph-oriented environment-not-ready notice is replaced by local
  task guidance. Original errors, warnings and blockers remain available under
  Technical validation details; unrelated blockers remain visible. Server
  capability checks, consent and start admission remain authoritative.
- Docker verification: TypeScript, production build and all 712 web tests in
  119 files passed. All 21 Chromium draft, prompt and layout scenarios passed
  against a disposable real backend. The preparation scenario was repeated after
  the final diagnostic refinement, confirming no execution before a separate
  reviewed start. Component regressions cover administrator/operator controls,
  pending state and transport failure with request preservation.
- Independent subagent review found no blocking issues. Its diagnostic-warning
  fidelity observation was corrected with regression coverage. This delta only
  changes the frontend; Go suites were not rerun. The existing bundle-size
  advisory remains. Local Compose rebuild is healthy and serves the verified
  `index-CfXoPuB9.js` bundle. No user task was prepared, edited or started during
  validation. No commit or push.
