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

This verifies the task-monitoring portion of S11. The agent creation and
confirmation in that scenario remain pending. Also pending are indexed
recents and due watches at large history sizes, the full independent
title retry flow, physical-device and assistive technology checks,
live provider qualification, and production backup and restore.
