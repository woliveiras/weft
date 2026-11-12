# RUN-006 historical evidence

These results were recorded at the time of the run. The documentation migration did not rerun them.

- 2026-11-11. Local-first progress records all seven trigger increments complete with fixtures; no real Jira/GitHub account qualification was recorded.

## Pre-implementation review on 2026-11-12

The trigger's issue source and SCM connection are independent. GitHub
interprets `Closes #N` in the PR's repository and requires a qualified
`owner/repository#N` for another repository. The maintainer needs to
choose whether to qualify cross-repository references or require the
same repository for issue and delivery. The gateway already opens PRs
against the destination's default branch.
[Trigger configuration](../../../server/internal/trigger/trigger.go),
[SCM gateway](../../../server/internal/scm/tools.go),
[GitHub closing references](https://docs.github.com/en/issues/tracking-your-work-with-issues/using-issues/linking-a-pull-request-to-an-issue).

The [RUN-007 baseline verification](../issue-readiness-before-work/evidence.md#pre-implementation-review-on-2026-11-12)
includes the existing trigger integration and Playwright scenarios. The new
label lifecycle and closure criteria still require fixture qualification.

The maintainer subsequently approved the specific tracker API gateway
operations with reviewed consent and fixture verification, and qualified
GitHub closure references for cross-repository delivery.

## B2 fixture verification on 2026-11-12

| Criterion | Observed evidence |
| --- | --- |
| AC-001 | Existing explicit issue-reference unit and integration regressions passed. |
| AC-002 | Existing trigger lease, scheduling and eligibility regressions passed. API and Chromium tests verify consent invalidation and read-only preview. The readiness runner test verifies default `weft:todo` and the custom selection label path. |
| AC-003 | GitHub and Jira gateway fixtures exercise claim, completion and failures with `weft:wip`, `weft:done` and `weft:needs-attention`. A label-operation unit test adds an unrelated GitHub label concurrently and verifies it is preserved. Jira status stays *To Do* throughout the fixture and no transition is invoked. |
| AC-004 | `TestAppendClosingIssue` and `TestTriggeredPullRequestBodyUsesAdmittedIssueIdentity` verify short and qualified closing references through the SCM gateway and GitHub HTTP fixture. Delivery without a GitHub issue retains the original PR body. |

See the [B2 command results](../issue-readiness-before-work/evidence.md#b2-fixture-verification-on-2026-11-12).
Live qualification remains with PROD-14.


## Operational rehearsal follow-up on 2026-11-12

The follow-up verifies that released holds free trigger capacity, an Operator
can resolve a hold without changing reviewed consent, and stale claims undo
new assignments and restore the waiting label. The GitHub and Jira fixtures
exercise comment replay and bounded history. The
[RUN-007 review matrix evidence](../issue-readiness-before-work/evidence.md#operational-rehearsal-follow-up-on-2026-11-12)
records the individual cases. Live account qualification remains in PROD-14.


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

The 60-issue GitHub fixture verifies held-issue scope on the second search
page. The claim fixture verifies retirement of the waiting label after
selection, with unrelated labels preserved. Operator resolution checks
pending rollback and the current hold generation before recording the actor
and issue key. See the
[RUN-007 fixture cases](../issue-readiness-before-work/evidence.md#independent-review-of-944a581-through-7d54912).

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

`TestHeldIssueReturnsWithTargetedQueryBeyondFirstAdmissionPage` and
`TestHeldGitHubSearchBudgetRotatesTenIssues` qualified narrowed scope
checks against 60 issues, including fair deferrals with ten holds.
`TestGitHubSearchBudgetUsesRollingMinute` verified that the 31st call is
deferred until a reservation expires. The admission fixture reproduced
an incomplete result inside the MCP envelope and verified the dedicated
search error and zero claims. The scope edit fixture verified cleanup of
`weft:needs-attention` after the original resolution fell out of the
20-entry history, with an automatic `trigger.hold_resolved` event from
`system:trigger`.

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

## Independent review of 30bbaa1 through 6ce5470

Fixture verification on 2026-11-12.

`TestSharedGitHubBudgetRotatesAcrossFourTriggers` exercised four due
triggers sharing the rolling 30-search window. Each trigger received
an earlier turn after being deferred. `TestGitHubPreviewSharesTheRollingSearchBudget`
used the operator route to exhaust that same window, verified that
polling persisted a visible deferral, and confirmed that preview kept
the trigger state unchanged. Agent MCP searches remain outside this
budget.

The filter and timestamp contract still needs separate live qualification.
See the [GitHub rehearsal checklist in RUN-007](../issue-readiness-before-work/evidence.md#live-tracker-rehearsal-checklist).

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

`TestSearchBudgetReportsOldestReservationExpiry` verified that the
rolling window reopens as the oldest reservation expires.
`TestDeferredLongTriggerGetsFirstTurnInReopenedWindow` exhausted the
budget with five-minute triggers, then verified that the deferred
15-minute trigger ran before another five-minute trigger at the
reopening, without waiting its configured interval.

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
