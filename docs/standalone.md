# Standalone Weft

Status: native bundle and task flow qualified with automated macOS arm64
checks on 2026-10-19; external/platform acceptance limits are recorded below. The existing Docker installation
remains independent; no migration or activation is implied by this document.

## Dependency and capability contract

| Component | Native installation | Docker/server installation |
|---|---|---|
| Control plane and UI | `weftd` plus bundled web assets; browser UI | Existing image and mounted/configured assets |
| Persistent state | Existing SQLite adapter (pure-Go modernc driver) | Existing SQLite/filesystem/PostgreSQL choices |
| Queue | Existing durable filesystem broker | Existing filesystem/NATS/Redis/RabbitMQ/Kafka choices |
| Git/workspaces | Installed Git, explicitly registered repository roots and task worktrees | Paths visible to the server/container; a mount is not host execution |
| Native agents | Existing in-process runtime, configured model provider | Same gateway and permissions |
| Models | Explicit cloud provider or separately available local inference service | Existing provider configuration |
| External runtimes | Optional separately configured runtime endpoint; Python/agent frameworks are not silently installed | Existing optional agent services |
| MCP | Explicit HTTP connections and reviewed native stdio processes | Existing remote HTTP connections |
| Sandboxes | Optional compatible Docker runtime; never required for process execution | Existing dedicated sandbox daemon and policy controls |
| Observability | Structured logs, API history and protected metrics; optional exporters | Existing optional collectors/dashboards |

A basic installation must not install PostgreSQL, NATS, Docker or a companion
executor. It runs as the invoking user. Process execution has that OS account's
authority; worktrees are file organization, not security isolation. Remote MCP
credentials and local OS credentials must never be imported implicitly.

## Supported platforms

The native CLI and bundle support Linux and macOS. Cloud deployments support
Linux only. Windows is unsupported. macOS arm64 is the initial native runtime
qualification target. macOS amd64 and Linux amd64/arm64 require their own
runtime smoke evidence. A cross-build verifies compilation only.

Source builds require the Go version declared by `server/go.mod`, Node/pnpm for
the web assets, and Git for repository operations/tests. End users of a packaged
bundle should need neither the Go toolchain nor Node merely to launch Weft.
Git and a usable model provider are necessary for real repository work. An echo
or mock run proves plumbing only and must be labeled synthetic.

## Initial native bundle

Build on the target machine with `make build-native`. The command prints a new
bundle directory under `dist/native/`; it never starts an installation. The bundle
contains `weftd`, the existing `weft` CLI and `web/` assets. Keep them together.
From that bundle, initialize a **new** profile and run it:

```sh
./weftd --init-local
./weftd --local
```

The default private data directory is the OS user configuration directory plus
`Weft` (on macOS, `~/Library/Application Support/Weft`). Override it with
`--data-dir /absolute/new/path` on both commands for a separate test profile.
Initialization refuses an existing directory, including a partial initialization;
it never overwrites credentials or data. A missing bundled UI is an error.

Open `http://localhost:8787`. Read `admin.token` in your own profile directory and
enter it in the login screen yourself. Initialization never prints its value.
The encryption key is generated inside this profile on first start. Do not share
these files. The native profile ignores `WEFT_*` configuration overrides so a
Docker installation environment is not silently imported. Existing HTTP MCP and
model credentials are entered explicitly through the UI.

`--port 8789` is an initialization option for a different loopback port. Start
uses the saved profile. All listeners are reserved before the engine starts;
occupied ports fail without starting agents. A file lock prevents two native
processes from opening the same data directory. Stop the foreground process with
Ctrl-C and start it again to retain state. SQLite and filesystem queues need no
separate services. Preview serving is initially disabled; optional preview/sandbox
qualification remains pending. No Docker services or existing volumes are changed.

For a first synthetic test, open Tasks → Preview example, confirm preparation,
then Review requirements → Start task. This saves an echo-only workflow and draft;
it uses no model, repository, tools or connected account and does not perform AI
analysis. Nothing starts during preparation. If preparation fails, inspect Tasks
and the linked example workflow before retrying; partial records are retained.

## Terminal feedback and stopping

`weftd --local` defaults to readable console logs: startup identifies the data
folder, readiness prints the browser URL, and Ctrl-C reports agent draining,
HTTP stream closure, storage/queue cleanup and elapsed time. Use
`weftd --local --log-format json` for structured collection, or `--log-format text`
for standard key/value logs. This presentation override does not rewrite the profile;
server deployments retain their configured format (JSON by default).

Browser event streams are cancelled after agents drain, so an open browser does
not hold graceful HTTP shutdown until its timeout. HTTP shutdown errors are
reported and remaining connections closed. Wait for the final stopped message
before restarting the same profile. Existing profiles need no reinitialization:
run the updated binary with `--local` and the same optional `--data-dir`.

## Software Factory on first launch

The native launcher seeds Software Factory once, stopped, before serving the UI.
It is embedded in the executable, so no repository checkout or Python agent
service is required. Repeated launches preserve saved edits, versions and an
intentional deletion. A failed seed/marker write fails startup explicitly; retry
preserves any graph already created rather than overwriting it. This also makes
the factory available when opening an older native profile that has no seed marker,
without replacing an existing workflow with the same ID.

This native variant has planner, researcher, developer, reviewer, QA and review
handoff agents. It uses native model calls and typed message channels. Configure a
real model before actual work; the initial mock provider is not a working software
engineering model. It does not silently connect accounts or select credentials.
The developer initially writes only `src/**` and `tests/**`; review grants for your
project. QA has read-only tools and explicitly reports checks that still need human
execution. It does not claim tests passed. A human approval gates the final review
handoff. No agent receives Git write or shell permission in this default template;
commits and pushes remain manual. The Docker/Python example is unchanged.

For a task using this workflow, select the registered project and Software Factory.
New task suggests entry `planner`, input channel `requests`, message type
`feature.request` and result agent `integrator` from its unambiguous graph structure.
Title and request populate the initial JSON with `title` and `description`; you can
edit all fields, and customized JSON is preserved.
Review the entire workflow's grants and model settings before starting.

For a first repository test:

1. In Configuration → Models, configure a real available provider, test it and
   choose Make default. The initial
   mock provider and echo runtime are synthetic plumbing tests, not real AI work.
2. Review the preinstalled Software Factory and its explicit tool grants. Its
   agents inherit the default provider/model unless individually overridden. You can also create a different workflow. For manual code review, prefer scoped filesystem
   tools; do not grant Git write or unrestricted shell actions unintentionally.
3. Register your repository in Projects. New task lets you select that project,
   workflow, entry/result agents and a reviewed commit.
4. Review and start. Task progress offers Open in VS Code after you confirm that
   Weft and the browser are on the same computer and VS Code is installed.
   Alternatively, open the displayed worktree path manually.
   Dirty files in the original checkout are not copied or overwritten.
5. Use Inspect changes in task progress for Git status/diff/history, and Tool calls
   for recorded command outcomes. Inspect results and tests before your own commit/push. Task completion
   itself performs neither action; agent permissions remain those you reviewed.

## Offline backup and restore

Stop Weft with Ctrl-C and stop other processes writing its workspaces before
backup. Do not open the same profile through the standard server entry point.
The native lock rejects an active native instance; it cannot stop external writers.
Create a new archive outside the profile:

```sh
./weftd --backup-local /absolute/backups/weft-backup.tar.gz
```

Use the same `--data-dir` as initialization for a nondefault profile. The archive
includes SQLite, filesystem queues, pending approvals, configuration, credentials,
the encryption key and managed workspaces. It is private (0600), **not encrypted**;
store it securely. Registered source repositories and their Git metadata need
separate backups. Workspace symlinks are stored without following their targets.
Special files are rejected. An unsuccessful backup remains `.partial` and is never
published as a complete archive; inspect it before deciding on cleanup.

Restore only a trusted archive, with Weft stopped, to its **original absolute
profile path**, which must not exist:

```sh
./weftd --restore-local /absolute/backups/weft-backup.tar.gz
./weftd --local
```

The command never overwrites a profile. Retain existing data separately before a
recovery attempt; restore also refuses relocation because worktree metadata and
configuration reference absolute paths. Keep the matching bundle/UI and external
source repositories available at their original locations. Invalid archives leave
a partial restore for inspection rather than deleting data automatically. Restore
checks path boundaries, duplicate entries, gzip integrity and the profile format;
it limits archives to 100 GiB and one million entries.

The profile format is versioned and unknown formats are refused. Before replacing
a bundle, preserve the stopped profile and source repositories and keep the old
bundle. There is no automated cross-version migration or downgrade guarantee.
Restart retains pending messages and approvals. Existing engine recovery may
redeliver interrupted work; this does not guarantee exactly-once external effects.

## Final external acceptance

- Real-model work, authenticated vendor MCPs and corporate approval where needed.
- Actual VS Code application launch and the user's review of resulting code.
- Optional real sandbox setup and runtime isolation on the chosen Docker daemon.
- Runtime smoke checks on macOS amd64 and Linux amd64/arm64. Windows is unsupported.
- Backups of repositories outside the native profile remain the user's responsibility.

Task flows, native capability checks, local MCP lifecycle, exact per-agent grants,
reservation reconciliation and format-1 upgrade coverage are implemented. No silent
sandbox fallback, secret import, Docker data migration or service activation occurs.
The Docker-toolchain `make build` is separate from the `make build-native` bundle.
See [task workflows](task-workflows.md) and [server operations](deployment.md).
This closes neither the external acceptance above nor unrelated Release 1 backlog.

## Initial baseline evidence (2026-10-19; extended by the qualification below)

The darwin/arm64 bundle built successfully and its executable was inspected as
Mach-O arm64. Native Go integration tests exercised authentication, durable draft
storage across shutdown/restart, occupied-port failure and single-instance locks.
The Chromium native journey used the production `weftd --local` entry point,
registered a disposable Git repository, reviewed/started an echo task in its own
worktree, reloaded its result and opened file inspection while preserving dirty
source files. This is explicitly synthetic execution, not real model/MCP evidence.

`CI=true make test` passed (303 frontend tests plus Go/Python suites), as did
`make lint`, `make build`, `make build-native`, `make test-e2e` (85 main cases,
1 hosted CIMD and 1 isolated provider case) and `make test-native-browser` (1 case).
The existing installation was not rebuilt, restarted, migrated or configured.
These initial counts describe the baseline delivery; current task-flow, stdio MCP
and operational coverage is recorded in the subsequent sections.

Offline recovery evidence: native unit tests cover active-instance refusal,
non-overwrite, cancellation, path traversal, destination symlink aliases, corrupt
archive checksums and synthetic credential preservation. The production native
HTTP integration restores a stopped profile and verifies its draft, pending queue
message and approval, then processes the retained messages after restart. These
checks use disposable fixtures only, not the existing installation.

Latest native task evidence: offline backup/recovery, explicit VS Code handoff,
synthetic first-use preparation and task-local Git inspection are implemented.
The final changed-code suite passed 312 frontend tests plus Go/Python, lint/build,
nine relevant browser cases and two native production-binary journeys. Earlier
full browser regression in this delivery passed 86 main cases plus hosted CIMD
and isolated provider cases. Actual VS Code launch, a real model, local stdio MCP,
optional native sandbox and other OS/architecture smoke checks remain unverified.

Native reservation recovery is qualified with a disposable SQLite profile: a
simulated crashed native execution and a foreign active worker are archived,
restored and restarted through the real launcher. The tree API reports released
undispatched reservations, conservative estimated charging for dispatched calls
without outcomes, unchanged foreign reservations, and unchanged totals after a
second restart. This is recovery evidence, not actual provider billing evidence.

## Explicit local MCP processes

In the native installation, open **Connections → New connection** and choose
**Local process (stdio)**. Enter an absolute executable path, working directory,
and one argument per line. Install the MCP server independently; Weft does not
run package installers or import another application's MCP configuration.

Review the executable and arguments before checking the host-execution consent.
Saving an enabled connection starts that process to discover tools. Discovery
never grants tools: select exact tool permissions in each agent's configuration.
The existing gateway approval, argument-policy, quota and audit paths also apply
to local MCP calls. HTTP connections remain available independently.

No parent environment variables are inherited. Supply required variables as an
explicit JSON object in **Environment values (JSON)**, including `PATH` if the
server needs to find additional executables. Put credentials there, not in command
arguments. Values are encrypted at rest and never returned after saving; only
variable names are displayed. Blank keeps existing values and `{}` clears them.
Changing launch settings requires replacing or clearing previously stored values.
Connection types cannot be changed in place between HTTP and local processes.

This implementation uses session-based MCP 2025-06-18 and starts a fresh process
for each discovery or tool call. It bounds operations to 15 seconds, discards
child stderr and rejects malformed/oversized responses and credential echoes.
Calls with uncertain outcomes are not automatically replayed. Unix process groups
are terminated after each operation.
A failed check remains visible and can be retried after explicit configuration
repair. Disabled/deleted connections cannot supply tools to new calls.

The process runs with the Weft OS user's authority. Agent tool grants control
which MCP operations can be dispatched; they do not sandbox the MCP executable.
Host/path/command grant constraints cannot be silently reinterpreted as process
confinement. Docker/server profiles reject local-process registration and execution.
Remote vendor MCP compatibility and real credentials remain external validation.

## Updating the native bundle

Stop Weft, then create an offline backup with the current bundle before changing
binaries. Keep the previous bundle and the archive. Extract/build the new bundle
separately and start it with `--local --data-dir <existing-private-profile>`.
Do not run `--init-local` against existing data. The configured web directory must
continue pointing to the intended bundle's assets; update that path explicitly
when relocating assets. Never share this profile with the Docker installation.

`make test-native-upgrade` qualifies the first format-1 native launcher from commit
`faf4644` against the current launcher using disposable binaries/profile data. It
verifies a retained custom graph, first-time Software Factory seeding, unchanged
factory version on restart, no unintended execution, and byte-for-byte state
preservation when an unsupported installation format is rejected. This is a
format-1 compatibility test, not a promise of arbitrary future downgrade safety.

If a future format requires migration, that release must supply its own reviewed
migration and recovery procedure. The current launcher refuses unknown formats
before opening the database. To recover an old snapshot, preserve the failed
profile separately and use the documented restore procedure at the original,
absent path. Never overwrite or delete the failed profile as an automatic repair.


### Cleaning a finished task worktree

Stop the task and wait for **cancelled**, then expand **Review workspace cleanup**.
Select the shared worktree or an agent replica, inspect the path, retained branch,
commit and status, and confirm removal only after stopping external writers.
Only administrators can request cleanup. Modified, untracked and ignored files
block removal; preserve or handle them yourself before reviewing again. Locked
worktrees and changed reviews are rejected. Weft never uses force removal and
keeps the branch, task history and registered source repository. This removes one
checkout at a time; it does not delete database records or other task worktrees.


## Automated native qualification (2026-10-19)

Eight Chromium journeys run against a freshly initialized disposable profile using
the production native binary and bundled UI. They cover initial login, Software
Factory as the only preinstalled stopped graph, project registration, worktree
execution and diff inspection, independent agent worktrees, editor URI review,
MCP stdio failure/repair, preparation errors, reviewed cleanup and retained branches.
Parallel starts produce independent worktrees; cancelling one task preserves the
other task, both retained checkouts and the original dirty source.
A real loopback HTTP model fixture is configured through the Models UI and made
default. The unchanged Software Factory then routes a task through all six agents,
waits at its human approval gate and retains the final result after reload. Original
tracked and dirty source files remain intact. This is synthetic model behavior,
not a claim that an actual model produced correct software.

Native storage/lifecycle integration and `make test-native-upgrade` separately cover
restart, reservation reconciliation, backup/restore and format-1 upgrade preservation.
`make test-e2e` retains the disposable server-mode regression suite. Neither fixture
mode validates Docker daemon isolation or real provider credentials.

For your first manual test, use a new `--data-dir` and the sequence above. Final
external checks remain: your model/MCP accounts, VS Code application launch and
human review, optional real sandbox configuration, and runtime smoke checks on
other supported build targets. The existing Docker profile is not imported,
restarted or migrated by these commands.
