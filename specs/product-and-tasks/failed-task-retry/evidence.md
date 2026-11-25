# TASK-011 evidence

Recorded on 2026-11-25. The specification and matrix preceded implementation.
Test authors inspected the existing architecture and contracts; tests added
during diagnosis are implementation-aware regression evidence, not independent
qualification. An independent subagent reviewed the spec, proposed tests and
implementation. No real remediation task was retried or started.

## Fail-first

| Criterion | Verification | Expected and observed failure | Oracle provenance |
| --- | --- | --- | --- |
| AC-001–003 | `./scripts/go.sh test ./internal/taskconfig -run TestRetryFailed -count=1` | Compilation failed because `RetryFailed` did not exist | spec-derived matrix; implementation-aware fixtures |
| AC-004/005 | Focused Vitest component and journey tests | Missing retry component and incorrect expired-approval guidance | spec-derived matrix; implementation-aware fixtures |
| TASK-007 AC-004 | Browser journey and API rejected/expired message-gate cases | Observed failure appeared in progress but durable task remained running with no executions; retry was unavailable | diagnostic regression |

The message-gate regression required a narrow settlement correction: a confirmed
failed outcome may settle without an execution. Pending approvals and active
work still prevent a failed outcome; preparation blocks without executions still
require explicit operator cancellation. No migration or retry eligibility
relaxation was introduced.

## Passing results

| Criteria | Verification | Result |
| --- | --- | --- |
| AC-001–003 | Manager units, filesystem/SQLite integration, API role and revision tests | Exact input/provenance; no inherited consent/runtime; source retained; stale pins preserved |
| AC-002 | Concurrent CAS, storage reopen, uncertain committed write, edited/started replay and tombstone tests | One child per source revision, errors exposed, deleted child not resurrected |
| AC-004/005 | Component, TasksPage and TaskTracking tests | Loading/error feedback, duplicate-click guard, role restrictions, original links, recorded-expiry and trigger-specific guidance |
| AC-004/005 | Docker Chromium `e2e/task-retry.spec.ts`, disposable SQLite backend | Passed with keyboard activation, focused origin notice, 720/1440 px layouts, preserved input after reload, explicit review without execution and replay to the same child |
| Regression | Docker Chromium `task-retry`, `task-outcome`, `task-authorization` | All four cases passed. Existing authorization/outcome fixtures were updated for current tabs and links, preserving contract assertions; the independent reviewer checked that delta |
| TASK-007 AC-004 | `./scripts/go.sh test ./internal/taskrun ./internal/api -count=1` after settlement correction | Passed, including real rejected/expired gates without executions and another pending approval |
| Regression | `./scripts/go.sh test ./...` | Passed before the additional message-gate settlement correction |
| Regression | Docker Node 22: `pnpm exec tsc --noEmit && pnpm test` | 775 tests in 128 files passed; focused follow-up covered expiry, conflict and focus changes |
| Build/checks | `make lint`, `make build`, `make test-python` | Passed; lint also rerun successfully after settlement correction |
| Local delivery | Standard `docker compose up -d --build --no-deps weftd`, `/readyz`, read-only task progress | Build succeeded; queue/storage/sandbox ready, Postgres and weftd healthy. The real failed manual task retains its expired decision and no retry child was created |

Visual inspection caught the origin notice inheriting a red, small error style.
It now uses a neutral card with theme text, spacing and a visible theme focus
ring. Updated wide and narrow browser screenshots were inspected. The final
focused UI run passed 45 tests, with app and E2E TypeScript checks. No usability
study or real-account retry is asserted.

## Documentation decision

- Decision: `required`.
- The endpoint, server-owned provenance, replay semantics, actor boundaries and
  recovery journey are new contracts.
- Updated artifacts: `api/openapi.yaml`, `docs/api.md`, `docs/events.md`,
  `schemas/event.schema.json`, `docs/features/tasks/README.md`.

## Residual limitations

- A retry is a separate attempt, not resume at the failed tool call. Existing
  remote effects must be inspected before repeating work.
- Only failed manual tasks are eligible. Issue-trigger recovery remains in its
  existing flow; cancelled/blocked tasks and automatic retry policies are excluded.
- No real hosted-model, Git push, pull-request creation or approval replay is
  part of this feature verification.
