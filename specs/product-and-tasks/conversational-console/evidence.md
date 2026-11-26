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
