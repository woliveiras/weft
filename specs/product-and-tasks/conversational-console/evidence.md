# Conversational Console delivery evidence

## 2026-11-26 — Explicit Chat model selection

S23 exercises the configuration part of AC-003.

- `./scripts/go.sh test ./internal/api -run 'TestChatModelSettingsAPI|TestValidateChatModel' -count=1` passed. The test crossed authenticated HTTP, the actual model registry and SQLite. It verified the revision-zero initial selection, viewer denial, rejection of extra inference parameters, stale-write rejection, independent workflow default and preservation of a now-unavailable selection. The integration first reproduced a missing route before implementation.
- Web TypeScript checking and the focused Vitest suites for ChatModelSection, modelsNavigation and InstallationPages passed in the Node 22 fixture. The eight tests include preservation of the draft after a stale write.
- Playwright passed `chat-model.spec.ts` in Chromium against a disposable Go/SQLite installation. The administrator saved a model in the browser. A new viewer browser context read the same selection.
- `make lint`, `make build`, OpenAPI parse and role lookup, and `git diff --check` passed.

This slice checks explicit selection and registry availability. No inference
or external provider qualification was exercised. Conversation interpretation,
ownership, monitoring and session presentation require their own evidence.

## 2026-11-26 — Conversational task tracking

- Targeted Go tests for Chat interpretation, session persistence and API
  ownership passed in the repository's Docker Go toolchain. SQLite reopened
  the same store, loaded an archived watch and recorded one failed result from
  the task manager despite two reconciliation passes. The filesystem store
  admitted one session under concurrent retries. HTTP integration covered
  distinct owners, forbidden cross-owner operations, archive and shared mode.
- TypeScript checking and 35 focused Vitest checks passed. These include the
  default route and invalid-route recovery, manual navigation, onboarding
  removal, model handoff, accessible session status and disclosure.
- Chromium on three independent fixture installations passed both
  `chat-session.spec.ts` journeys. The task-tracking journey used the real
  browser, API, SQLite, task engine and watch worker with a scripted model
  provider. The browser switched sessions and closed before approval. The
  server recorded an unread terminal result before the session was reopened.
  The two-tab test reproduced stale history before the identity fix and
  verified that page state clears on a token change.
- Chromium passed 15 existing navigation, OAuth, onboarding and layout
  regressions. `make lint`, `make build`, the OpenAPI parse and
  `git diff --check` passed.

This slice verifies the task-monitoring portion of S11.

A later full Chromium comparison recorded 162 passed, 18 failed and 7 skipped
on the session commit (1542352). Its pre-implementation baseline (140bb61)
recorded 159 passed, the same 18 failures and 7 skipped. This comparison
found no additional failures. The broad browser suite remains failing.

## 2026-11-26 — Reviewed catalog agent creation

- Targeted Go unit and HTTP/SQLite integration passed. The checks covered the
  exact digest, expiry, role denial, explicit Portuguese confirmation, multiple
  pending-review ambiguity, refusal by button, replay, revision changes and a
  durable catalog receipt. The fixture also verifies that an unpriced call is
  admitted according to the existing policy when no monetary budget applies,
  and deferred when an applicable budget refuses unpriced calls.
- The TypeScript check and 24 focused Vitest checks passed. The reviewed card
  displays the instructions and read-only effect and submits a scoped decline.
- Chromium passed the full S11 fixture journey. An authenticated task watch
  continued while the user opened another session, reviewed and created a
  reusable agent, closed the browser and returned to the original result.
  The API observed exactly one created version with empty tool grants.
- `make test` passed, including 830 web tests, alongside `make lint`,
  `make build`, the OpenAPI parse and `git diff --check`. Live-provider
  environment overrides were excluded from the fixture run.

## 2026-11-26 — Indexed session lists and watches

- Targeted Go tests crossed SQLite and the filesystem store. Retained records
  had identical content and revisions after backfill, and the cursor kept its
  boundary after that session was archived. Reopened archived watches reported
  one unread result in Recents and cleared the count after a read marker.
- The same index test and storage conformance passed against an isolated
  PostgreSQL 17 instance. PostgreSQL qualification used a disposable fixture.
- Browser fixture replay passed, including an archived watch that kept running
  and produced one unread result. Restoring opened the same conversation in
  Recents. TypeScript and 21 focused web tests passed, including the Archived
  selector count and paging.
- `make lint`, `make build`, the OpenAPI parse and `git diff --check` passed.

## 2026-11-26 — Manual inspection from a conversation

A bounded destination selector now opens a known console page from the reply.
Unit tests rejected arbitrary routes and mixed action/navigation payloads.
HTTP and SQLite integration preserved the appropriate destination separately
for the viewer and administrator. Chromium followed the link to Budgets and
returned by browser Back to the same message with focus restored. TypeScript
and seven component tests passed.

## 2026-11-26 — S17 manual PR analysis

An authenticated HTTP/SQLite fixture verified that a viewer could not trigger an SCM read. A new PR head invalidated confirmation. A fresh proposal dispatched the pinned diff to one native workflow execution, and the chat returned its draft and execution ID. Repeated confirmation generated no additional analysis. The synthetic SCM received only GET requests. Native runtime tests rejected tool calls in review responses. External targets are outside this validation by maintainer decision.

## 2026-11-26 — Installation inspection from Chat

Authenticated HTTP and SQLite covered the 21 read-only resource types as a viewer with no selected Chat model. A scripted natural-language query returned the same saved workflow revision as the explicit read. Unit tests covered length limits, filtering sensitive values, malformed commands and forbidden mixed intents. Chromium navigated from the returned inventory to Workflows and back to the originating link.

## 2026-11-26 — Workflow-local instructions

For S14, HTTP and SQLite checks retained the catalog pin and prior graph version. An intervening edit invalidated confirmation. Two confirmations produced a single new revision. Chromium reviewed both instruction texts, saved the change and returned from the selected node to the originating chat.

## 2026-11-26 — One-time guidance to a running node

S14 now has fixture evidence for reviewed, run-scoped messages. A viewer could not prepare a send. Changing the run invalidated confirmation. Postgres 17, SQLite and filesystem conformance verified single pickup and exclusion of other runs and replicas. Chromium followed a live fixture from review through actual pickup and checked the durable node-chat receipt. The shared event carries message and run identifiers.

## 2026-11-26 — Installation configuration review

An HTTP and SQLite fixture exercised S18 with a three-object plan. Fault injection after the first write preserved the applied, failed and untouched outcomes. Replaying confirmation left the previous results in place. A manual edit invalidated the next review, and a fresh review then applied successfully. Chromium kept the task budget unchanged until confirmation and displayed the object receipt. No provider, external account or persistent installation was used.

## 2026-11-26 — Independent title retry

S26 passed targeted Go unit and HTTP/SQLite integration. A title-only model
call used the first request, preserved the approved review and recency, and
left the catalog unchanged. A repeat returned the recorded title without a
second model call. Filtered or malformed model output could not introduce an
operation. TypeScript, five focused component tests and OpenAPI parsing passed.

## 2026-11-26 — Reviewed workflow creation

S27 passed the Go unit and authenticated HTTP/SQLite integration checks. The
review preserved the explicitly selected catalog version after another version
was published. Repeated confirmation returned the same stopped workflow and
left the manual editor's intervening save intact in the collision scenario.
Chromium followed the review through the real API and store, opened the saved
canvas, and returned to the original conversation. TypeScript, nine focused
component tests, OpenAPI parsing, `make lint` and `git diff --check` passed.

## 2026-11-26 — Reviewed task drafts

S28 passed targeted Go unit and HTTP/SQLite integration checks. The operator
confirmed one task draft, replay returned its original ID, and the workflow
revision and input remained pinned. The test tried a concurrent editor save,
a viewer decision and a different owner's token. None admitted a task. The
Chromium journey saved a workflow, returned from its canvas, reviewed a task
on that version, and opened the saved draft in Tasks. The full Go suites for
`chat`, `taskconfig` and `api` passed. TypeScript, ten focused component
checks, OpenAPI parsing, `make lint`, `make build` and `git diff --check` passed.

## 2026-11-26 — Reviewed local task start

S29 passed the Go suites for `chat`, `taskauth`, `taskrun` and `api`. The
HTTP/SQLite journey admitted the approved revision once, retained the human
gate and appended the observed execution result on approval. A changed
authorization revision left the second task unstarted. Reopening SQLite
preserved an archived admission claim in the detached observation queue.
Chromium replayed the task start through the independent approval and
execution evidence. TypeScript, eleven focused component checks and OpenAPI
parsing passed.

## 2026-11-26 — Reviewed workflow runtime controls

S30 passed targeted Go unit and HTTP/SQLite checks. A reviewed workflow
started one run; pausing one node left the workflow and sibling active. A
manual graph save invalidated the old resume proposal. A fresh proposal
resumed the node, and the graceful stop settled through the native engine.
Chromium opened the reviewed runtime controls as an operator, verified the
observed state, inspected the exact node and returned to the conversation.
TypeScript, ten focused component checks and OpenAPI parsing passed.

## 2026-11-26 — Reviewed task retry

S32 passed targeted Go tests for the review, API and native retry. HTTP and
SQLite preserved one linked draft on repeat, fenced a changed source
revision, and left the original consent separate. Chromium rejected a
fixture task's held input, prepared a retry only after confirmation and
opened the linked draft in Tasks. TypeScript, eleven component checks,
OpenAPI parsing and `git diff --check` passed.

## 2026-11-26 — Shared catalog publication

S33 passed targeted Go unit and HTTP/SQLite tests. Repeating the review
returned the same immutable catalog version. The original version, saved
workflow pin and local override remained intact. A publication through the
manual API invalidated a pending review. Chromium verified the before and
after text, published version 2, inspected the preserved workflow pin through
the API and returned from Agents. TypeScript, twelve focused component
checks, OpenAPI parsing and `git diff --check` passed.

## 2026-11-26 — Catalog adoption on a workflow node

S34 passed targeted Go unit and real HTTP/SQLite tests. A node adopted the
requested version after a further publication. The sibling pin, parameters
and local override stayed intact. The save recorded its operation and fenced
another workflow identity. Versions with other shared changes went to the
manual editor. Chromium published a version, then adopted it after a separate
review and returned from the selected node. TypeScript, twelve focused
component checks, OpenAPI parsing and `git diff --check` passed.

## 2026-11-26 — Reviewed pending approvals

S35 passed focused Go unit, HTTP/SQLite and storage conformance checks on
SQLite, filesystem and a disposable PostgreSQL 17 instance. The earlier native deadline bounded confirmation.
The exact answer was recorded once, and a decision made in Approvals left
an old Chat confirmation without effect. Chromium approved held task input
as an operator, verified the native receipt and observed task success
through its separate link. TypeScript, eleven component checks, OpenAPI
parsing, `make lint`, `make build` and `git diff --check` passed.

## 2026-11-26 — Reviewed task cancellation

S36 passed targeted Go unit and authenticated HTTP/SQLite integration.
The cancellation closed the selected task's held input, retained its
history and left the other run and its approval active. A durable request
saved before engine delivery settled through background reconciliation.
Repeated confirmation reused the original observation. Chromium saw one
observed cancellation and returned from the saved task. TypeScript and
eleven focused component checks passed.

Device and assistive-technology review, external provider qualification and
production backup and restore remain open.


## 2026-11-26 — Native archive and restore fixture

The native Chromium journey opened an unconfigured Chat, inspected the installed workflows, archived its session and restored it from Archived. A disposable format-1 installation from pre-Chat commit `140bb61` preserved the archived transcript and its index after upgrade, restart, offline backup and restoration. The historical upgrade target still references `faf4644`, which is absent from this checkout. The native browser suite also reproduced the same nine manual-journey failures at `140bb61` and with Chat installed.

## 2026-11-26 — Task draft corrections

For S16/S28 the HTTP and SQLite fixture preserved the workflow binding and the draft lifecycle after a reviewed text correction. A viewer received no review. A repeated confirmation returned the same revision and a stale proposal conflicted. Unit checks covered generated and custom channel payloads, invalid fields and sensitive source text. Chromium reviewed both texts, saved the correction, checked the resulting task via HTTP and reopened it in Tasks.


## 2026-11-26 — PR 418 across three local SCM adapters

The same authenticated Chat-to-execution journey passed for GitHub, Bitbucket Cloud and Bitbucket Data Center against loopback HTTP fixtures. Each exposed its own metadata and immutable diff route. A viewer made zero SCM requests, a changed head invalidated the initial review and a confirmed replacement returned one analysis and its execution ID. Replaying the confirmation made no further model call. The fixtures recorded zero SCM writes. This evidence is limited to simulated providers.


## 2026-11-26 — Task environment preparation

For S16 an administrator reviewed the saved task, environment definition and workflow version. The HTTP/SQLite and Chromium journeys used a simulated container daemon and observed matching readiness. Repeated confirmation delivered the same receipt. A stale proposal conflicted, an operator received no build action, and the task still had zero executions. Unit checks rejected unsafe or oversized definitions and mixed start/build intents.

## 2026-11-26 — Reviewed task authorization

S37 passed targeted Go unit tests and authenticated HTTP/SQLite integration.
The review bound the saved task and authorization revisions, mode, workflow
version and tool scope. An operator was denied, a concurrent native change
invalidated the proposal, and replay returned the same receipt. The running
fixture accepted only a reduction. Chromium verified the new record and found
the same current mode in the task's Settings & access while the task remained
a draft. TypeScript, eleven focused component checks and OpenAPI parsing passed.

## 2026-11-26 — Reviewed current budget increases

For S38, Go tests verified exact micro-unit amounts, actor and scope binding,
stale ledger and definition conflicts, and the UTC month boundary. With a
real SQLite ledger and a fixture model, authenticated HTTP verified one native
raise and audit event across repeated confirmation. Chat observed the original
run resume in one fixture and reported a different applicable budget blocking
it in another. Chromium confirmed 2.004005 USD, checked the stored allowance
and the separate default for new tasks, and returned from Models to the review.

## 2026-11-26 — Reviewed replica policy

S39 passed unit checks for exact counts, template limits, policy selection
and confirmation binding. Authenticated HTTP and SQLite observed three fixed
replicas through the engine after review, preserved a manual scale made after
another proposal, drained only the selected node at zero and saved one new
version for the autoscale ceiling on a stopped workflow. Viewers and out-of-policy
counts received no review. The affected Go packages, TypeScript, 14 component
checks, OpenAPI parsing, `make lint` and `git diff --check` passed.
Chromium confirmed both effects and restored focus on return from the selected
node in the workflow canvas.

## 2026-11-26 — Existing project settings

S40 passed the project settings unit and authenticated HTTP/SQLite checks
against a registered local Git fixture. A partial preference change used the
existing source and preserved its checks. A later checks review conflicted
after a native edit. A fresh plan recorded the exact replacement once while
preserving the new preference and the same project and commit. Unchanged
source paths and commands stayed out of the transcript.

## 2026-11-26 — Reviewed issue intake controls

S41 passed the typed consent and safe-query unit checks. Authenticated HTTP
and SQLite verified a native pause with the stored consent and existing task
reference, a failed resume after a workflow edit, then a successful resume
with the new workflow version and scope. The operator role received no review.
Repeated confirmation left one native pause and one resume audit. Chromium
verified both states, the fresh consent under the local fixture actor and focus
restoration from the workflow's Triggers section.

## 2026-11-26 — Reviewed reusable workflow environment

For S43, Go checked exact version and hash binding, rejection of force or
mixed task intents, and credential-shaped definition redaction. Authenticated
HTTP and SQLite used a fixture daemon to verify successful and failed native
builds, zero builds from a stale or viewer request, one operator audit after
confirmation and reuse of the ready matching definition. The workflow stayed
stopped. Chromium passed the task and workflow environment journeys and
returned from the workflow's Environment settings to the same review.

## 2026-11-26 — Session continuity and scope

Authenticated HTTP and SQLite tests exercised a task draft with multiple
registered Git repositories and rejected confirmation after a project changed.
A separate execution test cancelled a selected delegated child, recorded the
request once and observed settlement while its parent remained active.

The inventory follow-up test selected the first workflow from the order shown
in the same conversation. The role-change integration reopened a stored
review as the same principal with fewer permissions. The unit test checked
that the filtered conversation also omits restricted data from the next
model request.

Chromium exercised full generated titles in the desktop rail and in a
375-pixel touch drawer. It verified single-line overflow, pointer access to
the preview, keyboard dismissal and focus retention. The reduced-motion
journey checked the same session without horizontal page overflow.

## 2026-11-26 — Console regression comparison

The full Chromium fixture run at `16fa3c0` recorded 190 passes, 18 failures
and seven skips. The pre-Chat checkout `140bb61` recorded 159 passes, 18
failures and seven skips. The eighteen failing test names matched exactly.

The final `make test` at `6801e13` passed the Go suites, 40 Python tests,
web type-check and all 856 Vitest cases across 140 files. The stale-model
settings test waited for the provider option and verified that Save was
enabled before exercising the conflict.

After formatting the session-title fixture, `make lint`, `make build`,
Playwright type-check and all 33 Chromium Chat journeys passed at `796138a`
in a clean checkout. The browser and fixture services shared an isolated
loopback network. The documentation check resolved 37 added local links,
and the OpenAPI document resolved all 432 references. External SCM accounts,
production OIDC and physical assistive technologies still require separate
qualification.
