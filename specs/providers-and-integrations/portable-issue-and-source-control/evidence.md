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


## INT-009-06j — Lifecycle selection and historical recovery console

- Native trigger configuration offers optional explicit work-start/PR-delivery
  mapping revision selectors. Only enabled definitions matching the reviewed
  connection revision/site/project are eligible. Connection or scope changes
  clear selections; admitted tasks keep their pins locked. Loading/catalogue
  failures remain visible and do not silently replace the saved choice.
- Connection mapping details now include bounded synchronization history with
  page-local search/status filters. Recovery exposes the source issue and exact
  historical transition/source/target IDs. GET detail returns {sync, transition?}
  from the retained adapter record; current mapping edits cannot rewrite this
  evidence. Scope divergence conflicts, storage failure propagates, and missing
  pre-dispatch intent remains explicitly absent rather than inferred.
- Viewer inspection, operator read-only reconciliation and admin preflight retry
  retain role/CAS fences. Unknown writes have no retry action. A preflight failure
  before an adapter record exists still supports explicit admin recovery. Stale
  revisions require refresh; dependency failures disable action until reviewed.
- Docker make test-web passed TypeScript plus 108 files/584 tests
  (/tmp/weft-lifecycle-console-v7.log). The first complete run encountered an
  unchanged 100-item cleanup test timeout during concurrent browser execution;
  the rerun passed without changing its timeout or checks. Three Chromium
  journeys passed (/tmp/weft-lifecycle-console-e2e-v3.log), including a real
  browser→API→FS store→verified TLS Jira fixture reconciliation and mobile-width
  overflow check. Fixtures reject all vendor mutations. API unit/integration
  tests cover historical mapping changes, missing intent, scope corruption and
  storage failure; full API suite and vet passed before the final site/project
  guards, then targeted lifecycle tests passed again with those guards.
- Independent read-only review found no remaining blockers. No live connection,
  credential, policy, issue or PR changed; nothing was deployed. Declarative
  mapping definitions, portable review routing/adoption, Jira Cloud transition
  adoption and real-account/version qualification remain pending.


## INT-009-04e — Revision-scoped file and reported-check evidence

- Extended the existing portable read operation with optional files/checks.
  Evidence reads recheck head/base, branches, draft and PR state; no writes,
  credentials, new grants or merge capability are added. GitHub file comparison
  pins immutable IDs and refuses its 300-file boundary; Cloud follows an exact
  topic comparison with unfiltered, bounded pagination. DC file inventory remains
  explicitly unavailable while its existing structured diff remains supported.
- GitHub reads combined statuses plus check runs; Bitbucket editions read build
  reports for the exact head. Missing/malformed collections and excessive pages
  fail. Unknown/skipped/neutral states are unknown; empty reports never prove
  passing tests. Required rules remain unavailable. Independent review identified
  the DC API's newest-100 cap and query changes on Cloud next links; fixes retain
  incomplete DC coverage and reject any unreviewed topic/filter query.
- Docker full SCM suite passed (/tmp/weft-review-evidence-v4.log) and SCM vet
  passed. Unit plus gateway→adapter→HTTP fixtures cover all three editions,
  success/empty/unknown/error, changed revisions, limits, malicious pagination,
  scope denial and credential reflection. Independent final review found no
  remaining blockers. Provider references and limitations are recorded in
  docs/portable-review-evidence.md. No real service was called or deployed.


## INT-009-06k — Declarative workflow mapping definitions

- Added IssueWorkflowMapping to installation parser, dependency order, structural
  dry-run, CAS apply, snapshot/export and reference diagnostics. Numeric sample,
  issue-type/status/transition IDs and connectionRevision remain explicit.
  Dry-run makes no vendor call; effective apply uses the existing read-only
  sample/discovery validation. Unchanged round-trips and exact disabling are
  offline. Export excludes discovered labels and credentials.
- Planned connection saves require their exact resulting revision; it is never
  rewritten into a mapping silently. Trigger consent can select only definitions
  already saved: creating/replacing a definition and adopting it in the same
  bundle is refused before writes, with instructions to review the saved version.
- Docker full installation/jiradc/API/cmd-weft tests, corresponding vet and
  weft/weftd builds passed. Unit and real SQLite/verified-TLS API fixtures cover
  explicit revision forecasting, no-op/export, offline disable, stale plans,
  concurrent connection changes, sample change/denial, partial apply recovery
  and same-bundle consent protection. Independent code review and targeted
  reruns found no blockers. No YAML was applied to the running installation.


## INT-009-07a — Running-version review admission in the engine

- Added an internal DispatchReview boundary requiring the exact graph version,
  run, exclusive event consumer and stable message. A lifecycle lease checks
  both durable control state and the actually loaded runtime. The public Inject
  contract is unchanged. This method never starts/resumes graphs or adds grants.
- Engine-authored review inputs carry a reserved marker and version/run fence.
  Fetch and pre-runtime checks reject stale inputs after queueing or approval
  waits; paused/control/storage failures defer visibly. Public injection/runtime
  output cannot forge the marker and normal child output does not inherit it.
- Docker unit and SQLite/memory broker integration tests passed, followed by
  the full engine suite (39.290 seconds) and vet. Coverage includes channel and
  BeforeTurn gates, paused/stopped destinations, changed queued run/version,
  durable-new/loaded-old versions, approval-time pause, storage/broker failures
  and legacy injection compatibility. Independent review found no blockers.
- This is a dispatch primitive only. Route configuration/controller/adoption
  wiring remains a separate lot; the live pilot was not changed or deployed.

## INT-009-07b — Durable internal review routing and recovery

- Added installation routes pinned to SCM identity/revision and an exact saved
  graph, event ingress and agent. Activation uses a durable event sequence, not
  timestamps. Existing backlog is not replayed. Duplicate routes/events reserve
  one scoped head/base receipt. Bounded cursors persist across restart.
- Dispatch requires existing named repository-scoped read/summary grants and the
  exact loaded running version/run. It never starts a workflow, expands grants
  or changes the pilot. Failed pre-dispatch and uncertain dispatch remain
  separate. Unknown delivery is reconciled only through exact retained internal
  message evidence; absent history remains unknown. Admin retry is limited to
  known pre-dispatch failures with unchanged authority. Recovery uses CAS,
  audit events and a shared lease; cleanup failures propagate.
- Unit and FS/SQLite/broker integration cover three provider editions, activation
  failures, concurrent reservations, changed scope/run/version, bounded pages,
  restart, lost responses, stale revisions, forged/public message evidence,
  storage/audit/lease errors and recovery roles. Targeted API/app tests passed
  (/tmp/weft-review-route-wiring-v7.log), reviewroute tests and vet passed.
- A separate adoption example pins a reusable portable reviewer without merge.
  The real API→SQLite→queue→native runtime→SCM→verified TLS provider fixture test
  publishes one summary for duplicate events using deterministic model responses.
  It validates collaboration, not a live model's review quality. Independent
  review found a weak publication oracle in the initial fixture; the corrected
  positive case requires tool success, published SCM receipt and summary event.
  The missing-comment case requires a failed execution, pending receipt and no
  confirmation event. Both passed (/tmp/weft-portable-example-v8.log). Live adoption
  still requires operator resources and an explicit reviewed activation.

## INT-009-07c — Declarative review route adoption

- Added PullRequestReviewRoute parser/validation, dependencies, local dry-run,
  CAS apply and export. Enabled changes require recorded consent. SCM/graph
  dependencies must already be saved; changing them and adopting the route in
  the same bundle is refused. Native numeric CAS and opaque YAML revisions remain
  distinct. No-op/export and exact disable preserve authority without discovery.
- Unit and real API/SQLite fixtures passed, followed by the full API suite
  (44.843 seconds), installation suite and vet. The first broad API run hit an
  existing monthly-budget lease conflict; its isolated rerun and the complete
  suite passed unchanged. No checks or timeouts were weakened.
- No YAML was applied to the running installation, no existing bridge was
  migrated, and no real external review, issue transition or merge was triggered.

- Checkpoint after these route lots: Docker `./scripts/go.sh test ./...` passed
  (/tmp/weft-portability-go-final-current.log), including full API/engine suites.
  Independent route/recovery/API/YAML review found no remaining blockers after
  the example-oracle correction. Cloud backend work is a separate later lot.

## INT-009-06l — Shared edition-specific workflow backend

- Extracted a narrow WorkflowBackend/WorkflowClient seam around scope, read-only
  discovery, authorization, storage guards and the single transition write.
  Mapping validation, durable reservation, snapshot rechecks and reconciliation
  remain one implementation. Nil backend preserves existing DC keys/contracts;
  explicit backends do not fall back to DC on failure.
- Every backend supplies guards for all persisted authorities, including linked
  credential-connection revisions. Alternate mapping/sync namespaces cannot
  collide with native DC records. Once a transport may dispatch, errors remain
  unknown; recovery does not repeat POST.
- Docker full jiradc tests and vet passed. New unit and TLS/fsstore integration
  tests cover shared preparation, unchanged native keys, alternate namespace,
  second-authority races/lookup failure, read-only clients, reservation before
  send, lost response and reopen/reconcile after mapping disable with one POST.
  Independent subagent review found no blocking regression. No live change.

## INT-009-06m — Explicit Jira Cloud project profiles and transition adapter

- Added credential-free Cloud workflow profiles referencing an existing exact
  MCP connection/revision. Preview/save validate first-party static REST
  compatibility without opening credentials or making remote calls. MCP OAuth,
  custom audiences and incompatible policies remain rejected. Technical-account
  project restriction is an explicit administrator attestation, not a claim that
  local code can configure remote permissions or atomically prevent issue moves.
- Cloud discovery uses a minimal project/type/status/update snapshot, independent
  of ADF descriptions. Scoped IDs and actual transition field metadata feed the
  shared journal; no column-name guesses, arbitrary fields or same-status action.
  Profile and MCP guards bind reservation/write. Once entering TrackerAPI.Call,
  failure is conservatively uncertain, even if transport dispatch is unavailable.
  Cloud namespaces are separate from DC; no PAT or audience fallback.
- Mapping/sync audit events now include effective site/project, distinguishing
  equal local names across editions. The Cloud profile audit includes its site,
  cloud and project scope. No credential or issue body is logged.
- Docker full jiracloud, jiradc and mcpconfig tests and vet passed. Unit plus
  real fsstore/MCP manager/TrackerAPI/verified TLS fixtures cover missing pins,
  incompatible credentials, CAS races across profile/MCP/journal, revocation,
  cross-project issue, required fields, malformed metadata, credential reflection,
  403/429, lost response, storage reopen and read-only reconciliation with one
  POST. Audit-scope assertions passed on a further Cloud rerun. Independent
  subagent review found no blockers; no real service or installed configuration
  was changed.
- Provider contract references checked: [Jira Cloud REST v3 issue transitions](https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issues/),
  [REST API token authentication](https://developer.atlassian.com/cloud/jira/platform/basic-auth-for-rest-apis/)
  and [REST OAuth audience](https://developer.atlassian.com/cloud/jira/platform/oauth-2-3lo-apps/).
  These fixtures do not qualify a real account or OAuth REST integration.

## INT-009-06n — Cloud lifecycle adoption, configuration and recovery API

- Cloud trigger opt-in explicitly pins profileId/profileRevision plus exact
  cloud/site/project, independently of its MCP consent revision. Legacy sources
  without profiles/mappings remain readable and retain their prior behavior.
  Opt-in issue reads require numeric issue/type/project/status metadata. Search
  uses numeric project scope and stable status-category keys with bounded pages;
  it does not assume a column/status display name.
- The private trigger gateway dispatches the shared lifecycle core by edition.
  Durable runtime/PR evidence, per-task ordering, optional failure, preflight-only
  admin retry and read-only unknown reconciliation remain intact. Historical
  detail reads validate the exact edition/profile/scope/intent; Cloud profile
  filtering uses kind=jira separately from legacy MCP connection filtering.
- Added role-guarded profile preview/CAS configuration, read-only discovery and
  separate Cloud mapping endpoints. No credential fields or implicit audience
  migration are accepted. Dependency failures and missing historical evidence
  remain distinct. The existing fault-injection test caught an API detail read
  through the runtime manager's storage instead of the API controller's boundary;
  the handler now uses its configured backend and that test passes unchanged.
- Docker unit and FS/private-gateway/backend integration passed for trigger;
  full suite passed repeatedly, plus the final added Cloud recovery case and vet.
  API unit plus real SQLite/MCP/verified-TLS integration covers strict input,
  roles, explicit scopes, revision errors, discovery refusal, offline disable
  and cross-edition history. API/app/trigger/jiracloud/jiradc/installation full
  suites passed together (/tmp/weft-cloud-backend-regression-v1.log); focused API
  recovery/failure/history tests passed (/tmp/weft-cloud-api-v5.log).
  Independent subagent review found no blockers. No installed connection, issue,
  policy or running workflow was changed.

## INT-009-06o — Preserve Cloud compatibility versus dependency errors

- TrackerAPI.Revision now returns a typed compatibility refusal separately from
  actual manager/storage failures. Cloud profiles translate only incompatible or
  missing MCP configuration into invalid input; a transient second-read storage
  failure propagates instead of becoming an apparent operator configuration error.
  The compatibility message and static first-party audience rules are unchanged.
- Docker full mcpconfig/jiracloud tests passed, including a second-read dependency
  failure assertion preserving errors.Is. Independent subagent reviews found no
  blockers. No connection, credential or authentication configuration changed.

## INT-009-06p — Declarative Cloud profile and mapping adoption

- Added separate JiraCloudProfile and JiraCloudWorkflowMapping kinds for local
  plan/preview, CAS apply and export. The existing IssueWorkflowMapping kind
  retains its DC meaning. Equal mapping names do not cross edition namespaces.
  Opaque YAML baselines, native MCP revisions, profile revisions and mapping
  revisions remain distinct and are never calculated or substituted implicitly.
- Profiles require an already saved compatible MCP connection. Mapping adoption
  requires an already saved profile and reviewed sample metadata. Plans perform
  no remote discovery; effective mapping apply uses the shared read-only sample
  validation. Bundles cannot change MCP/profile/mapping authority and adopt it
  in the same new trigger consent. Unchanged dependencies may be included.
  Export/no-op and exact disabling remain available after provider refusal or
  MCP revocation, without opening a new scope or rewriting retained pins.
- Unit and real API/SQLite/MCP/verified-TLS fixtures cover invalid/missing pins,
  denied and stale discovery, separate applies, namespace isolation, safe
  export, unchanged/offline disable, distinct MCP revision 2 versus profile
  revision 1, and rejection of mixed-adoption applies before any mutation.
  The all-kinds round-trip fixture includes the two Cloud kinds through separate
  saved-definition adoption steps. Its fake provider accepts discovery GETs
  only; no transition is dispatched by these configuration tests.
- Docker full API and installation suites passed (45.170 and 0.002 seconds).
  A final preview edge was then corrected: unchanged MCP dependencies in a
  profiled Cloud trigger bundle retain their actual saved revision instead of
  the legacy planned-connection placeholder. The targeted API/installation
  rerun passed (0.258 and 0.002 seconds), followed by API/installation/MCP/Cloud
  vet and diff checks. Independent review, including that final edge, found no
  remaining blockers. No tests or security checks were weakened.
- No configuration was applied to the running installation. No real account,
  credential, issue, trigger, workflow or remote permissions were changed.

## INT-009-06q — Cloud console adoption and verified-TLS browser journey

- Added dedicated credential-free Cloud profile pages, explicit saved MCP and
  profile revision selection, scoped transition discovery/mapping pages and
  edition-specific synchronization history/recovery. Existing unmapped Cloud
  trigger behavior is preserved. A saved profile does not enable a trigger or
  grant tools. Project-only account permissions require explicit administrator
  attestation; local preview cannot prove or change remote permissions.
- Independent review caught cross-edition editor-state reuse and a catalogue
  failure blocking exact offline disable. Both were fixed with regressions.
  Selecting a profile revision is explicit (`name@revision`); stale pins stay
  visible and changing scope clears dependent mappings. The editor mounts after
  its saved snapshot arrives, avoiding a draft initialization race.
- Component unit and API-boundary integration checks passed. Docker TypeScript
  checks, Vite build and the full web suite passed: 109 files / 595 tests. An
  initial concurrent run hit the existing SkillsView revision/deletion timing
  failure; that unchanged test passed alone (13 tests), then the unchanged full
  suite passed with two workers. No assertion, timeout or test was weakened.
- Added a disposable backend fixture with actual verified TLS, real MCP/Cloud
  scope checks and persistent API storage. It rejects vendor mutations and
  unrelated origins; its GitHub fallback is the existing offline readiness
  fixture. A fixture unit test checks these boundaries. The browser creates and
  reviews a profile, discovers duplicate-named transitions, refuses missing
  field metadata, saves a mapping, verifies the DC namespace is separate and
  reloads an exact disable. Desktop and 390px screenshots were visually checked;
  both forms remain within the viewport. No public network or live credentials
  are used.
- Docker e2e-tagged backend tests and TypeScript e2e checks passed. The targeted
  Chromium run passed all four Cloud/DC journeys, including DC read-only unknown
  reconciliation. An initial fixture-only failure revealed readiness setup
  replacing the Cloud transport; installation now follows that setup, preserving
  both deterministic transports. The original discovery assertion passed after
  the fixture correction. Independent review found no remaining blockers.
- This proves configuration, storage and transport collaboration, not a real
  Jira account, installed Data Center version or model-driven live rehearsal.

## INT-009-07d — Console regression compatibility checkpoint

- The first full Chromium run exposed older navigation tests still expecting
  MCP permissions in the inspector's Overview, ambiguous partial labels matching
  both a help trigger and its control, and a navigation label that ignored the
  pending-approval count. Tests now follow Configuration and use exact scoped
  labels or the anchored approval-count label. All original consent, persistence,
  denial, revocation, runtime, scope and layout assertions remain in place.
  No tests, timeouts, retries or security checks were weakened.
- Restored the useful allowed-hosts help text: one lowercase exact public DNS
  hostname per line, no wildcard/URL/IP/port, and an empty list denies egress.
  The viewer component test and existing help browser oracle verify the text
  without enabling editing or dispatching policy writes.
- Focused Docker Chromium runs passed 17 MCP/help cases and eight
  layout/integrated-journey/task-authorization cases. The seven opt-in prompt
  cases were enabled separately with the deterministic interpretation fixture;
  its eight-case run passed, including the overlapping unavailable-tool case.
  This exercises real local Git worktrees and gateway collaboration with a
  scripted model, not a live model or provider account.
- Docker `./scripts/go.sh test ./...`, `make lint` and `make build` passed after
  the Cloud backend/YAML changes. `make test-web` passed after the help correction:
  109 files / 595 tests, with TypeScript checking. E2E-tagged backend tests and
  e2e TypeScript checks passed. Python Docker checks passed 16 SDK tests, 14 agent
  tests and all ten script tests; the Compose configuration checks used a Docker
  CLI image and synthetic configuration rather than the installed `.env`.
- Independent subagent review covered each implementation lot and these final
  regression corrections. The setup guide and rehearsal checklist distinguish
  local evidence from pending actual account/version/combination qualification.
- Final standard Chromium run passed: 163 cases, with the seven existing
  deterministic-prompt opt-ins exercised in the separate successful eight-case
  run above. Commands used `pnpm exec playwright test --project chromium` and
  `WEFT_E2E_PROMPT_MODEL=1` for the two prompt test files. A prior overlapping
  browser run was interrupted after a second build replaced shared `web/dist`
  assets mid-test; the final run used one browser build at a time and passed
  without retries or changed oracles. Logs: `/tmp/weft-portability-browser-final-serial.log`,
  `/tmp/weft-portability-browser-prompts-final.log` and
  `/tmp/weft-portability-web-final.log`. No real credentials, installed
  configuration, remote account or running pilot was changed or deployed.


## INT-009-09 — Explicit Bitbucket Cloud API-token setup

- 2026-11-20: the maintainer requested removing the manual OAuth-token setup
  prerequisite, creating rehearsal issues through the connected Atlassian plugin,
  and updating the local stack. AC-009 and its matrix were recorded before the
  implementation review. Independent specification review found no blocking
  ambiguity; authority metadata now records these narrow live-action exceptions.
- Primary Atlassian contracts reviewed: Bitbucket REST accepts Basic auth with
  account email/API token; Git accepts the static
  `x-bitbucket-api-token-auth` username. The caller-scoped repository permission
  endpoint is documented under the repository API group and requires
  `read:repository:bitbucket`. The guide lists explicit read/write repository and
  PR scopes; no administration/deletion scope is required for these operations.
- At 16:02:35Z and 16:02:36Z, the connected Atlassian plugin created SCRUM-5
  (bounded Go greeting delivery) and SCRUM-6 (intentionally unresolved multiline
  behavior/readiness hold) in the maintainer's personal Cloud project SCRUM,
  `https://woliveiras.atlassian.net`. Both target the test repository
  `woliveiras-home-made-software/weft-rehearsal`. They have rehearsal/scenario
  labels only, without the trigger selection label. Creation proves plugin
  access, not authentication by the local Weft installation or task execution.
- Independent frontend test review found no blocking findings. Focused Docker
  checks passed 14 tests plus TypeScript. Root `make test-web` passed all 110
  files / 601 tests plus TypeScript. Browser/API and backend verification,
  final code review and authorized local deployment are recorded below when
  completed; no real credential or external delivery result is claimed here.

- Backend unit/integration coverage includes encrypted manager persistence/reload,
  legacy Bearer compatibility, account/auth/destination replacement fences,
  bounded token input, administrator writes/operator checks, and declarative
  apply/export/no-op/preserve/replacement. Actual local HTTP and Git smart-HTTP
  fixtures exercise push, idempotent PR lookup/create, immutable reads and durable
  review summaries. Denied, rate-limited, failed, redirected and cancelled requests
  are covered without sending credentials to real providers.
- Independent implementation review found a P1: reusable Basic payloads encoded
  in provider URLs or alternate JSON string escapes could survive the initial
  literal-byte redaction. Decoding JSON strings before typed responses and matching
  percent-decoded bytes closes those observed cases. Regressions cover mixed-case
  escapes, encoded unreserved characters, adjacent invalid escapes, actual Git
  pre-receive rejection output, delivery/review outputs, persisted receipts and
  events. Independent focused SCM/API reruns passed after the fix; final review
  found no remaining blocking findings. No fail-first/TDD claim is made; initial
  targeted failures corrected two mistakes in new fixtures.
- Docker Node 22 Chromium passed all six connection/SCM journeys. The prior edition
  regression now explicitly selects Bearer; its original persistence and scope
  assertions remain intact. The new journey exercises the API-token default,
  saved API state without returned secrets, OAuth mode clearing, and desktop/mobile
  overflow checks. Empty-form screenshots were visually inspected at 1280x720
  and 390x844. Browser tests contact a disposable local backend, not Atlassian.
- `make lint` and `make build` passed after the encoded-credential fix. All Python
  checks passed via `make test-python` (16 SDK, 14 agents, 10 scripts). Browser runner
  setup first exposed missing Corepack in its older Node 26 base; a temporary
  test-only image added the cached Node 22 toolchain and enabled only pnpm. The
  application deployment still uses the repository's default Dockerfile.
- Final `./scripts/go.sh test ./...` passed after the security correction.
  Logs: `/tmp/weft-cloud-api-token-go-final.log`,
  `/tmp/weft-cloud-api-token-web.log`,
  `/tmp/weft-cloud-api-token-browser-final.log`,
  `/tmp/weft-cloud-api-token-lint-final.log`,
  `/tmp/weft-cloud-api-token-build.log` and
  `/tmp/weft-cloud-api-token-python.log`. A previous browser run passed the new
  API-token scenario but failed the old edition test's implicit Token selector;
  its explicit Bearer selection fixed that stale expectation without relaxing
  its assertions. The final six-case run passed without retries.
- Authorized local update completed at 16:19Z using exactly
  `docker compose up -d --build --no-deps weftd` and the standard Dockerfile,
  without overrides or binary copies. Compose reports `weftd running healthy`;
  `/healthz` returns `status: ok`. The administrative SCM kind catalogue exposes
  all three adapters and API-token guidance. The served console asset
  `/assets/index-DeXfKd0J.js` contains the new API-token form and account-email
  field. Volumes, saved connections and secrets were not edited or removed.
- Operator actions still pending: initialize the test repository's main branch,
  enter/test each reviewed credential in Weft, qualify Jira MCP and typed REST
  access, adopt/review workflows and trigger scope, then explicitly start the
  selected rehearsal. No trigger, remote branch/PR/review/merge or automatic
  tracker transition was executed by this setup change.


### Batch10 — independent Jira Cloud REST credential (2026-11-20)

- A live personal Cloud MCP V2 token with the three Jira agent-interface scopes
  successfully discovered 23 tools, while the same token received REST HTTP401
  scope rejection. The maintainer explicitly authorized the blocking correction,
  tests, independent review and local update. Specification/test reviews preceded
  implementation; fail-first API/core logs reproduced missing input/state and
  wrong-audience behavior. A separate maximum-token API test first returned 400.
- AC010 uses a separate encrypted `jiraRest` slot and Cloud/purpose/connection AAD.
  Unit and real TLS/storage tests cover separate authentication headers, MCP OAuth
  coexistence, restart/discovery preservation, credential clearing without fallback,
  scope replacement and revision fences, denied/malformed destinations, redirects,
  transport/HTTP errors and credential reflections. API/profile integration checks
  REST reads and stale profile refusal after REST-only clearing. YAML round trips
  use independent secret references and preserve-only exports, with no public
  email/token/ciphertext. The 64 KiB JSON envelope bound is independent of per-field
  byte limits. No remote secret is used in fixtures.
- Frontend unit/component tests and a Chromium journey against a disposable real
  Go API cover creation, blank edit, independent clear, incompatibility, no echoed
  secrets, keyboard help and 390 px overflow. Docker web verification passed 112 files /
  631 tests, TypeScript and production build; final focused 42 tests also passed.
- Root `make test-go lint build` completed with exit 0. Focused API tests passed
  in `/tmp/weft-jira-rest-api-green.log`; full Go/vet/build evidence is in
  `/tmp/weft-jira-rest-full-go.log`. Independent implementation review found no
  blocking findings in core/API/YAML/UI/docs. Additional trigger TLS integration
  exposed a pre-existing Jira MCP-envelope decoding defect; that correction and
  its separate evidence are pending, rather than treating the search as qualified.
- The authorized local standard-Dockerfile update used
  `docker compose up -d --build --no-deps weftd`, exit 0, with healthy container.
  At 17:03:16Z the administrator saved independent REST credentials in personal
  connection `rehearsal-jira`; discovery preserved MCP authentication and produced
  connection/catalogue revision 4, Ready, 23 tools. Values remain encrypted and are
  absent from this evidence. No grants or trigger were activated.
- At 17:03:38Z, credential-free profile `rehearsal-scrum` revision 1 pinned connection 4,
  Cloud ID `eca45cd2-875d-4ff3-a84e-cb02398b8c88`, project 10000. Read-only transition
  discovery of SCRUM-5 succeeded through local Weft to real Jira Cloud: issue 10004,
  type 10003, current status 10000 (To Do), transition IDs 11/21/31/41 to statuses
  10000/3/10001/10002. This qualifies the scoped REST read boundary only. No issue
  mutation occurred. `projectRestricted` remains false; a personal administrator
  account is not asserted to be a project-restricted technical account.


### Batch10b — Jira MCP search envelope regression (2026-11-20)

- The added real TLS trigger integration initially observed MCP search 1, REST
  reads 0/writes 0 and tasks 0 without an error. The Jira decoder read the MCP
  envelope as a direct issue page, silently treating valid results as no work.
  This is a bug restoring existing polling behavior, without a new acceptance
  criterion or relaxed scope. The maintainer's authorization covers blocking
  Jira/Bitbucket corrections.
- Independent TEST review preceded the decoder correction. Unit cases cover
  legacy direct pages, structured content, one textual block, legitimate empty
  results, invalid/null/error results, dependency failures and scoped pagination.
  Every page now uses the same decoder and malformed results fail explicitly.
- `TestJiraCloudTriggerSeparatesMCPAndRESTCredentialsOverTLS` exercises the real
  Runner, Gateway, manager, encrypted filesystem store and separate MCP/REST TLS
  servers. It asserts MCP discovery/search receives only MCP authentication,
  Jira issue reads/claim/completion labels/comments receive only REST Basic,
  one task is admitted, and REST rotation invalidates old trigger consent.
- `./scripts/go.sh test ./internal/trigger -count=1` passed in 6.956 s; log
  `/tmp/weft-jira-trigger-green.log`. Independent implementation review found no
  blocking findings. These are synthetic local fixtures; no real issue trigger
  or model execution is claimed by this regression test.
