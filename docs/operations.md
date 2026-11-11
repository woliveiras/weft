# Installation operations

This runbook describes the shipped single-instance Docker Compose installation.
Infrastructure maintenance requires an installation administrator. Console work
(models, MCP connections, grants, graphs, approvals and executions) uses the
existing role checks. This guide does not introduce an onboarding wizard, user
registration or automatic secret rotation for every credential type.

## Compose review

The shipped long-running services use `unless-stopped`; the one-shot sandbox
image builder uses `no`. Store, broker, workspaces, controller key and dedicated
Docker state use named volumes. Controller and example-runtime health checks
come from their images; database, broker and daemon have Compose health checks.
Controller startup waits for database, broker and runtime health. Sandbox
readiness remains a separately reported dependency, not a global startup gate.
Secrets are required environment substitutions, not literal defaults; they are
still visible to a Docker administrator and are not a secret-manager boundary.
The agents service receives only its runtime authentication secret, not provider
or MCP credentials. Privileged Docker-in-Docker remains the explicit isolation
tradeoff; rootless deployment is deferred. See the [port contract](deployment.md#network-and-ports)
for default loopback publications and remote deployment implications.

Configuration tests render the real Compose file with synthetic credentials and
without loading `.env`. They verify volumes, restarts, network membership,
publications, remote overrides and missing-credential rejection. They do not
claim service reachability or a successful clean installation.

## Initial identity and credentials

A new Compose installation requires four independent bootstrap secrets: the
Weft administrator token, runtime shared secret, database password and Grafana
administrator password. The [getting-started procedure](getting-started.md)
creates them only when `.env` does not already exist, with owner-only permissions.
Missing values stop Compose configuration. Do not substitute a checked-in default
or reuse one secret for several services. Infrastructure bootstrap is the only
shell step before normal console configuration.

The initial Weft identity is the configured administrator bearer token, not a
user account created by a web signup form. Sign in locally, verify the admin role,
then configure models, MCP connections and graph grants in the console. Connecting
GitHub/Atlassian OAuth authorizes an MCP connection; it does not create a Weft
login identity. Grafana authentication is a separate service.

For a named first operator, configure a distinct `auth.tokens` entry with role
`operator` and a mounted `tokenFile`, or map the operator through the existing OIDC
issuer/audience/role-claim configuration. An administrator must provision that
identity through installation configuration and restart as required; there is no
browser account-management screen or automatic elevation of the first visitor.
Viewer/operator/admin permissions remain unchanged. Never hand out the bootstrap
admin token as a shared operator credential. Keep a separately protected recovery
admin identity and verify it before retiring old authentication material.

Remote access additionally needs reviewed TLS ingress, the intended public URL
and host/network exposure. OAuth callback URLs derive from the public URL and must
match provider registration. Do not disable authentication to fix a callback,
reverse-proxy or connectivity problem. Changes to real authentication settings,
identity-provider applications or secret values are separate maintenance actions.

## Before maintenance

Record the deployed source revision, image digest, Compose project name, enabled
profiles, database schema versions and named-volume identifiers. Keep an offline
copy of the exact configuration and image. Store secret material in the approved
secret store, never in Git, tickets, command transcripts or screenshots. Do not
print a resolved Compose configuration: it contains interpolated credentials.

Use the console to stop admission of new work and let active turns finish. Confirm
that approvals, builds, setup and delegated children are no longer active before
taking a consistent backup. Pausing graph admission alone does not stop all
in-flight work. If maintenance interrupts work, record that fact: replaying
messages may repeat external side effects. Provider idempotency is not universal.

## Backup inventory

Back up these resources as one dated recovery set. A database dump alone is not
a complete Weft backup.

| Resource | Why it matters | Recovery rule |
|---|---|---|
| PostgreSQL (`pg-data`) or the configured store | Graph versions, grants, MCP configuration, encrypted tokens, approvals, history, leases and tree budgets | Use a database-native consistent backup. Never copy live PostgreSQL data files as an ordinary file archive. |
| `weftd-data` | Generated installation encryption key; local state when configured | Protect separately from the database backup. An external key must be retained in the secret store instead. |
| `nats-data` | JetStream messages and consumer acknowledgement state | Use a broker-supported backup or a consistent stopped-volume snapshot. Do not infer exactly-once delivery from a restore. |
| `workspaces` | Repositories, uncommitted edits and isolated worktrees | Preserve ownership, permissions and hidden Git metadata. A Git remote does not preserve uncommitted work. |
| `workspace-journal` | Process evidence and replay history | Back up the active file, retained archives and retention anchor with the workspace snapshot and the store containing the import cursor. Lost kernel events cannot be reconstructed. |
| `sandbox-daemon-data` | Suspended containers, images and shared caches | Preserve if container-local files must survive. Treat Docker state as version-sensitive, not portable workspace backup. |
| `sandbox-certs-ca`, `sandbox-certs-server`, `sandbox-certs-client` | Mutual TLS identity for the dedicated daemon | Keep as a matching protected set; do not restore only one side. |
| Configuration and external secrets | Database/runtime authentication, provider keys, OAuth clients, public callback URL | Retain out of band with access controls and the required historical keys. |
| Optional model volumes | Downloaded Ollama/Hugging Face weights | Back up or document the exact reproducible download and available disk space. |

Prometheus, Loki and Tempo data have their own retention and backup policy.
Grafana dashboards/provisioning are repository-mounted; the shipped Compose file
does not persist arbitrary UI-created Grafana state in a named volume.

## Restore drill

1. Restore into an isolated target, with distinct ports, networks and volumes.
   Keep outbound provider/MCP calls blocked and graph admission disabled. Never
   test a restore over the live installation's volumes.
2. Restore the matching store, broker, workspaces and required daemon state using
   their supported procedures. Restore encryption/TLS material through the secret
   store without disclosing values. Start with the recorded image and schema.
3. Check service health and **Administration → Health**. `/healthz` is liveness;
   `/readyz` checks store and queue. A sandbox failure is reported but does not
   make the entire control plane unready. Public readiness responses use bounded
   `ok`, `unavailable` or `not configured` states, never raw dependency errors
   containing endpoints or credentials. Provider and MCP availability must be
   checked independently; a healthy HTTP process does not prove valid credentials.
4. Compare graph versions, explicit grants, connection names and authorization
   states. Check a representative file and diff, execution/approval history and
   token totals. An old OAuth access token may already be revoked or expired.
5. Reconcile interrupted work before resuming graphs. Do not replay an entire
   message history blindly: confirm which external effects already occurred.
6. Record recovery duration, missing evidence and validation results. A written
   procedure is not a successful restore drill; clean-install/recovery acceptance
   remains a separate check.

## Upgrade and rollback

Build the new image before the maintenance window and retain the previous digest.
Back up first, stop the old controller, then recreate only the intended service
with the same named volumes. Never use `down -v`, global prune, or workspace reset
as an upgrade step. Migrations run at startup; inspect their compatibility before
assuming an older binary can read the new database.

For the tree-budget recovery upgrade, stop every old worker before starting the
new version: old binaries do not honor closed-budget tombstones. The new engine
reconciles terminal roots idempotently and retains unknown reservations until
terminal execution evidence exists. Do not run mixed old/new budget writers.
Pending OAuth browser transactions currently live in memory and must restart
after controller restart; completed credentials remain persisted.

After restart, compare mounted volume identities, graph versions and dependency
status. Validate a read-only console journey before resuming work. If rollback is
needed, restore the prior image only when its schema is compatible; otherwise use
a matching backup in an isolated recovery environment. Do not overwrite the new
state without an explicit recovery decision.

## Workspace journal

The collector rotates before a record would take the active segment past
64 MiB. It preserves the old inode as
`events.jsonl.segment-<sha256-of-segment-id>`, then atomically publishes
the successor at `events.jsonl`. Its first record contains
`previousSegment.id` and `previousSegment.bytes`. Archives and the active
segment must stay in the same directory on a filesystem supporting hard links
and directory sync. An interrupted rotation can leave a staged
`.events.jsonl.next-*` file and an archive name linked to the still-active
inode. On reopen the collector validates the published chain, removes
orphaned staging files and reuses the archive link. If the published chain
cannot be validated, it preserves the staging files for recovery.

### Prune imported archives

During approved maintenance, keep the four newest archived segments by
default, plus the active segment and any archives still needed by the
importer. With a caught-up importer this keeps up to 256 MiB of archives
after each prune. The journal can grow beyond that between maintenance
windows or when the importer falls behind. Use `--journal-keep-archives N`
to set the minimum number of newest archives retained. Preserve exported
backups according to the installation's audit policy.

1. Quiesce sandbox work and stop `sandbox-daemon` so the maintenance command
   can acquire the writer lock. Let the importer catch up. Back up the journal
   directory and the store containing the cursor as a matching recovery set.
   A filesystem-backed store also requires stopping its controller before
   opening it from the maintenance command.
2. Run `weftd --prune-workspace-journal` with the installation's config and
   original `WEFT_WORKSPACE_JOURNAL` path. It prints the eligible filenames
   and byte count. Eligibility requires a persisted cursor in a later
   segment. A cursor at the exact end of an archive still needs that archive
   until it commits the successor link.
3. After reviewing and authorizing the listed removal, repeat with
   `--apply-journal-prune`. The command reads the cursor from the configured
   store and revalidates the chain under the writer lock. It syncs
   `events.jsonl.anchor.json` before unlinking each eligible segment. The
   anchor identifies the oldest retained segment, its immediate predecessor
   and the cursor checkpoint. Retrying an interrupted prune finishes a
   committed unlink before advancing the anchor again.
4. Restart the collector and check daemon health and import progress.
   Restore the anchor, retained segments and cursor together. A missing or
   mismatched anchor, missing retained segment or cursor behind the checkpoint
   stops import. The importer caches validated archive links and checks file
   identities and lengths on subsequent passes.

For Compose, use a maintenance override file for the one-off command. The
normal weftd service mounts the journal read-only.

```yaml
services:
  weftd:
    user: "0:10001"
    volumes:
      - workspace-journal:/var/lib/weft/journal:rw
```

```sh
docker compose stop sandbox-daemon
docker compose -f docker-compose.yml -f journal-maintenance.yaml \
  run --rm --no-deps --pull never weftd \
  --config /etc/weft/weft.yaml --prune-workspace-journal
```

Use the same override and flags for the approved apply, adding
`--apply-journal-prune`. Start the collector with the installation's normal
Compose file.

### Journal alerts

The weftd `/metrics` handler reads filesystem and collector status directly
from the read-only journal mount. These series remain available when import
is stopped or blocked. The shipped Prometheus job `weft` also alerts when
the scrape or filesystem measurements are unavailable.

| Alert | Condition |
|---|---|
| `WeftWorkspaceJournalVolumeLow` | Available filesystem space stays below two segment limits or 10% of capacity for two minutes. |
| `WeftWorkspaceJournalArchiveGrowth` | Archive bytes exceed their one-hour low by more than two segments, with less than 24 times that growth available on disk, for ten minutes. |
| `WeftWorkspaceJournalWriteFailed` | The collector recorded a write or rotation failure in the last five minutes. |
| `WeftWorkspaceJournalCollectorUnavailable` | The collector heartbeat is missing or more than 45 seconds old for 30 seconds. |
| `WeftWorkspaceJournalMonitoringUnavailable` | Prometheus cannot scrape the weftd job, filesystem metrics fail or collector metrics are missing on a configured controller for one minute. |

| Series suffix | Meaning |
|---|---|
| `active_bytes`, `segment_limit_bytes` | Active size at scrape time and the per-segment limit. |
| `archived_bytes`, `volume_available_bytes`, `volume_size_bytes` | Archive bytes on disk, and the available and total bytes of the journal filesystem. |
| `collector_heartbeat_timestamp_seconds`, `collector_failure_timestamp_seconds` | Capture-loop heartbeat and last failure timestamp by `operation` (`write` or `rotate`). A stale heartbeat also covers failures that prevent writing status. |
| `rotations_total` | Segment links committed by this importer process. A backlog can delay this counter after the collector logs a rotation. |
| `gap_windows_total`, `gaps_coalesced_total` | Imported unresolved-path windows and additional kernel gaps represented by those windows, summed as `count - 1`. |
| `records_imported_total` | Committed observation records. Each record is counted once, independent of the number of workspace scopes. |
| `imports_total{outcome="error"}`, `import_duration_seconds` | Failed import passes and the pass latency histogram. |

1. Check `docker compose ps weftd sandbox-daemon`. Inspect the journal-related
   lines in the two services' logs for `workspace journal rotated`,
   `workspace journal import failed` or collector shutdown. Check free disk
   space and inodes for the journal volume.
2. After a normal rotation, confirm that the active size dropped, the archived
   byte count increased, and the importer crossed the link. When available
   space is low, review archive growth, other users of the filesystem and
   import errors. Expand the filesystem or run the approved prune procedure.
3. On a missing link, wrong identity, length mismatch or incomplete archived
   record, preserve the journal directory and store. Restore the matching
   segment at its expected name from a verified backup and confirm its ID and
   exact length against the successor. The importer resumes from the saved
   cursor. Keep partial active records for diagnosis if the collector cannot
   reopen the file. A recovery decision must account for any unavailable
   evidence before starting a new chain.

### Upgrade an exhausted legacy journal

Record the legacy file size, first-record ID and saved cursor under
`workspace-journal/cursor/<sha256-of-configured-path>`. Upgrade the collector and
importer together. Preserve `WEFT_WORKSPACE_JOURNAL` and the original journal
file at the configured path until the upgraded collector starts. Its first
append rotates an exhausted, complete journal. The existing cursor still
addresses the archived segment. If its offset already equals the archive
length, the importer advances directly through the link.

Confirm the archive has the original ID and length, then verify that the cursor
points to the new segment and both services stay healthy. An older importer
cannot follow segment links. Rollback after rotation requires a matching
pre-upgrade journal and store recovery set.

## Encryption-key rotation

The primary key seals new credentials; previous keys can decrypt old ciphertext.
Rotation changes secret material and requires a separate authorized maintenance
operation. Preserve a recovery set with the old keys before changing anything.

Configure the new primary and retain the old primary as a previous key on every
controller, then restart consistently. Model-provider configuration supports
re-sealing on load. MCP access/refresh tokens and OAuth client secrets do **not**
have a complete bulk re-sealing migration: keeping previous keys is necessary
until every affected credential has been replaced/re-authorized through an
approved flow and verified. A successful model-provider rotation log is not proof
that MCP credentials were migrated. Do not remove a previous key on that basis.
Older backups also require their original keys. Lost keys cannot be reconstructed
from ciphertext; restore the key or re-enter/re-authorize affected credentials.

## Failure triage

- **Store/queue unavailable:** preserve volumes, inspect dependency health and
  restore connectivity. Do not reset data to make readiness green.
- **Unknown encryption key:** verify the matching key set; avoid repeated OAuth
  registrations or overwriting an existing connection while investigating.
- **DCR outcome uncertain:** reconcile with the provider before retrying creation;
  an HTTP/network failure does not prove no client was created.
- **Sandbox capacity wait:** inspect CPU/memory/build/setup reservations, active
  containers and shared-cache references. Use reviewed unused-resource cleanup,
  not a daemon-wide prune.
- **Interrupted delegation:** inspect the execution tree, approval and budget
  snapshots. Terminal reconciliation must not charge a settled reservation twice.
- **Missing workspace attribution:** preserve journal evidence and report the
  gap; never infer an author solely from timing or file ownership.
