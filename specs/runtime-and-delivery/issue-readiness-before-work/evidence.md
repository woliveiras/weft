# RUN-007 verification evidence

The maintainer approved D-01 through D-09 on 2026-11-12. The sections below
record the pre-implementation baseline and subsequent fixture verification.
The [matrix](matrix.md) maps coverage to each criterion; live tracker
qualification remains separate from these recorded runs.

## Pre-implementation review on 2026-11-12

The current GitHub MCP trigger adapter can search, replace the full label
list and post comments. D-03 requires evidence of who added `weft:todo`
and whether that human is a maintainer. A label snapshot cannot establish
either fact. D-07 also requires preserving unrelated concurrent label
changes. The GitHub REST API exposes issue timeline, repository permission
and individual label operations. The maintainer needs to select how the
trigger gateway will obtain these operations under reviewed consent.
[Current adapter](../../../server/internal/trigger/issue.go),
[GitHub MCP issue tools](https://github.com/github/github-mcp-server/blob/main/pkg/github/issues.go),
[Timeline events](https://docs.github.com/en/rest/issues/timeline),
[Repository permissions](https://docs.github.com/en/rest/collaborators/collaborators#get-repository-permissions-for-a-user),
[Issue label operations](https://docs.github.com/en/rest/issues/labels).

The RUN-006 closure rule also requires a decision when the issue source
and PR target are different repositories. See the
[RUN-006 pre-implementation review](../issue-references-and-polling-triggers/evidence.md#pre-implementation-review-on-2026-11-12).

| Verification | Result on the specifications-only revision |
| --- | --- |
| Specification and matrix inventory | All six RUN-007 criteria, four RUN-006 criteria and nine maintainer decisions are represented. |
| `go test -count=1 ./...` and `go test -count=1 -tags e2e ./cmd/weft-e2e` | Passed in the cached Linux Go image. |
| `./scripts/go.sh vet ./...`, `gofmt -l .` | Vet passed and gofmt returned no files. |
| `go build ./...` with `GOOS=linux` and `GOOS=darwin` | Both arm64 builds passed. |
| `pnpm -C web test`, `pnpm -C web exec tsc --noEmit`, `pnpm -C web exec tsc --project tsconfig.e2e.json` | 444 Vitest tests and both type checks passed. |
| `pnpm -C web exec playwright test e2e/activity.spec.ts e2e/triggers.spec.ts`, `pnpm -C web exec playwright test --config playwright.activity.config.ts` | Six Activity scenarios and one trigger scenario passed. |

The checks establish the pre-implementation baseline. Qualification of the
new behavior remains pending.

The maintainer subsequently approved the specific tracker API gateway
operations with reviewed consent and fixture verification, and qualified
GitHub closure references for cross-repository delivery.

## B2 fixture verification on 2026-11-12

The provider and tracker fixtures exercise the production runner, tool gateway,
durable trigger state, Activity API and task admission. No real tracker account
or inference service was used. The reviewed installation setting
`readinessMaxMicros` defaults to 100,000 micro-units of the price catalogue
currency per evaluation.

| Criterion | Observed evidence |
| --- | --- |
| AC-001 | `TestTriggerStartsARealTaskUnderConsent` verifies one tool-free model request and task admission under current consent. `TestReadinessRevalidatesSnapshotAndPreservesHoldsWhenDisabled` changes the issue during inference and confirms the stale verdict cannot claim or start it. |
| AC-002 | `TestReadinessAsksTwiceAndRequiresVerifiedManualRequeue` verifies question comments, removal of the custom selection label and preservation of other labels. `TestReadinessCapacityAndIndependentAdmission` exercises the free active slot, tenth hold, capacity stop and resumption after a hold resolves. The Activity API fixture verifies no draft, workspace or task budget is created by a hold. |
| AC-003 | The persisted GitHub fixture exercises author and configured owner replies, stale and duplicate replies, bot rejection, two rounds, intervention and the verified maintainer return. `TestHeldIssueRechecksEligibilityBeforeReopening` exercises closure, blocking, reassignment and prior admission. The Jira fixture exercises reporter reply and status preservation. Jira requeue authority is also tested against the changelog and permission response fixtures. |
| AC-004 | `TestReadinessRecoversUnconfirmedCommentWithoutRepeatingIt` simulates a lost comment response and a restart. The pending state recovers from the existing comment without another post. `TestReadinessHoldVisibleInActivityWithoutTaskOrPayload` and Playwright verify consistent issue, reason and round without raw tracker payloads and with the existing access checks. |
| AC-005 | `TestReadinessModelUsesOneBoundedToolFreeCall` exercises the byte boundary, output cap, deadline, malformed verdict, tool-call injection and monetary preflight. The API admission test passes hostile issue text only as data. `TestReadinessScopeAndBudgetChangesInvalidateConsentBeforePolling` verifies stale scope blocks tracker dispatch. The tracker tests verify connection revision, redirect refusal, credential-echo rejection and operation-specific authority. |
| AC-006 | GitHub and Jira fixtures verify the shared lifecycle, preserved unrelated labels and no Jira transition. The API and Chromium tests verify default readiness, consent pause on edit, disabled hold preservation and read-only preview. |

| Verification | Result |
| --- | --- |
| `go test -count=1 ./...` and `go test -count=1 -tags e2e ./cmd/weft-e2e` | Both passed with the cached Linux Go image. The first full run had a transient `TestDurableReviewBlocksAndResumesEngine/approved` failure reporting a changed candidate. The full rerun passed. The expanded capacity case passed again with `go test -count=1 ./internal/trigger -run TestReadinessCapacityAndIndependentAdmission`. |
| `./scripts/go.sh vet ./...` and `gofmt -l .` | Vet passed and gofmt returned no files. |
| `GOOS=linux go build ./...` and `GOOS=darwin go build ./...` | Both arm64 builds passed with the cached Go image. |
| `pnpm -C web test`, `pnpm -C web exec tsc --noEmit` and `pnpm -C web exec tsc --project tsconfig.e2e.json` | 444 Vitest tests and both type checks passed. The earlier run exposed a test fixture that still expected the old selection default. The test now asserts `weft:todo`. |
| `pnpm -C web exec playwright test e2e/activity.spec.ts e2e/triggers.spec.ts` and `pnpm -C web exec playwright test --config playwright.activity.config.ts` | All eight Chromium scenarios passed, including the same held issue in Activity and trigger state and the consent review showing the installation limits. |


## Operational rehearsal follow-up on 2026-11-12

| Review item | Fixture evidence |
| --- | --- |
| 1. Hold release | `TestClosedHoldsReleaseFullCapacity` releases ten closed issues and evaluates the next candidate. Eligibility fixtures cover closed, reassigned, blocked, unlabeled and previously admitted issues. The API and Chromium journeys verify operator resolution and Activity history. Viewer resolution is denied, and disabled readiness preserves the hold until the operator resolves it. |
| 2. Own replies | `TestEligibleRepliesRejectPublishedReadinessComments` and the persisted GitHub and Jira journeys cover Markdown and flattened ADF returned under an otherwise eligible author or owner identity. |
| 3. Publication recovery | Tracker fixtures cover permanent 403, 503 followed by a successful retry and a lost response. The lost response test uses a tracker timestamp earlier than the runner's clock. Each resend requires a complete bounded read without the original marker. Failed or over-limit reconciliation leaves the hold visible for review. |
| 4. Failed assessment allowance | Unit accounting and `TestFailedAssessmentsExhaustRoundsDespiteRepeatedAuthorReplies` confirm that the second failure consumes the remaining round, requests intervention and stops evaluation for that hold. |
| 5. Assignment rollback | The stale-snapshot fixture edits the issue during assignment. Weft restores the prior assignment state and replaces `weft:wip` with the waiting label. A Jira unit check retains a different current assignee. |
| 6. Hostile tracker text | `TestHostileIssueAndReplyCannotDispatchToolsOrChangeConsent` passes hostile body and reply data through the production evaluator, runner and gateway. The malicious verdict dispatches no requested tool or out-of-scope write and leaves consent unchanged. |
| 7. Guidance grant | `TestSubdirGrantMatchesCanonicalRootReference` and the engine integration cover ordinary workdir-relative reads, canonical package-reference matching and refusal of unrelated root reads under `.agents/**`. |
| 8. Tracker history limit | Unit and runner/gateway fixtures cover bounded comment and label history reads, including pending publication. The limit becomes an operator-resolvable reason, and subsequent polling does not repeat the scan. |


| Verification command | Result |
| --- | --- |
| `./scripts/go.sh test ./... -count=1` | The offline Docker build stopped at `apk add --no-cache git git-daemon`. The tests below used the cached Git-enabled image without downloading anything. |
| `go test ./... -count=1` and `go test -count=1 ./internal/trigger ./internal/api ./internal/activity ./internal/engine` | Full and affected-package suites passed in the cached `weft-review-go:git-http-backend` image with `--pull=never`. |
| `go test -tags e2e -count=1 ./cmd/weft-e2e` | Passed in the same image. |
| `./scripts/go.sh vet ./...` and `docker run --pull=never --rm -v "$PWD:/repo" -w /repo/server golang:1.26-alpine gofmt -l internal` | Vet passed and gofmt reported no files. |
| `GOOS=linux go build ./...` and `GOOS=darwin go build ./...` | Both arm64 builds passed. |
| `pnpm -C web test`, `pnpm -C web exec tsc --noEmit` and `pnpm -C web exec tsc --project tsconfig.e2e.json` | All 444 Vitest tests and both type checks passed. |
| `pnpm -C web exec playwright test e2e/activity.spec.ts e2e/triggers.spec.ts` and `pnpm -C web exec playwright test --config playwright.activity.config.ts` | Six Activity and trigger scenarios plus three work Activity scenarios passed. |

## Independent review of 944a581 through 7d54912

| Review item | Fixture evidence |
| --- | --- |
| Held issue scope and waiting label | `TestHeldIssueReturnsBeyondFirstQueryPage` finds issue 60 on page two and starts only that issue. The bounded-scan unit case distinguishes unverified scope from exclusion. `TestResolveLeavesWaitingLabelUntilReselectedAndClaimed` confirms that resolution itself makes no tracker write and that a later claim retires the waiting label while preserving an unrelated label. |
| Pending rollback | The hold unit test and operator HTTP test reject resolution while assignment rollback or either pending label write remains. The hold remains available for polling and no resolution is recorded. |
| Snapshot changes | Both the evaluation and claim paths reach the shared two-round allowance under repeated edits. They produce one intervention comment, stop calling the model and start no work. The claim fixture also checks the restored assignee. |
| Publication receipt | Unit tests require the saved comment ID and the token author's stable identity. GitHub and Jira fixtures cover publication and self-reply filtering. The lost-response case includes matching text from another author and confirms the actual Weft comment without reposting. The identity operation is included in trigger consent. |
| Resolution audit | The operator HTTP fixture checks the durable `trigger.hold_resolved` event, including the issue key and authenticated actor. Chromium checks that the Activity entry displays the same issue and actor. |
| Scope edit documentation | The API contract describes paused polling, release after renewed consent and disabled holds that require an operator. |
| Hold generation | The unit tests cover generation rotation and stable legacy holds. The runner fixture keeps the opening time constant across rounds and proves the older generation returns a conflict. The HTTP fixture expects 409 for the older generation, and Chromium resolves the current hold through the console. |
| Durable review fixture | The original intermittent timeout did not recur in 20 baseline runs. A unit test confirms that a new replica scratch directory changes the review fingerprint. The engine fixture now waits for both replicas to finish setup in the shared checkout before injecting input and surfaces the first worker error directly. The durable gate timeout and approval and rejection assertions are unchanged. |

| Verification command | Result |
| --- | --- |
| `go test ./... -count=1` | Full Go suite passed. |
| `go test -count=1 ./internal/trigger ./internal/api ./internal/activity ./internal/engine ./internal/review` and `go test -tags e2e -count=1 ./cmd/weft-e2e` | Changed packages and the fixture server passed. |
| `go test -count=20 ./internal/engine -run '^TestDurableReviewBlocksAndResumesEngine$'` | Passed all 20 runs after synchronizing fixture startup, including approval and rejection. |
| `./scripts/go.sh vet ./...` and `docker run --pull=never --rm -v "$PWD:/repo" -w /repo/server golang:1.26-alpine gofmt -l internal` | Vet passed and gofmt listed no files. |
| `GOOS=linux go build ./...` and `GOOS=darwin go build ./...` | Both builds passed. |
| `pnpm -C web test`, `pnpm -C web exec tsc --noEmit`, `pnpm -C web exec tsc --project tsconfig.e2e.json` and `pnpm -C web build` | All 444 Vitest tests, both type checks and the production web build passed. |
| `pnpm -C web exec playwright test e2e/triggers.spec.ts e2e/activity.spec.ts` and `pnpm -C web exec playwright test --config playwright.activity.config.ts` | Nine Chromium scenarios passed. |

Go tests and cross-builds used the cached `weft-review-go:git-http-backend`
image with `--pull=never`. Tracker calls used fixtures. Live account
qualification remains in PROD-14.

## Independent review of 7d54912 through 30bbaa1

Fixture verification on 2026-11-12.

The maintainer's D-10 decision is the oracle for separate question and edit
counts. The evaluation and claim fixtures reached operator intervention
on the third confirmed edit and still allowed both question rounds after
one edit. The transient fixtures covered a busy lease, lease and store
errors, a failed claim reread, and GitHub and Jira author lookup failures.
They checked rollback, visible retry state, unchanged allowances and
comment publication after recovery.

The label fixtures exercised permission refusal, issue deletion and
retry exhaustion. Paused and disabled readiness fixtures completed the
pending rollback under current consent. API tests exercised ordinary
and forced resolution, viewer denial, and the audit warning about
abandoned tracker changes. GitHub scope fixtures retained a hold while
its issue was missing from the index, confirmed two misses only after
the current revision was visible, and kept the question count on
reselection.

| Command | Result |
| --- | --- |
| `go test -count=1 ./internal/trigger ./internal/api ./internal/activity` | Passed all three changed packages. |
| `go test -count=1 ./...` and `go test -count=1 -p 1 ./...` | The final serial full suite passed. Earlier runs failed in `TestActivityAPIWithEngine` and `TestBoundedBuildPipelineIntegration`. Both passed on the final run. |
| `go test -count=1 ./internal/engine` twice | Both runs passed (31.079 s and 29.551 s). |
| `./scripts/go.sh vet ./...` and `gofmt -l .` in `server/` | Vet passed. Gofmt listed no files. |
| `GOOS=linux CGO_ENABLED=0 go build ./...` and `GOOS=darwin CGO_ENABLED=0 go build ./...` | Both arm64 builds passed. |
| `go test -count=1 -tags e2e ./cmd/weft-e2e` | Passed the disposable backend tests. |
| `./node_modules/.bin/vitest run` in `web/` | 444 tests passed in 88 files. |
| `./node_modules/.bin/tsc --noEmit` and `./node_modules/.bin/tsc --project tsconfig.e2e.json` in `web/` | Both type checks passed. |
| `./node_modules/.bin/playwright test e2e/triggers.spec.ts e2e/activity.spec.ts` and `./node_modules/.bin/playwright test --config playwright.activity.config.ts` in `web/` | Six trigger and Activity scenarios plus three Work Activity scenarios passed. Both browser runs built the production web bundle and disposable Darwin fixture. |

Go used the cached toolchain images with Docker pulls disabled. The pnpm
bootstrap and dependency verification attempted registry access and failed.
Web verification used the lockfile versions recovered from the local pnpm
cache and the installed Chromium. The Playwright web server ran the same
`tsc --noEmit` and `vite build` steps as the package script. The tests
used fixture accounts and trackers.

## Live tracker rehearsal checklist

- With separately authorized GitHub credentials, confirm that the
  `search_issues` MCP operation accepts the second-precision
  `created:<RFC3339>` qualifier and returns the expected issue from the
  configured repository.
- Compare `updated_at` from search and the REST issue read for the same
  revision. Observe both during an issue edit, check that different
  representations of the same instant compare equal, and verify the
  transition from visible index lag to REST signal processing and then
  to query-confirmed admission.

## Independent review of 30bbaa1 through 6ce5470

Fixture verification on 2026-11-12.

- The HTTP transport and runner fixtures exercised GitHub primary and
  secondary 403 responses and verified retry deadlines for label writes,
  claim rereads and identity lookups. The 401 unit fixture verified that
  only a confirming identity 401 makes the rejection definitive.
- Timestamp comparisons used equivalent UTC instants. After three
  lagging polls, the GitHub runner fixture processed an author response
  and a verified maintainer requeue through REST in separate cases.
  Claim waited for query verification and a fresh verdict.
- The D-10 eligibility fixture reached operator intervention at three
  discarded evaluations without using a clarification round. The 404
  fixture released a confirmed hold and recorded the issue-not-found
  reason and `system:trigger` actor in Activity and the event stream.
- Retention tests covered the 30-day boundary, the 100-entry limit and
  paused-trigger maintenance. Persisted failure transitions emitted one
  metric and one structured log per outcome. The web and Chromium tests
  verified the edit count and the cancel and confirm paths when a
  readiness comment remains unpublished after the label write.

| Command | Result |
| --- | --- |
| `go test -count=1 ./internal/trigger ./internal/observability ./internal/api ./internal/activity ./internal/mcpconfig` | All five packages passed. |
| `go test -count=1 -p 1 ./...` | The full Go suite passed. |
| `go test -count=1 ./internal/engine` twice | Both runs passed (30.622 s and 30.262 s). |
| `./scripts/go.sh vet ./...` and `gofmt -l .` in `server/` | Vet passed. Gofmt listed no files. |
| `GOOS=linux CGO_ENABLED=0 go build ./...` and `GOOS=darwin CGO_ENABLED=0 go build ./...` | Both arm64 builds passed. |
| `go test -count=1 -tags e2e ./cmd/weft-e2e` | Passed the disposable browser fixture tests. |
| `./node_modules/.bin/vitest run` in `web/` | 446 tests passed in 88 files. |
| `./node_modules/.bin/tsc --noEmit` and `./node_modules/.bin/tsc --project tsconfig.e2e.json` in `web/` | Both type checks passed. |
| `./node_modules/.bin/playwright test e2e/triggers.spec.ts e2e/activity.spec.ts` in `web/` | Four trigger and three Activity scenarios passed, including cancel and confirm for forced resolution. |

Go used cached Docker images with pulls disabled. Browser verification
used the installed web tools and Chromium. The browser fixture built the
production web bundle and a disposable Darwin backend. All tracker
operations used fixtures.

## Independent review of 6ce5470 through 29b552d

Fixture verification on 2026-11-12.

- The gateway and runner fixtures covered a GitHub 404 with repository
  access, a repository 404 or 403, immediate deletion on 410, and
  operator holds with missing issues. Two consecutive polls were
  required for a 404 release. Automatic releases preserved rounds
  and edits and produced an Activity event.
  [GitHub issue status contract](https://docs.github.com/en/rest/issues/issues#get-an-issue).
- The transport, runner and console tests covered oversized Retry-After
  and primary reset values, a one-hour cap, and a visible retry deadline.
- The GitHub scope fixture covered both an author reply and a verified
  maintainer requeue while search lagged. The deferred question survived
  restart without a tracker write or a spent round. Query confirmation
  allowed publication of the saved question or reevaluation if the
  issue changed.

| Command | Result |
| --- | --- |
| `go test -count=1 ./internal/trigger ./internal/api ./internal/activity ./internal/mcpconfig` | All four affected packages passed. |
| `go test -count=1 -p 1 ./...` | The full suite passed on repeat. The first run failed in the existing `TestBoundedBuildPipelineIntegration` fixture, which passed on repeat. |
| `./scripts/go.sh vet ./...` and `gofmt -l .` in `server/` | Vet passed. Gofmt listed no files. |
| `GOOS=linux CGO_ENABLED=0 go build ./...` and `GOOS=darwin CGO_ENABLED=0 go build ./...` | Both arm64 builds passed. |
| `go test -count=1 -tags e2e ./cmd/weft-e2e` | Passed the disposable browser fixture tests. |
| `./node_modules/.bin/vitest run` in `web/` | 447 tests passed in 88 files. |
| `./node_modules/.bin/tsc --noEmit` and `./node_modules/.bin/tsc --project tsconfig.e2e.json` in `web/` | Both type checks passed. |
| `./node_modules/.bin/playwright test e2e/triggers.spec.ts e2e/activity.spec.ts` in `web/` | Four trigger and three Activity scenarios passed. |

Go used cached Docker images with pulls disabled. Web checks used the
installed tools and Chromium. Playwright built the web bundle and a
disposable Darwin backend. Tracker verification used fixtures.

## Independent review of 29b552d through 65f0d60

Fixture verification on 2026-11-12.

- GitHub repository and runner fixtures verified that an issue 410 can
  release an ordinary hold only after an accessible repository reports
  `has_issues: true`. A disabled Issues feature reports the reason and
  keeps the hold. [GitHub repository API](https://docs.github.com/en/rest/repos/repos#get-a-repository).
- Jira project fixtures verified the D-05 extension. The gateway reads
  the issue's project through the reviewed site connection. Project
  404 and 403 keep the hold and report the access failure.
  [Jira project API](https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-projects/#api-rest-api-3-project-projectidorkey-get).
- A temporary repository probe failure kept its retry deadline and
  preserved other polling work. A restart preserved the initial 404
  observation, an early poll did not qualify for release, and a
  successful issue read cleared the observation.
- Vitest and Chromium asserted the localized retry text. The console
  omitted expired deadlines and displayed the pending access retry.

| Command | Result |
| --- | --- |
| `go test -count=1 ./internal/trigger ./internal/api ./internal/activity` | All three packages passed. |
| `go test -count=1 -p 1 ./...` | The full Go suite passed. |
| `./scripts/go.sh vet ./...` and `gofmt -l .` in `server/` | Vet passed. Gofmt listed no files. |
| `GOOS=linux CGO_ENABLED=0 go build ./...` and `GOOS=darwin CGO_ENABLED=0 go build ./...` | Both arm64 builds passed. |
| `go test -count=1 -tags e2e ./cmd/weft-e2e` | Passed the disposable browser fixture tests. |
| `./node_modules/.bin/vitest run` in `web/` | 447 tests passed in 88 files. |
| `./node_modules/.bin/tsc --noEmit` and `./node_modules/.bin/tsc --project tsconfig.e2e.json` in `web/` | Both type checks passed. |
| `./node_modules/.bin/playwright test e2e/triggers.spec.ts e2e/activity.spec.ts` in `web/` | Five trigger and three Activity scenarios passed. |

Go used cached Docker images with pulls disabled. Web verification used
installed tools and Chromium. Playwright built the web bundle and a
disposable Darwin backend. All tracker operations used fixtures.

## Independent review of 1ac3098 through 9251f46

Fixture verification on 2026-11-12.

- The disposition table covered unverified, inaccessible, first, early
  and confirmed Jira 410 responses. The runner fixture exercised both
  Jira 404 and 410 with project access granted and denied. Verified
  absence preserved readiness progress.
- Unit tests covered retry counts, deadlines and guidance for 401, 503
  and unavailable probe results. Persisted runner fixtures verified
  that the count survived a restart, stopped at five, kept the hold
  and allowed unrelated discovery. Renewed consent resumed verification
  without spending a round. The console unit test exercised that action.
- The workflow guide and D-05 document why project denial blocks a poll
  and identify the required project permission and API scopes.
  [Atlassian Get project](https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-projects/#api-rest-api-3-project-projectidorkey-get)
- Vitest expired two visible deadlines in order with the page open.
  Chromium rendered the localized access deadline and removed it as
  the clock passed it while the hold remained visible.

| Command | Result |
| --- | --- |
| `go test -count=1 ./internal/trigger ./internal/observability ./internal/api ./internal/activity` | All four packages passed. |
| `go test -count=1 -p 1 ./...` and `go test -count=1 ./internal/engine` | The full suite passed. The engine passed in the full suite (30.571s) and on its standalone run (30.417s). |
| `./scripts/go.sh vet ./...` and `gofmt -l .` in `server/` | Vet passed. Gofmt listed no files. |
| `GOOS=linux CGO_ENABLED=0 go build ./...` and `GOOS=darwin CGO_ENABLED=0 go build ./...` | Both arm64 builds passed. |
| `go test -count=1 -tags e2e ./cmd/weft-e2e` | Passed the disposable browser fixture tests. |
| `./node_modules/.bin/vitest run` in `web/` | 449 tests passed in 88 files. |
| `./node_modules/.bin/tsc --noEmit` and `./node_modules/.bin/tsc --project tsconfig.e2e.json` in `web/` | Both type checks passed. |
| `./node_modules/.bin/playwright test e2e/triggers.spec.ts e2e/activity.spec.ts` in `web/` | Five trigger and three Activity scenarios passed in Chromium. |

Go used cached Docker images with pulls disabled. Playwright used installed
Chromium and a disposable Darwin backend. Live tracker qualification remains
in PROD-14.

## Independent review of 9251f46 through abf8bc6

Fixture verification on 2026-11-12.

- The runner observed five consecutive GitHub repository rate limits,
  preserved each deadline and left the access failure count at zero.
  A unit case kept four existing failures unchanged. The Jira REST
  transport fixture preserved Retry-After from a project 429.
  [Atlassian rate-limit response](https://developer.atlassian.com/cloud/jira/platform/rate-limiting/)
- The persisted runner exposed both failed holds by issue key. Vitest
  verified that each hold renders its own error, and Chromium verified
  the error from the fixture API.
- Jira project 403 and 404 report Browse Projects permission and the
  granular scope `read:project:jira`. The workflow guide explains
  recovery after five non-rate-limit failures.

| Command | Result |
| --- | --- |
| `go test -count=1 ./internal/trigger ./internal/mcpconfig ./internal/api ./internal/activity` | All four affected packages passed. |
| `go test -count=1 -p 1 ./...` and `go test -count=1 ./internal/engine` | The full suite passed. The engine passed in the full suite (30.524s) and on its standalone run (30.434s). |
| `./scripts/go.sh vet ./...` and `gofmt -l .` in `server/` | Vet passed. Gofmt listed no files. |
| `GOOS=linux CGO_ENABLED=0 go build ./...` and `GOOS=darwin CGO_ENABLED=0 go build ./...` | Both arm64 builds passed. |
| `go test -count=1 -tags e2e ./cmd/weft-e2e` | Passed the disposable browser fixture tests. |
| `./node_modules/.bin/vitest run` in `web/` | 450 tests passed in 88 files. |
| `./node_modules/.bin/tsc --noEmit` and `./node_modules/.bin/tsc --project tsconfig.e2e.json` in `web/` | Both type checks passed. |
| `./node_modules/.bin/playwright test e2e/triggers.spec.ts e2e/activity.spec.ts` in `web/` | Five trigger and three Activity scenarios passed in Chromium. |

Go used cached Docker images with pulls disabled. Playwright used installed
Chromium and a disposable Darwin backend. Live tracker qualification remains
in PROD-14.

## AA1–AA3 trigger launch repair on 2026-11-16

The operational failure was confirmed in the trigger/task managers: an empty
result was copied into a draft, trigger launch skipped environment preparation,
and start errors were replaced by generic tracker messages without a saved
launch diagnostic. Recovery also retained a failed admission that ordinary
resume/relabel could not reconsider.

The repair defaults omitted results to the entry at save, legacy decode, consent
comparison and review. Trigger tasks use the existing status tick to await a
durable Ask approval, prepare asynchronously and start the ready execution copy.
Repository images retain the shared pipeline. Compatible source images without
repository, context, provenance or setup can be tagged for a task with private
caches. No new reconciler was introduced. The maintainer explicitly approved
this minimal extension. Public failure causes are classified from trusted text,
persisted on the draft, logged once at the trigger boundary and linked from the
tracker and console. A narrow administrator retry records a new single-use
admission generation, retires the undispatched draft without deleting it and
requires renewed consent plus a fresh verified maintainer requeue.

Verification covers:

- Unit tests for result defaults, legacy fingerprints, enabled-agent validation,
  reuse exclusions, approval validation, safe diagnostics and retry generations.
- API/store/gateway/environment/engine integration with a GitHub transport fixture:
  readiness verdict, claim, pending Ask decision, asynchronous build, compatible
  reuse with and without a repository, one task execution and retained `weft:wip`.
  The real managers run against SQLite and a controlled Docker CLI boundary.
- Preparation, build and start failures; approval rejection, expiration, stale
  scope and role restrictions; persisted safe cause, task link, failure label and
  exactly one structured launch-failure log. The existing no-environment launch
  and budget-before-effects integration tests remain enabled.
- Persisted runner recovery with unchanged original admission, audited generation,
  rejection of old/unprivileged labels and exactly one fresh admission. API tests
  reject missing confirmation and non-admin callers and prevent old-draft start.
- Vitest task/trigger diagnostics and pending-approval navigation; Chromium trigger,
  task-draft and task-authorization flows using a disposable backend.

| Command | Result |
| --- | --- |
| `./scripts/go.sh test ./...` | Full Go suite passed. The cached Go test image includes Git and git-daemon; no fallback image was needed. |
| `./scripts/go.sh test ./internal/api -run TestTriggerEnvironmentLaunchLifecycle -count=1` | All nine lifecycle cases passed, including repository image reuse. |
| `./scripts/go.sh test -tags e2e ./cmd/weft-e2e` | Passed. |
| `./scripts/go.sh vet ./...`, `make lint`, Docker `gofmt -l .` with `--pull=never` | Passed; no unformatted files. |
| `make build` and Docker `CGO_ENABLED=0 GOOS=linux/darwin go build ./...` | Passed for both supported targets. Windows was not built. |
| `make test-python` | All 40 SDK, agent and script tests passed. |
| `pnpm typecheck`, `tsc --project tsconfig.e2e.json`, `pnpm exec vitest run`, `pnpm run build` | Passed in cached `node:22-bookworm` with the named node-modules volume and public npm registry; 464 tests in 88 files. |
| `playwright test e2e/triggers.spec.ts e2e/task-drafts.spec.ts e2e/task-authorization.spec.ts` | 22 Chromium scenarios passed, including all six trigger scenarios. |
| `git diff --check` | Passed. |

The aggregate `make test` target was covered through its Go/Python suites and
the equivalent web commands above. Playwright used the installed macOS Chromium
and Node, with a disposable Darwin backend cross-built in the cached Go container;
a temporary config changed only the web-server executable, not checks or tests.
An initial Vitest run timed out in an unchanged shared-environment test while
Go builds were running; the complete rerun passed without changing its timeout.
The first Go full run caught the new event missing from the schema/catalog; both
were added before the successful full rerun. No tests were weakened.


### Read-only operational stack verification

`docker compose up -d --build --no-deps weftd` succeeded using the unchanged
standard Dockerfile. All three base image digests were already cached; no base
image was downloaded. The running container is `e2e5c7640b85` with image
`sha256:8b5fd37100600c56b5311d008ceb221e86c3d4f28efd5ba0cc47ab12459d3153`.
Docker health and `/healthz` both passed. `docker history --no-trunc` showed the
Dockerfile's runtime package layer, compiled `/out/weftd` and `/out/weft` copies,
preview relay and `/web/dist` copy. There was no override or injected binary.

Thirteen authenticated read-only API projections were checked before and after.
The workflow, its environment, providers, prices, cost policy, budgets, network
policy, MCP/SCM connections, projects, triggers and health had identical SHA-256
comparisons. The retained task projection differed only by the derived
`resultNodeId: updater`; removing that derived field reproduced its original
hash. Its persisted revision remained 1 and status remained `draft`, with no
execution graph. The old result-agent blocker disappeared from its review.
The retained task still needs preparation and renewed task authorization;
recovery creates a new admission with fresh scope rather than modifying it now.

Preserved operational state: Gemini ready; workflow `weft-deps` environment
ready, hash `594c685eceed`; price version 3; task budget USD 2 with 80% warning;
installation budget USD 10/month with 110% interruption; network policy revision
1; MCP `github`, SCM `weft-github`, project `weft`; paused `weft-issues` trigger
`trg_mwj0ulob8333166fa4c262284fb0` with its failed processed key; original task
`tsk_mwj0v5hzff6b56126a14f84f37e6`. No real GitHub requests or writes, trigger
resume, task start/cancellation/deletion, direct database writes, volume deletion
or secret changes were performed. The administrator token was used without
printing it. The operator's live issue recovery remains intentionally pending.

### Changed files for AA1–AA3

The implementation commits are `74c4eab` and `52fca58`. All paths below are
relative to the repository root; the subsequent verification commit updates
only this evidence file.

```text
docs/api.md
docs/environments.md
docs/events.md
docs/task-workflows.md
schemas/event.schema.json
server/cmd/weft-e2e/tracker.go
server/internal/api/server.go
server/internal/api/task_execution_test.go
server/internal/api/tasks_test.go
server/internal/api/trigger_launch_test.go
server/internal/api/triggers.go
server/internal/api/triggers_test.go
server/internal/app/triggers.go
server/internal/app/triggers_test.go
server/internal/domain/state.go
server/internal/environment/manager.go
server/internal/environment/workflow_reuse.go
server/internal/environment/workflow_reuse_test.go
server/internal/taskconfig/integration_test.go
server/internal/taskconfig/manager.go
server/internal/taskconfig/manager_test.go
server/internal/taskconfig/prompt.go
server/internal/taskconfig/prompt_test.go
server/internal/taskrun/environment_approval.go
server/internal/taskrun/environment_approval_test.go
server/internal/taskrun/environment_prepare.go
server/internal/taskrun/manager.go
server/internal/taskrun/manager_test.go
server/internal/trigger/eligibility.go
server/internal/trigger/launch_error.go
server/internal/trigger/launch_error_test.go
server/internal/trigger/result_test.go
server/internal/trigger/retry.go
server/internal/trigger/retry_test.go
server/internal/trigger/retry_unit_test.go
server/internal/trigger/runner.go
server/internal/trigger/runner_test.go
server/internal/trigger/trigger.go
server/internal/trigger/trigger_test.go
specs/runtime-and-delivery/issue-readiness-before-work/evidence.md
specs/runtime-and-delivery/issue-readiness-before-work/matrix.md
web/e2e/task-drafts.spec.ts
web/e2e/triggers.spec.ts
web/src/api/tasks.ts
web/src/api/types.ts
web/src/components/ApprovalsView.tsx
web/src/components/TasksPage.test.tsx
web/src/components/TasksPage.tsx
web/src/components/TriggersSection.test.tsx
web/src/components/TriggersSection.tsx
```


## 2026-11-16 operational blockers AB2–AB4 (AC-004)

- `internal/api/task_outcome_test.go` exercises trigger admission, a scripted native agent, real engine/task lifecycle, tracker HTTP boundary and explicit retry. Both `blocked` and `no_further_work` without a required PR produce task `blocked`, graph `stopped`, `weft:needs-attention`, a bounded/redacted agent summary and task URL. Admin/confirmation requirements, duplicate retry refusal, fresh maintainer relabeling and preservation of the original task/graph/executions are asserted.
- Retry unit/integration cases reject running graphs, unfinished execution, pending approval, delivered PR, wrong ownership, empty history and storage failure. Existing admission, restart and maintainer-role tests still pass. A tracker write-back failure retains the sanitized reason and task URL while pausing.
- `make test` passed: the full Docker Go suite, 40 Python tests and 466 Vitest tests. Node 22 Bookworm separately passed typechecking, E2E TypeScript compilation and Vitest. Selected Playwright task/trigger/workflow flows passed 29 tests. `make lint`, `make build`, explicit Linux/Darwin builds and `./scripts/go.sh test -tags e2e ./cmd/weft-e2e` passed. The normal Go test image with git/git-daemon was usable; no fallback image was needed for Go tests.
- Verification found and corrected transient-state assumptions in budget and task-authorization fixtures: terminal task/graph state must now be observed, while budget-resume audit and authorization checks remain required. Earlier verbose runtime guidance exceeded small-context fixtures; the final concise finish description passes those unchanged tests.
- Live issue retry and Gemini recovery were deliberately not executed. Operator recovery is documented in `docs/task-workflows.md`; it requires explicit task Stop for historical active copies, paused trigger, admin-confirmed retry and a new human Maintain/Admin `weft:todo` event. No retained admission or execution is deleted.
- Final contract follow-up passed: `./scripts/go.sh test ./internal/eventtest ./internal/api ./internal/trigger -count=1`; emitted blocked events satisfy the schema. `make lint` and `make build` passed again after the final review; explicit cached-image `gofmt -l` produced no output and `git diff --check` passed.
- `docker compose up -d --build --no-deps weftd` completed using the unmodified standard Dockerfile and cached base images. Running image: `sha256:2d0f31160f93e49c8f8d3384d5227beaeef3abf1efbc5a75aa728bb40f2f6e2c`. `docker history --no-trunc` shows the standard build-stage binary copies and `/web/dist`; container image ID matches, Docker health is healthy, `/healthz` is OK and authenticated `/api/v1/info` reports admin access.
- Read-only API snapshots before/after show zero differences in provider, prices, cost policy/budgets, MCP/SCM connections, projects, network policy, triggers, both task records, all four graph definitions and graph state/run/version fields. `weft-deps` remains ready/up-to-date; prices version 3, task limit USD 2, warning 80%, installation USD 10/month, interruption 110%, network policy revision 1 and `weft-issues` paused are confirmed. The historical task and execution graph intentionally retain persisted `running`; no resume/retry/task-control/tracker mutation was performed.
- Toolchain note: an initial offline build of the web tools could not fetch the missing pnpm package. The standard network-enabled package build succeeded; all base images were already cached. No project dependency or lockfile changed. An intermediate schema-test compile error used a nonexistent event constant; using the existing dynamic event name fixed it. All final checks above passed.

- Post-rebuild read-only progress for the historical rehearsal task reports `blocked` and a 155-character displayed summary. Its stored task status remains `running` and original execution remains `succeeded` / `no_further_work`; audit history is not rewritten.

Changed files for AB1–AB4:

```
docs/api.md
docs/events.md
docs/task-workflows.md
schemas/event.schema.json
schemas/turn.schema.json
server/cmd/weft-e2e/main.go
server/internal/activity/normalize.go
server/internal/api/cost_test.go
server/internal/api/task_outcome_test.go
server/internal/api/trigger_launch_test.go
server/internal/api/triggers_test.go
server/internal/app/app.go
server/internal/app/triggers.go
server/internal/engine/engine.go
server/internal/engine/turn.go
server/internal/eval/shell_recovery_test.go
server/internal/eventtest/schema_test.go
server/internal/runtime/native/blocked_test.go
server/internal/runtime/native/native.go
server/internal/runtime/runtime.go
server/internal/secrets/diagnostics.go
server/internal/secrets/diagnostics_test.go
server/internal/taskrun/delivery.go
server/internal/taskrun/manager.go
server/internal/taskrun/manager_test.go
server/internal/taskrun/settle.go
server/internal/taskrun/settle_test.go
server/internal/tools/builtin_shell.go
server/internal/tools/shell_guidance_test.go
server/internal/trigger/complete_test.go
server/internal/trigger/retry.go
server/internal/trigger/retry_delivery_test.go
server/internal/trigger/retry_unit_test.go
server/internal/trigger/runner.go
specs/product-and-tasks/task-execution-admission-and-recovery/evidence.md
specs/product-and-tasks/task-execution-admission-and-recovery/matrix.md
specs/product-and-tasks/task-execution-admission-and-recovery/spec.md
specs/runtime-and-delivery/issue-readiness-before-work/evidence.md
specs/runtime-and-delivery/issue-readiness-before-work/matrix.md
specs/runtime-and-delivery/issue-readiness-before-work/spec.md
specs/runtime-and-delivery/native-coding-runtime/evidence.md
specs/runtime-and-delivery/native-coding-runtime/matrix.md
specs/runtime-and-delivery/native-coding-runtime/spec.md
web/e2e/task-authorization.spec.ts
web/e2e/task-outcome.spec.ts
web/src/App.tsx
web/src/components/GraphList.tsx
web/src/components/GraphNavigation.test.tsx
web/src/components/TasksPage.tsx
web/src/components/TriggersSection.test.tsx
```
