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
