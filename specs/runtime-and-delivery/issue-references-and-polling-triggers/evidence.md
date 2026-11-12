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
