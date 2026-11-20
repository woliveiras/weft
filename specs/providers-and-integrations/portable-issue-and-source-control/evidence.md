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

## INT-009-03 — Edition-aware configuration

- Added authenticated read-only `/api/v1/scm/kinds` with registered built-in
  delivery capabilities and edition-specific URL/repository/credential guidance.
- Console creation handles loading, unavailable/empty catalogue and retry, preserves
  GitHub defaults, requires the Data Center site URL and clears unsaved credentials
  and API/Git overrides when switching editions. No connection is tested implicitly.
- Go units plus real API/storage/YAML round trips passed:
  `./scripts/go.sh test ./internal/scm ./internal/api ./internal/installation -count=1`.
- `make test-web` passed (typecheck and Vitest), including loading, empty, failure,
  retry and edition switching. Chromium connection journeys passed (4 tests), using
  the disposable Go backend and saved synthetic credentials; no vendor was called.
- `./scripts/go.sh vet ./internal/scm ./internal/api` and `git diff --check` passed.
- Subagent review found no blockers; its suggestions to document the endpoint and
  cover catalogue loading/empty states were incorporated and checked. React and
  product UI/UX checklists were applied; no accessibility-conformance claim is made.
- Metadata describes implementation availability, not live permissions or supported
  production versions. Portable review and merge are not advertised as delivered.

## INT-009-04a — Portable PR snapshots and immutable diffs

- Added `scm.pull_request.read`, requiring the exact SCM connection grant; no
  writes or implicit grants. GitHub and both Bitbucket editions normalize PR
  revision identity and refuse wrong repositories, unknown/incomplete states.
- Diffs address immutable source/base hashes; PR identity is rechecked afterward.
  All responses are bounded; Data Center structured truncation flags and response
  hashes are checked, including nested `truncated` and string flags. Secrets
  reflected in repository text are removed before JSON encoding (including nested
  structured diff strings).
- Unit and HTTP/fsstore/gateway integration tests cover all editions, changed
  head/base, denied responses, invalid JSON/identity, oversize, scoped denial,
  unavailable adapters and nested truncation/redaction. SCM, API, taskrun and tools
  checks passed with `./scripts/go.sh test ./internal/scm ./internal/api ./internal/taskrun ./internal/tools -count=1`; `./scripts/go.sh vet ./internal/scm ./internal/api` and `git diff --check` passed. Final subagent review found no remaining blockers.
- Review identified the Cloud PR patch redirect and before/after-only ABA race;
  replaced these with immutable compare endpoints. It also identified escaped
  and nested JSON reflection; decoding/redacting before serialization and decoded
  output regressions address those paths.
- Contracts: [GitHub PR API](https://docs.github.com/en/rest/pulls/pulls),
  [Bitbucket Cloud comparison](https://developer.atlassian.com/cloud/bitbucket/rest/api-group-commits/),
  [Data Center REST 1.0 structured diff](https://docs.atlassian.com/bitbucket-server/rest/7.15.3/bitbucket-rest.html).
  Fixture references do not qualify a live account or production version.
- Summary publication, guarded mutation and event adoption remain in batch04;
  this batch alone does not satisfy AC-005/006.

## INT-009-04b — Confirmed review summaries

- Added scoped `scm.pull_request.review` across GitHub and both Bitbucket editions.
  It publishes a revision-labelled verdict/summary comment; provider approval and
  automatic merge remain separate operations and are not claimed here.
- Durable CAS reservation precedes the only POST. Retries reconstruct the provider
  and reconcile the exact marked comment. Timeout-after-write, failed writes,
  unavailable comment lists and read/reservation/confirmation storage failures are
  covered by HTTP + real fsstore integration fixtures for all three adapters.
- Tests cover simultaneous reservation attempts (one POST), stale revision,
  cross-connection scope, verdict overwrite, pending comments and URL credential
  reflection in result, stored receipt and audit events. Unit coverage includes
  input boundaries, receipt identity and encoded credential redaction.
- `./scripts/go.sh test ./internal/scm ./internal/api ./internal/taskrun ./internal/tools -count=1`
  passed; final expanded SCM regressions passed separately. Vet for SCM/API and
  `git diff --check` passed. Subagent review found no residual blockers after fixes.
- An unconfirmed reserved attempt never reposts automatically. Its outcome must
  be reconciled or investigated by the operator; no deletion/reset capability was
  added. No real external comment, connection change or workflow grant was made.

## INT-009-04c — Automatic merge capability boundary

- Subagent review rejected the uncommitted merge prototype: an expected-head
  mutation does not atomically constrain the destination. A concurrent retarget
  can preserve the same head while changing the branch being updated. Preflight
  checks and post-write readback cannot prevent that unauthorized effect.
- The prototype mutation is not shipped. `scm.pull_request.merge` returns an
  explicit unavailable result without contacting a provider; `automaticMerge`
  is false for all current built-in adapters. Wildcards do not authorize this
  capability. Existing pilot workflows/grants were not changed.
- Remediation: qualify a remote operation that enforces the reviewed head and
  authorized destination atomically, including provider rules without a bypass.
  Until then, review summaries remain usable and merge uses the provider's
  separately authorized review process. AC-005 automatic merge remains blocked.
- Contract evidence: [GitHub REST merge](https://docs.github.com/en/rest/pulls/pulls#merge-a-pull-request)
  documents an expected head SHA; [GraphQL PullRequest](https://docs.github.com/en/graphql/reference/pulls#pullrequest)
  documents bypass capability. Neither establishes an atomic expected destination
  for the attempted implementation. No production version was qualified.
- SCM/API/tools Docker tests passed (`/tmp/weft-merge-gate.log`). Unit coverage
  checks capability metadata; integration invokes the real gateway/store for all
  three editions and retries, asserting zero remote calls and wildcard refusal.
  Final subagent review found no blockers; `git diff --check` passed.

## INT-009-04d — Portable delivery identity

- Existing PR-opened events retain delivery fields and now include `provider`,
  `instance`, `connectionRevision`, `headSha`, `baseSha` and
  `reviewRevisionAvailable`. Commit IDs come from create/lookup responses; no
  extra network call can turn a delivered PR into a failed delivery.
- Legacy/incomplete responses still record delivery but cannot supply an
  automatic review revision. Availability means complete revision identity, not
  an eligible/approved PR: review tools still read and validate state and draft.
- Portable read returns the connection revision. Read and summary accept the
  optional `connectionRevision` from the event/snapshot, rejecting reconfigured
  connections before opening credentials or calling a provider. Existing callers
  remain compatible; adopting event routing must carry this fence explicitly.
- Unit tests cover response conversion, edition instance and incomplete identity.
  HTTP/fsstore/gateway integration covers all three adapters, matching revisions,
  actual connection updates, stale identity, legacy missing hashes and reflected
  credentials that themselves look like valid commit IDs.
- SCM/API/taskrun/trigger/tools Docker tests passed
  (`/tmp/weft-portable-event-reviewed.log`). Subagent review found reflected
  hexadecimal credentials in commit IDs; redaction before validation and decoded
  result/event regressions fixed it. Final review reported no residual blockers;
  `git diff --check` passed. No live routing, grants or deployment changed.

## INT-009-05a — Trusted issue origins and provider pairing

- Trigger-created tasks retain a separate trusted issue origin: product, API
  instance/cloud scope, connection and consent revision, canonical key and link.
  Editable request/PromptInput cannot replace it. Save/SavePrompt and storage
  restart preserve this snapshot independently of the selected SCM connection.
- PR bodies use closing syntax only for the public GitHub-to-public-GitHub pair;
  other pairs receive an ordinary source issue reference. Historical GitHub tasks
  preserve their prior behavior. Invalid stored origins refuse PR creation.
- Jira tasks now require PR delivery just as GitHub issue tasks do: a successful
  agent turn without the required PR is reported as blocked.
- Units cover scoped identity and pair selection. fsstore restart/edit integration
  and gateway + HTTP fixtures cover GitHub/Jira origins with all three SCM
  adapters. A real app trigger launch/echo execution checks origin derivation
  from consent, absence of implicit MCP reads/grants, and blocked delivery state.
- SCM/taskconfig/app/taskrun/API tests passed (`/tmp/weft-origin-reviewed.log`);
  added launch integrations passed (`/tmp/weft-origin-launch.log`). Vet for
  SCM/taskconfig/taskrun/app and diff check passed. Subagent found permissive
  Cloud prefix extraction; exact prefix validation plus pre-persistence rejection
  tests fixed it. Final review found no residual blockers.
- This does not complete AC-001: admission-key namespacing, Jira DC support and
  operator configuration remain pending. No live task/connection/grant changed.

## Event contract regression correction

- Full Go verification caught the summary event missing from the public event
  enum/catalogue. Added `EvReviewSummaryPublished`, the schema enum and catalogue
  entry; documented the expanded PR-delivery payload without changing emission.
- Domain event/schema consistency and SCM unit/HTTP integration tests passed
  (`/tmp/weft-event-catalog.log`). Subagent review found no blockers; diff check
  passed. Full-suite rerun follows the correction.

## INT-009-05b-base — Jira Data Center REST boundary

- Added isolated native REST 2 client for HTTPS sites/context paths and numeric
  project scope, using its own PAT Bearer credential. Identity, issue/search,
  bounded comment history and one-shot comment publication have TLS HTTP fixtures.
  This base is not wired to app connections, triggers, console or YAML yet and
  is not advertised as an operational Jira DC connection.
- Reads validate numeric issue/project/type/status identity; status display names
  remain presentation data. Subsequent comment operations address the resolved
  numeric issue ID, so a key alias cannot retarget them.
- Project validation is a preflight, not an atomic project-membership guarantee.
  Adoption requires a remote principal restricted to the reviewed project and
  durable write reservation/reconciliation. A broad PAT must not be described as
  project-isolated by this client. A timeout never triggers an automatic POST retry.
- Bounds: verified TLS, 10-second request deadline, no redirects, 1 MiB responses,
  bounded search offset and 10 comment pages. Invalid/partial/duplicate histories
  fail without partial evidence. Decoded/URL-encoded credential reflection fails.
- Unit and TLS HTTP integration tests passed (`/tmp/weft-jiradc-reviewed.log`);
  Jira DC vet and diff check passed. Subagent found empty-query URL acceptance
  (`ForceQuery`) and requested negative comment pagination cases; fixed and tested.
  Final review found no residual blocker for this isolated client.
- References: [REST 2 resource shapes](https://docs.atlassian.com/software/jira/docs/api/REST/9.12.0/)
  and [Data Center authentication](https://developer.atlassian.com/server/jira/platform/rest/v10000/).
  These references describe fixture provenance, not supported production versions.

## INT-009-05b — Native Jira connection storage and API

- Native Jira Data Center connections store their own sealed PAT and numeric
  project/site scope, independent of MCP and SCM credentials. Associated data
  binds ciphertext to that scope; changing scope requires a replacement token.
- Viewer read, admin create/update and operator identity-test routes enforce
  revisions, reject unknown/trailing JSON and hide credential values. Disabled
  and stale tests fail before credential decryption or remote dispatch.
- Unit and real fsstore/API integration coverage includes invalid inputs,
  role denial, CAS conflicts, audit/storage failures, safe views and ciphertext
  audience mismatch. No live connection, credential or policy was changed.
- This batch is configuration infrastructure. Console, YAML and operational
  trigger adoption remain pending; an identity test does not establish project
  permissions or production-version support.
- Docker `./scripts/go.sh test ./internal/jiradc ./internal/api ./internal/app ./internal/domain -count=1`
  passed; the added TLS/API checks passed separately with `-run TestNative`.
  Vet for Jira DC/API/app and diff checks passed. Subagent review requested the
  positive revision fence and additional role/ETag/TLS cases; implemented and
  verified. Final read-only review found no residual blockers.

## INT-009-05c — Jira Cloud processed-history scope

- New Jira processed/readiness history keys include the reviewed cloud ID.
  Returning to a previously processed site retains its outcome; query or
  credential-connection selection does not create a new remote resource.
- Historical Jira entries without a cloud ID remain conservative fences;
  their original site is not guessed. Legacy retained readiness allowances
  remain effective, including exhausted rounds, snapshot edits and intervention.
  No persisted history is deleted or rewritten by this change. The global
  admission journal already includes cloud ID and is unchanged.
- Subagent review identified the indirect readiness-key migration; added the
  conservative fallback plus an integration regression proving zero additional
  assessments/starts with exhausted legacy allowances. Final review found no
  residual blockers. Units cover identities/fallback; integration uses the real
  trigger manager, gateway and fsstore across site edits and completion.
- Docker `./scripts/go.sh test ./internal/trigger ./internal/api ./internal/taskconfig -count=1`
  passed after the compatibility fix (`/tmp/weft-jira-scope-final-v3.log`), as did
  the isolated exhausted-legacy regression and trigger vet. Diff check passed.

## INT-009-05d — Declarative native issue connections

- `IssueConnection` supports native Jira Data Center site/project configuration
  and secret references. Dry-run validates the same inputs as the API without
  network calls, events, settings or credential sealing. Apply uses the native
  CAS revision and makes no identity/permission test or tracker write.
- Export contains credential-free public fields and `secretRef: {preserve: true}`.
  Unchanged referenced values are compared without resealing; missing secrets,
  stale baselines and site/project changes with preserved credentials fail.
- Unit checks cover effect-free normalization and credential comparison; API
  integration exercises real plan/apply/export/fsstore, no-op reapply, stale
  revisions, scoped replacement and credential-free events. The existing all-kind
  round trip includes the new document; parser units cover its kind registration.
- No operational Jira DC trigger, agent grant or live setup is implied. The
  new kind is additive; existing declarative objects retain their contracts.
- Docker `./scripts/go.sh test ./internal/jiradc ./internal/installation ./internal/api -count=1`
  passed (`/tmp/weft-jira-yaml-reviewed-v1.log`), as did vet for those packages
  and `git diff --check`. Read-only subagent review found no blockers; the new
  kind and operational limits are documented in the API/declarative guides.

## INT-009-06a — Read-only Jira DC transition discovery

- Added issue-specific discovery through numeric issue IDs and
  `expand=transitions.fields`. Site, project, issue type and current status IDs
  accompany available transition/target IDs; names remain display text, including
  `Review`, `In Review`, localized and duplicate labels. No name matching,
  automatic transition, mapping editor or mutation is provided in this batch.
- Known-empty fields differ from unavailable metadata. Required fields and a
  bounded schema/operations/allowed-value subset are retained. This subset is
  not a complete editor schema or authorization to write arbitrary fields.
- Scope and issue identity are checked before discovery and re-read afterward;
  changed status/type/update, foreign project, permission denial, missing/invalid
  metadata and credential reflection fail without returning partial evidence.
- Jira DC Docker unit and TLS HTTP integration tests passed
  (`/tmp/weft-jira-transitions-v1.log`). Read-only subagent review found no
  blockers and highlighted the partial-schema boundary, now documented.
- Contract provenance: [Jira REST 2 transitions](https://docs.atlassian.com/software/jira/docs/api/REST/9.12.0/#api/2/issue-getTransitions).
  This is fixture coverage, not version qualification. Optional mappings,
  synchronization recovery and Cloud discovery adoption remain pending.

## INT-009-05e — Project-restricted native write prerequisite

- The maintainer explicitly approved requiring a project-restricted technical
  account for automatic Jira DC writes. Added a default-false
  `projectRestricted` administrator confirmation, persisted/exported with the
  connection and included in the credential-free change-intent audit.
- Directly constructed clients and `Connections.Open` are read-only. A write
  client can be opened only with enabled/current revision and confirmation,
  checked before decryption. `AddComment` denies read clients before HTTP.
  Write clients revalidate the revision/enable/confirmation before preflight and
  again before mutation dispatch; cached clients cannot skip a later edit. A
  request already dispatched can still finish.
- The confirmation records an operator review; it is not an automatic proof or
  remote permission change. Tool grants, per-call revision checks and durable
  uncertain-write reconciliation remain necessary for trigger adoption.
- Units cover read-only construction; fsstore/TLS integration covers absent,
  confirmed, revoked and stale confirmation plus exactly one numeric-issue POST
  and no POST through a read client. YAML integration round-trips confirmation;
  existing documents/records default to false.
- Docker Jira DC/API/installation tests passed
  (`/tmp/weft-jira-restricted-v4.log`). Subagent review initially highlighted
  cached clients; revision checks now also run per mutation, with revocation
  during TLS preflight coverage. Final review found no residual blockers.
