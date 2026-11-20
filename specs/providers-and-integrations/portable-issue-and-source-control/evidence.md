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

## Regression checkpoint after native write gate — 2026-11-20

- `./scripts/go.sh test ./...` passed with live model URL/key variables removed
  from the process environment (`/tmp/weft-portability-go-milestone15.log`).
  No live provider or broker qualification is claimed by these local fixtures.
- `make test-web` passed: TypeScript and 103 Vitest files / 552 tests
  (`/tmp/weft-portability-web-milestone15.log`). A previous full run had an
  intermittent Skills history-test failure; the isolated test and subsequent
  full runs passed without changing that unrelated code or weakening the test.
- `make test-python` passed: SDK 16, example agents 14 and scripts 10 tests
  (`/tmp/weft-portability-python-final.log`).
- `make lint` and `make build` passed after the native write gate
  (`/tmp/weft-portability-lint-milestone15.log`,
  `/tmp/weft-portability-build-milestone15.log`). No stack deployment or push.
- The 15 implementation/fix commits from `5d19387` through `807234a` were
  separately reviewed by the read-only subagent before commit. Each behavior
  batch has unit and component-boundary integration coverage recorded above.
- The overall INT-009 capability is **not complete**. Remaining implementation:
  native DC trigger/console integration, DC operational admission identity,
  optional mapping configuration and durable tracker synchronization recovery,
  integrated event/workflow adoption and code-review migration. Automatic merge
  has the concrete atomic-destination blocker in batch04c. Live/version/pair
  rehearsals require separately authorized resources; no version is inferred.
- Checkpoint documentation received a final read-only subagent review: statuses,
  commit range and test-log totals were checked without rerunning commands;
  no blockers. Eighteen local Markdown link targets and diff checks passed.

## INT-009-05f — Relative DC labels and project access

- Added a single numeric-issue PUT with relative label add/remove operations,
  preserving unrelated labels and all assignments. No automatic assignment or
  undo is exposed: a preflight read cannot protect an assignee against a
  concurrent human edit without a remote compare-and-swap guarantee.
- The project-restricted write gate is checked before preflight and before
  dispatch. Invalid labels, empty removals, reflected credentials, stale stored
  authority, foreign projects, denied writes and unknown outcomes fail without
  blindly repeating a mutation. Project access probes use the reviewed numeric
  project ID and reject divergent or incomplete identities.
- Docker unit and fsstore/TLS integration checks passed:
  `./scripts/go.sh test ./internal/jiradc`
  (`/tmp/weft-jira-labels-v2.log`). Read-only subagent review initially found the
  assignment race; removal of assignment writes and final test coverage were
  reviewed with no residual blockers. No real-account operation or deployment.
- Operational trigger adoption remains a separate batch; this client is not
  itself a grant. REST 2 contract provenance remains the official Jira reference
  linked in batch06a.

## INT-009-05g — Native DC operational trigger lifecycle

- Native sources explicitly snapshot `kind: jira-dc`, connection ID, HTTPS
  `siteUrl` and numeric `projectId`. Site/project must match an enabled,
  credentialed connection with confirmed remote project restriction. Consent
  fences its revision; edits/revocation pause work before new remote calls.
  The native gateway branch cannot use MCP credentials or be invoked solely by
  granting an agent the same tool names: private trigger scope is also required.
- Search uses bounded native REST pages, the selection label and provider `new`
  category key, without matching status display names. Issue data is re-read for
  readiness/claim. Standard inward `is blocked by` links to non-Done categories
  and the `blocked` label prevent admission; unavailable link metadata or blocker
  category fails closed. Custom relationship-type mappings are not provided.
- Claims and completion use relative labels/comments; DC preserves assignees.
  Owner logins control eligibility and accepted clarification replies. Active
  reporter/owner replies participate in bounded readiness rounds. Immutable
  comment keys/IDs reconcile uncertain publication through existing hold state.
- Native site/project-scoped processed keys and global admission journals avoid
  equal-key collisions across instances. Credential/query changes do not reset
  history; DNS case, trailing dot and default HTTPS port spellings share the
  same admission identity. Old GitHub/Cloud fingerprint encodings are unchanged.
- Trusted native issue origin (site/project/key/connection revision and canonical
  link) reaches the real task launch separately from its untrusted request.
  A native source requires actual PR delivery and does not add an agent-side MCP
  read. Shared scope validation prevents a connection-valid context path from
  failing only after claim when the task origin is saved.
- DC cannot prove a relabel actor's project-admin permission using the
  current-user endpoint. Relabel-only readiness requeue becomes an explicit
  operator hold; resolve it through the existing console/API before reselection.
  Automatic failed-admission relabel retries remain unavailable for this edition.
  This limitation is not bypassed with the connection user's permissions.
- Unit and fsstore/TLS integration cover normalization, malicious/incomplete
  identities, grant-without-context denial, consent revocation, foreign projects,
  HTTP denial, readiness questions/replies, manual-requeue safety, history limits,
  site-separated admissions/credential rotation, claim/completion/audit and real
  API-to-task launch. The history-limit operator hold is not rescanned on each
  poll. No real model/provider/issue write or deployment is included.
- Docker checks passed: `./scripts/go.sh test ./internal/jiradc
  ./internal/trigger ./internal/taskconfig ./internal/app ./internal/api
  ./internal/installation` (`/tmp/weft-jira-native-trigger-v6.log`).
  Read-only subagent review identified history-limit translation, blocking links,
  shared scope validation and real launch coverage; all were corrected and the
  final delta review found no residual blockers. `git diff --check` passed.

## INT-009-05h — Native DC console configuration and trigger selection

- Added a credential-free issue-tracker table with combined search/status filters,
  dedicated create/edit routes, contextual help and identity-check loading/error
  feedback. Identity verification explicitly does not certify project-only write
  permissions. Admin edits use If-Match; a conflict preserves the draft and
  requires an explicit reload before resubmission.
- PATs are write-only and cleared after save. Changing site/project clears any
  typed PAT and the project-restriction attestation; replacing a PAT also clears
  that attestation. These corrections came from subagent security review.
- Trigger configuration selects native DC connections independently from SCM,
  snapshots exact site/project scope and requires explicit adoption after scope
  changes. Unqualified/disabled connections and discovery failures block save.
  A pending trigger save disables editing/cancellation so an older response cannot
  discard a newer draft. GitHub and Jira Cloud fields remain edition-specific.
- Verification: Docker `make test-web` passed TypeScript and 104 files / 564 tests
  (`/tmp/weft-native-issue-console-v5.log`). One earlier full run encountered the
  previously recorded intermittent SkillsView history test; the next two full
  runs passed without changing or skipping it. The isolated Chromium journey
  passed against the real disposable API/storage backend, including responsive
  layout, help, native connection creation/editing and secret-free responses
  (`/tmp/weft-native-console-e2e-v3.log`); only the remote identity-check response
  is injected, and no vendor is contacted. Docker `./scripts/go.sh test
  ./internal/api` passed (`/tmp/weft-native-console-api-v1.log`).
- Final read-only subagent review found no residual blocker. No live connection,
  credential, trigger consent or grant was changed; no deployment was performed.
  Optional mappings, transition recovery, portable review adoption and live
  qualification remain separate incomplete work.

## INT-009-06b — Read-only transition API and console inspector

- Exposed bounded, strictly decoded issue-key discovery to operators/admins with
  a reviewed If-Match connection revision. No write client, mapping, grant or
  transition request is created. A connection changed/disabled mid-request
  returns conflict without presenting obsolete results or errors. HTTP failures,
  foreign projects and reflected credentials return no partial metadata.
- Added a dedicated inspector route with contextual help, loading/error states,
  separate transition/destination IDs, required-field details and 20-row pages.
  Null field metadata is explicitly unavailable, distinct from a known-empty map.
  Changing the issue, repeating discovery or reloading clears previous results.
- Docker unit/integration checks passed: `./scripts/go.sh test ./internal/jiradc
  ./internal/api` (`/tmp/weft-jira-discovery-api-v2.log`); `make test-web`
  (TypeScript plus 105 files / 569 tests,
  `/tmp/weft-jira-discovery-console-v1.log`). Chromium also passed the native
  connection/inspector journey (`/tmp/weft-jira-discovery-e2e-v1.log`); its remote
  discovery response is injected, with real verified-TLS adapter/API fixtures
  independently covering Jira reads, denial, foreign scope and mid-read disable.
- Read-only subagent review found no blockers. Mappings, transition mutations,
  durable sync recovery and Jira Cloud adoption are not implemented by this
  read-only batch. No real account or installation configuration was changed.

## INT-009-06c — Internal durable DC transition synchronization core

- Added an internal reviewed-ID transition intent and separate CAS journal. An
  operation ID cannot be rebound to a new connection, revision, issue, type or
  transition. Journal reservation with an atomic connection-revision guard
  precedes POST; a second worker cannot dispatch another fresh reservation.
- Preparation discovers the actual issue transition and destination, validates
  source status/type and valid update metadata, and rereads after reservation.
  Already-at-target is a recorded no-op. Same-status actions, missing metadata
  and required fields (including fields with reported defaults) are unsupported
  by this initial no-field-update subset and retain a visible preflight error.
- POST contains only the numeric transition ID and addresses the numeric issue.
  Once dispatch is crossed, failures remain uncertain. Replays reconcile by GET
  only; seeing the intended target records `target_observed`, without attributing
  the change to Weft. A different current status never authorizes reposting—even
  if a person moved the issue back after the first request. Completed records
  describe historical evidence, not a guarantee of the issue's current status.
- Verified real TLS + filesystem storage fixtures for success/no-op, malformed
  intent, unavailable/changed transitions, required/missing metadata, invalid
  timestamps, changed issue/type, revocation after reservation, lost response,
  actual store close/reopen, storage failures and competing fresh reservations.
  Docker `./scripts/go.sh test ./internal/jiradc ./internal/domain -count=1`
  passed (`/tmp/weft-jira-transition-sync-v4.log`). Schema/event catalogue updated;
  final read-only subagent review found no residual blockers.
- Contract source: [Jira REST 2 transition discovery and mutation](https://docs.atlassian.com/software/jira/docs/api/REST/9.12.0/#api/2/issue-doTransition).
  REST does not provide a status CAS here: local preflight is not an atomic
  guarantee against a human edit between the final read and POST. Live version
  qualification remains separate.
- This core is not yet wired into tasks or exposed as an API/agent tool. Mapping
  configuration, lifecycle consent snapshots, operator recovery and Jira Cloud
  adoption remain incomplete. No live transition, grant or policy was changed.

## INT-009-06d — Reviewed native workflow mapping storage and API

- Added independent mapping records with connection revision, exact site/project,
  issue type, source status and transition/destination IDs. Actual sample issue
  ID/key/update metadata is rediscovered when saving; display names come from
  that discovery. No transition or issue field update is sent by configuration.
- Admin writes require mapping CAS and atomically guard the reviewed connection
  revision. The initial connection read must also match that revision, avoiding
  mixed audiences during rotation. Disabling exactly the saved definition needs
  no remote access, so an unavailable Jira cannot prevent disabling it. Enabling
  or changing it requires fresh discovery and project-restricted account review.
- The initial mapping profile rejects same-status actions, unavailable metadata
  and required fields rather than inventing values. Definitions do not attach
  themselves to workflows, expand consent or rewrite execution history.
- Unit + real TLS/fsstore/API integration fixtures cover ID validation, custom
  names, stale discovery/CAS, role denial, credential-free reads, disabled and
  changed connections, rotation between reads, revocation during discovery,
  audit/save faults, actual reopen and corrupt rows (including a wrong stored
  name pointing to another valid record). Docker `./scripts/go.sh test
  ./internal/jiradc ./internal/api ./internal/domain -count=1` passed
  (`/tmp/weft-jira-mappings-v3.log`). Final read-only subagent review found no
  residual blockers after the revision and corrupted-key checks were corrected.
- Mapping editor/selection, lifecycle pins/adoption, optional synchronization
  recovery and Cloud adoption remain incomplete. No live configuration changed.


## INT-009-06e — Dedicated workflow mapping console

- Added connection-scoped mapping list, creation and review pages. The editor
  discovers a sample issue, displays transition and destination IDs, rejects
  unsupported field requirements and saves the reviewed connection/mapping
  revisions. Changing the sample clears discovery; conflicts retain the draft
  and require reloading. Existing disabled definitions stay disabled on edit.
- Administrators can disable an exact saved definition without contacting Jira,
  including after connection rotation. Viewers cannot save; a mapping belonging
  to another connection cannot be edited through a foreign connection URL.
  Saving does not attach a workflow or execute a transition.
- Docker TypeScript and Vitest passed (106 files / 576 tests,
  `/tmp/weft-jira-mappings-console-v1.log`). The isolated Chromium suite passed
  both journeys (`/tmp/weft-jira-mappings-e2e-v1.log`): the mapping journey uses
  the real browser/API/fsstore boundary and a verified-TLS fake Jira, including
  discovery, save, reopen, disabling and narrow-screen overflow assertions.
  The fake is compiled only into the disposable e2e backend and rejects all
  vendor mutations. No production TLS setting was changed.
- Final read-only subagent review found no blockers. Lifecycle pins/adoption,
  operator recovery, declarative mapping configuration and Cloud adoption remain
  incomplete; no live connection, consent or workflow was changed.


## INT-009-06f — Bind synchronization dispatch to mapping revisions

- Internal transition intents optionally pin a mapping name and positive revision
  together. Dispatch checks exact connection/site/project/type/source/transition/
  destination IDs, enabled state and revision. Reservation atomically guards the
  mapping revision, and the write check reloads it before dispatch.
- Historical pending/unknown operations can still reconcile by GET after their
  mapping is disabled. This does not authorize a new operation or another POST.
- Docker `./scripts/go.sh test ./internal/jiradc -count=1` passed
  (`/tmp/weft-pinned-mapping-v2.log`), covering malformed pins, substituted
  destination, response loss followed by disable, and disable after journal
  reservation. Final read-only subagent review found no blockers. Trigger
  lifecycle adoption remains pending; no live configuration changed.


## INT-009-06g — Durable runtime admission evidence

- Added optional Execution.runtimeStartedAt, persisted after turn approval,
  sandbox resume and instruction preparation, immediately before runtime.Turn.
  Failure to persist prevents the runtime call and restores operator requests
  and queue inputs through the existing failure path. Legacy execution JSON
  omits the field. StartedAt/execution.started retain their prior meanings.
- This marker proves durable admission at the runtime boundary, not a model
  call or remote effect: a crash between persistence and invocation is possible.
  It prevents task admission, pending human approval and environment preparation
  from being mistaken for the work-started lifecycle milestone.
- Docker full domain/engine suites and vet passed
  (`/tmp/weft-runtime-start-tests.log`, `/tmp/weft-runtime-start-vet.log`).
  Unit and integration fixtures cover legacy encoding, pending/rejected/approved
  gates, context failure, fast completion and storage failure before runtime.
  Independent read-only subagent review found no blockers. No live execution or
  installation setting was changed. Lifecycle consumption is a subsequent lot.


## INT-009-06h — Bounded settings pagination for synchronization history

- Added the storage ListSettingsPage contract with literal prefix filtering,
  exclusive key cursor and a validated 1–200 row limit. SQL applies the bound
  in its query; FS selects keys before cloning values. Existing ListSettings
  callers retain their original contract. No schema migration or data deletion.
- Shared conformance fixtures passed for FS/SQLite via Docker
  `./scripts/go.sh test ./internal/storage/...`; storage vet and diff check also
  passed. Coverage includes 1,005 records, missing/deleted cursors, literal `%`
  and `_`, Unicode/case, empty values, copy isolation, cancellation, closed stores
  and invalid limits. PostgreSQL qualification still requires its existing test
  DSN. Independent read-only subagent review found no blockers.
- The lifecycle controller/API will consume bounded pages in the next lot, so
  growth of completed history does not impose a global stop at 1,000 records.


## INT-009-06i — Optional native lifecycle queue and operator recovery API

- Native trigger input/consent now accepts explicit mapping name/revision pins
  for runtime admission and confirmed PR delivery. Empty fields preserve legacy
  consent encoding. Console/API edit and declarative apply refuse pin replacement
  during an admitted task. Definitions are reviewed when recording fresh consent;
  later definition changes reject that synchronization without stranding PR
  write-back. Resuming an already admitted task still validates workflow, source
  and authorization while leaving optional mapping availability to dispatch.
- Preserved the actual numeric issue/type identity from native lookup through
  current task and trusted task origin. Work evidence comes from runtime admission
  after gates; PR evidence comes from its persisted scoped PR-opened event. URLs
  in summaries or generic tool output do not supply either milestone.
- Before current-task cleanup, CAS records stable task/stage jobs independently.
  A separate bounded-page controller invokes tracker.transition through the
  private trigger gateway. Unique-holder task leases serialize both stages across
  workers and explicit retry. Queued/unknown starts prevent overtaking; late start
  retries after delivery are rejected. Failed/unknown tracker outcomes retain task,
  PR and delivery labels. No task/PR is recreated by synchronization recovery.
- Viewer reads and paginated history, operator read-only reconciliation and admin
  explicit preflight retry use role guards, strict confirmation and positive
  If-Match revisions. Unknown/pending adapter operations never accept retry POST.
  A known pre-dispatch failure can retry only under unchanged reviewed authority;
  its journal is CAS-reserved again before dispatch. Mapping/connection changes
  never silently replace the original intent. Merge-confirmed mapping remains
  unavailable; no task success or PR URL is treated as a merge.
- Docker full suites passed for trigger, API, app, jiradc, taskconfig and taskrun
  (`/tmp/weft-lifecycle-regression-v6.log`). Added unit, real TLS/fsstore/API and
  app→taskrun→engine/storage fixtures for pins/legacy encoding, origin persistence,
  actual milestone evidence, immediate terminal executions, optional denial,
  response loss, journal failure retaining CurrentTask, recovery CAS/roles,
  two controllers (including overlapping Tick with the same instance), ordering,
  retry during/after blocked PR dispatch and declarative pin protection. Storage
  pagination separately exercises more than 1,000 records. Event schema/catalog
  and API/setup/tool guidance updated. Independent review found and prompted fixes
  for cross-worker ordering, retry serialization and a global history ceiling;
  final review of the admitted-task resume refinement is recorded with this lot.
- Lifecycle selection/recovery console, declarative mapping definitions, native
  review routing/adoption, Jira Cloud transition adoption and real-account/version
  qualification remain pending. No installation, credential, live issue or PR was
  changed, and no deployment was performed.
