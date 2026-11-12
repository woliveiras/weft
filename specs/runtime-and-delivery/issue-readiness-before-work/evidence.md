# RUN-007 verification evidence

The maintainer approved D-01 through D-09 on 2026-11-12. Implementation
verification is pending. The matrix records the fixture-based coverage
required for each criterion.

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
