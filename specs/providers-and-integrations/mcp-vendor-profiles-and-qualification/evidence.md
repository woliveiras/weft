# INT-007 historical evidence

These results were recorded at the time of the run. The documentation migration did not rerun them.

- 2026-10-17. GitHub OAuth consent, callback, persistence and discovery passed locally. Exact profile and file-read grants worked with a controlled model and local Ollama; the live read covered a public repository README. Private access, writes and renewal were outside the run.
- 2026-10-18. Opt-in anonymous discovery recorded DeepWiki 3 tools, Hugging Face 4 and Context7 2. DeepWiki and Context7 public reads verified exact grant enforcement. BigQuery stateless discovery returned 9 tools without running a query. Snowflake was documentation review only.


## Atlassian confidential DCR selection — 2026-11-23

Live recovery of rehearsal-jira-oauth returned HTTP 201 and a locally authored
incompatibility field list containing only client_secret. No rejected value was
retained or inspected. The administrator-approved correction requests an
advertised confidential method for the canonical Atlassian server-side profile;
none of the response acceptance checks are loosened.

- Independent spec and implementation reviews found no blocking issue.
- Docker `./scripts/go.sh test ./internal/mcpconfig ./internal/api -count=1`
  passed. Unit cases cover Basic/Post preference, public fallback, foreign
  resources and mismatched issuer/authorization/token metadata. Real TLS fixture
  integration validates per-client secret use at exchange, refresh and revoke,
  encrypted persistence across restart, failed authentication, redacted views
  and unchanged manual/public GitLab/CIMD contracts.
- Live outcome remains separate: initial method selection and fixture acceptance
  do not establish successful Atlassian consent, tool invocation or PR delivery.

## Atlassian consent identity scopes — 2026-11-23

The live DCR client reached consent, but the provider returned invalid_request
after Accept without reaching the Weft callback. Official Atlassian bug
[ID-9242](https://jira.atlassian.com/browse/ID-9242) documents this outcome when
read:me, read:account or offline_access is absent and gives an explicit scope
workaround. The saved rehearsal list lacked read:account. This identifies an
upstream consent requirement; it does not establish a successful new consent.

- Regression tests first failed because the defaults and rendered form omitted
  read:account. The correction adds it to the explicitly selected Atlassian
  family defaults and explains the requirement in the provider setup guide.
- Docker Node 22 bookworm focused Vitest passed: mcpServices.test.ts and
  MCPSection.test.tsx, 18 tests. TypeScript `tsc --noEmit` passed. Tests cover
  defaults isolation, explicit selection, visible scope submission through the
  real API client and preservation of a saved narrower scope list. Existing
  invalid-input, dependency-failure and public OAuth assertions remain intact.
- Docker `./scripts/go.sh test ./internal/mcpconfig -count=1` passed (2.010s).
  The real TLS DCR/authorization integration asserts identical explicit scopes
  on registration, persisted configuration and authorization, including a
  deliberately narrow API caller to prove the backend does not expand scopes.
- Full Docker web TypeScript, Vitest and production build also passed after
  the focused checks; the existing bundle-size warning remains unchanged.
- Updating a real connection and confirming provider consent remain separate
  authorized operational steps. No live credentials or account configuration
  were changed by this regression fix.
