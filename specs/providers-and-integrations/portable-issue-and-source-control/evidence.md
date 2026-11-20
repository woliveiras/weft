# Documentation evidence — 2026-11-20

## Request and decisions

The maintainer requested documentation before further setup questions. Both
Cloud and Data Center are in the planned Jira/Bitbucket scope. A prior project
failed on `Review` versus `In Review`; INT-009 AC-003 explicitly removes display
name assumptions through discovered, scoped workflow mappings.

## Current-state inspection

- `server/internal/trigger/issue.go` and `tracker_api.go`: Jira Cloud search and
  normalized issue/comment/label paths exist; cloud-ID/API-gateway assumptions
  do not establish Jira Data Center support.
- `server/internal/scm/scm.go` and `github.go`: provider interface/registry exists;
  GitHub is the only registered native SCM adapter.
- `web/src/components/SCMConnectionsSection.tsx`: Data Center option exists in the
  selector, without a corresponding registered adapter.
- `server/internal/scm/tools.go`: PR delivery emits
  `scm.pull_request_opened`; this internal event is distinct from authenticated
  external webhooks. The previous code-review pilot uses GitHub MCP operations.
- RUN-006 currently excludes Jira transitions/closure; RUN-005 and INT-007 keep
  fixture delivery and real-account qualification separate. INT-009 adds planned
  intent without labeling those existing exclusions as already implemented.

## Documentation changes and review

- Added planned INT-009 with eight criteria, a separate scenario/oracle matrix,
  delivery sequence, compatibility and remaining decisions.
- Routed it from the specification index, related capability owners, PROD-14
  and setup/workflow guides. Existing lifecycle/security criteria remain owned
  by their original specifications.
- Self-review checked current/planned separation, Cloud/DC distinction,
  independent issue/SCM identity, custom-name/column ambiguity, unavailable
  transitions, uncertain writes, duplicate events and exact-revision merge.
  Detailed new recovery/merge criteria remain proposed pending maintainer review;
  no independent formal approval, test review or implementation review is claimed.

## Verification and limitations

- Documentation only; no runtime code, tests, connections, policies, credentials
  or live resources changed. Unit/integration execution is not applicable to this
  edit; the matrix names the required future coverage.
- Local Python documentation checks passed: 14 relative links/anchors resolve,
  INT-009 is unique and all eight criteria have matrix rows. `git diff --check`
  passed. No adapter/live qualification, build or deployment is claimed. Exact
  public API schemas and vendor version/authentication choices must be verified
  against official references before implementation.


## INT-009-01 — Bitbucket Cloud delivery

- Scope: native `bitbucket-cloud` registry adapter, existing server-held credential
  storage/gateway tools, repository identity/permission check, bounded PR lookup,
  creation and comments. Git push uses the existing branch/approval boundaries.
  User OAuth Bearer access tokens are the initial supported authentication shape;
  OAuth acquisition/refresh and other Bitbucket credential modes are not claimed.
- Fail-first: `./scripts/go.sh test ./internal/scm -run TestBitbucketCloud -count=1`
  failed because `bitbucket-cloud` was not registered (local synthetic fixture).
  Log: `/tmp/weft-bitbucket-failfirst.log`.
- Tests exercise actual HTTP handlers, encrypted fsstore connections, the gateway
  and a temporary bare Git remote, plus pure/default/auth/transport boundary unit
  checks. Cases cover duplicate creation, foreign repositories/collections,
  malformed/oversized responses, bounded pagination, redirects, 401/403/404/429/5xx,
  invalid comments and conflict reconciliation. No live accounts are called.
- Docker `./scripts/go.sh test ./internal/scm ./internal/api ./internal/installation -count=1`
  passed (`/tmp/weft-bitbucket-cloud-all.log`). After review fixes, SCM tests passed
  again (`/tmp/weft-bitbucket-cloud-final.log`); `./scripts/go.sh vet ./internal/scm`
  and `git diff --check` passed. Go formatting was applied with the Docker toolchain.
- Subagent `review_bitbucket_cloud` found invalid empty collections, missing OPEN
  validation, unconfined pagination, HTTP Git transport and dot-segment validation
  gaps. These were corrected with regressions; final read-only review reported no
  blocking findings. Reviewer did not rerun tests and made no edits.
- No UI changes, live connection changes or deployment in this batch. Existing
  gateway events/audit and approvals remain in use; no remote response bodies or
  tokens are used as error text. Data Center and portable review remain pending.


## INT-009-02 — Bitbucket Data Center delivery

- Implemented native `bitbucket-dc` REST 1.0 delivery with site context paths,
  authenticated repository permission checks, default branch, bounded cursor
  pagination, scoped PR lookup/create/comment and existing Git push boundaries.
  HTTP access tokens use Bearer headers; API/Git TLS is verified. Shared PR URL
  validation rejects executable, credential-bearing or insecure remote links.
- Fail-first: Docker SCM tests failed with unsupported `bitbucket-dc` before the
  adapter was registered (`/tmp/weft-bitbucket-dc-failfirst.log`). Unit checks
  cover cursor validation, Git/auth defaults and unsafe links. Integration uses
  actual HTTP, encrypted fsstore and temporary bare Git; cases include read-only
  and foreign-repository permission lists, page cursor 3, invalid pages, forks,
  401/403/404/429/5xx, conflict reconciliation and closed PR rejection.
- `./scripts/go.sh test ./internal/scm ./internal/api ./internal/installation -count=1`
  passed (`/tmp/weft-bitbucket-dc-final.log`). After review fixes, SCM tests passed
  again (`/tmp/weft-bitbucket-dc-scm-final.log`); SCM vet and diff check passed.
- Subagent `review_bitbucket_cloud` reviewed this batch separately and found that
  documented PR refs can omit numeric repository IDs. Matching now uses scoped
  project/slug for PR refs while identity/permission checks still require IDs.
  Added the requested negative/read-only permission and DC pagination/conflict
  regressions. Final read-only review found no remaining blocking code defect;
  reviewer ran no additional tests.
- Target-version/live qualification, personal DC projects and custom CA entry
  remain unsupported/unqualified as documented. No live connection or deployment
  changed. Existing gateway observability/approval contracts are retained.
