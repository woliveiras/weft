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

Device and assistive-technology review, external provider qualification and
production backup and restore remain open.
