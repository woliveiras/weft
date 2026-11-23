# TASK-008 historical evidence

These results were recorded at the time of the run. The documentation migration did not rerun them.

- 2026-10-19. The foundation passed two-worktree isolation, SQLite reopen, API role/error checks, six focused browser cases and the historical full checks with 303 frontend tests. Native cleanup validation used disposable worktrees.

## Projects console refinement — 2026-11-19

- `make test-web` passed TypeScript checking and all 484 tests in 91 files.
- `playwright test e2e/projects.spec.ts e2e/repository-imports.spec.ts` passed
  all six Chromium scenarios in the Docker E2E runner. Checks covered registration
  consent, independent task worktrees, dirty-source preservation, preference
  persistence, project search, import/retry/cancellation, viewer restrictions,
  authentication repair and 390 px reflow.
- The standard `docker compose up -d --build --no-deps weftd` rebuilt the local
  stack. The live Projects list, search/reset, registration fields and import
  columns were inspected in the browser. The initial list fit the viewport
  without vertical overflow, and registration inputs shared a baseline.
- This qualifies the console refinement under AC-001 and AC-002. Native cleanup
  remains covered by its prior evidence.

## Self-contained task Git — 2026-11-23

- Real rehearsal diagnosis: a linked task `.git` referred to imported source
  metadata outside the sole Docker workspace mount. Native Git and shell Git
  failed, although server-side push could still see the base branch. The failed
  live task was cancelled by the operator flow; this change does not migrate or
  delete its workspace or remote branch.
- New preparation streams only the reviewed commit ancestry into a fresh local
  object store. Unit/provenance and real Git + SQLite integration tests cover
  history, source replacement refs, dirty/config preservation, independent object
  storage, missing objects, durable partial preparation, storage failure/reopen,
  stale bindings and archive collisions. No existing assertion about source
  isolation or legacy worktree retention was removed.
- Cleanup retains all task refs, checkpoints, reflogs and objects before removing
  the clean checkout. Local replica cleanup preserves its private task base;
  changed refs, cross-replica pointers, locks, redirects and executable config
  fail closed. Both base and replica status use the existing confined snapshot
  and config allowlist; no clean/process filter or config include executes on the
  server. Legacy cleanup tests remain unchanged.
- The dedicated-daemon fixture `TestTaskCheckoutGitRealDaemon` passed at
  12:09:46Z (0.20 s) and was repeated against the final code at
  12:18:48Z (0.33 s): actual Git log, branch, add and commit succeeded at
  `/workspace`, exactly one workspace mount was present, and source content/HEAD
  stayed unchanged and inaccessible from the sandbox. The fixture reproduces
  the production UID 10001 ownership without changing private directory modes.
- Final affected Docker suite passed after cleanup hardening:
  `./scripts/go.sh test ./internal/projectconfig ./internal/workspace
  ./internal/taskrun ./internal/app ./internal/api -count=1` (0.911 s, 0.872 s,
  0.069 s, 0.139 s and 41.648 s respectively). An added FIFO/backlink/archive
  collision case passed with all `TestTaskCheckout*` tests (0.732 s).
  `git diff --check` passed. This qualifies shared-workspace task mounts and
  local replica cleanup, not Docker per-node linked replica execution.
- Backlog: Docker `WorkspaceWorktree` replicas still reference common metadata
  outside their mount. Resolving that requires a separate design preserving
  branch exchange/merge semantics; the rehearsal uses a shared task workspace.
