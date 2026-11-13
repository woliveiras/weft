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


Shared verification commands and results: [RUN-007 — Operational rehearsal follow-up on 2026-11-12](../issue-readiness-before-work/evidence.md#operational-rehearsal-follow-up-on-2026-11-12).

## Independent review of 944a581 through 7d54912

The 60-issue GitHub fixture verifies held-issue scope on the second search
page. The claim fixture verifies retirement of the waiting label after
selection, with unrelated labels preserved. Operator resolution checks
pending rollback and the current hold generation before recording the actor
and issue key. See the
[RUN-007 fixture cases](../issue-readiness-before-work/evidence.md#independent-review-of-944a581-through-7d54912).

Shared verification commands and results: [RUN-007 — Independent review of 944a581 through 7d54912](../issue-readiness-before-work/evidence.md#independent-review-of-944a581-through-7d54912).

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

Shared verification commands and results: [RUN-007 — Independent review of 7d54912 through 30bbaa1](../issue-readiness-before-work/evidence.md#independent-review-of-7d54912-through-30bbaa1).

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

Shared verification commands and results: [RUN-007 — Independent review of 30bbaa1 through 6ce5470](../issue-readiness-before-work/evidence.md#independent-review-of-30bbaa1-through-6ce5470).

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

Shared verification commands and results: [RUN-007 — Independent review of 6ce5470 through 29b552d](../issue-readiness-before-work/evidence.md#independent-review-of-6ce5470-through-29b552d).

Go used cached Docker images with pulls disabled. Web checks used the
installed tools and Chromium. Playwright built the web bundle and a
disposable Darwin backend. Tracker verification used fixtures.

## Independent review of 29b552d through 65f0d60

Fixture verification on 2026-11-12.

`TestTemporaryRepositoryProbeFailureDefersOnlyAffectedHold` verified
that a rate limit or transport error on the repository read deferred
the affected hold while a second hold advanced and a newly selected
issue was discovered. `TestMissingIssueConfirmationSpacingSurvivesRestartAndResetsOnSuccess`
verified that early polls simulating budget-driven rescheduling did not shorten the
configured interval required to confirm issue absence.

Shared verification commands and results: [RUN-007 — Independent review of 29b552d through 65f0d60](../issue-readiness-before-work/evidence.md#independent-review-of-29b552d-through-65f0d60).

Go used cached Docker images with pulls disabled. Web verification used
installed tools and Chromium. Playwright built the web bundle and a
disposable Darwin backend. All tracker operations used fixtures.

## Independent review of 1ac3098 through 9251f46

Fixture verification on 2026-11-12.

The Jira project fixture covered issue 404 and 410 with the reviewed
connection. A project 403 or 404 kept the hold and reported why polling
could not continue. Temporary access failures stopped retries for the
affected hold at the limit and preserved discovery for other issues.
Chromium verified that the console hid an expired retry while the page
remained open.

Shared verification commands and results: [RUN-007 — Independent review of 1ac3098 through 9251f46](../issue-readiness-before-work/evidence.md#independent-review-of-1ac3098-through-9251f46).

Go used cached Docker images with pulls disabled. Playwright used installed
Chromium and a disposable Darwin backend. Live tracker qualification remains
in PROD-14.

## Independent review of 9251f46 through abf8bc6

Fixture verification on 2026-11-12.

The tracker and runner fixtures verified that five consecutive rate
limits deferred the affected hold without spending its access retry
budget. Multiple terminal access errors remained visible by issue key.
Chromium verified the access error on its hold in the trigger details.

Shared verification commands and results: [RUN-007 — Independent review of 9251f46 through abf8bc6](../issue-readiness-before-work/evidence.md#independent-review-of-9251f46-through-abf8bc6).

Go used cached Docker images with pulls disabled. Playwright used installed
Chromium and a disposable Darwin backend. Live tracker qualification remains
in PROD-14.
