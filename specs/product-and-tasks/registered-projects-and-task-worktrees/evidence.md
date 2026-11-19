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
