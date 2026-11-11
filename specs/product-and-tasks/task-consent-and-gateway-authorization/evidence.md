# TASK-006 historical evidence

These results were recorded at the time of the run. The documentation migration did not rerun them.

- 2026-10-19. AUTH-02 passed focused Go suites, 21 Vitest cases and one Chromium journey for all modes. AUTH-03 passed focused Go suites, 15 Vitest cases and a Chromium live-reduction journey. The historical record notes a parallel cleanup timeout that passed on focused rerun.

## Go API role verification

- 2026-11-11. The consent, task approval and environment proposal role
  tests passed five repetitions with a real HTTP handler and disposable
  SQLite store. They covered anonymous, Viewer, Operator and Admin access
  to consent, cancellation, task progress, the approval inbox and decision
  routes. Denied requests left consent, approvals and task state intact.
  The task and inbox showed the same pending and final decision identities.

## Legacy execution compatibility

- 2026-11-11. `TestRunningLegacyTaskWithoutConsent` passed 20 repetitions.
  A disposable filesystem store held a running execution copy without a
  consent record. The gateway allowed the ungated grant, waited for approval
  on the gated grant, and denied a gated call in a non-interactive context
  and an ungranted call. The fixture provided the human approval response.
  Saving Ask, Workspace or Full returned a conflict and left consent absent.

## Independent review of ad5cfd5..965aa72

2026-11-11. Reviewed the consent, trigger, gateway, sandbox factory and
journal contracts against the maintainer's seven findings. Before the
corrections, the new regressions reproduced accepted Workspace consent for
`check.run` in both native modes, accepted trigger creation, a generic gateway
error for previously consented tasks and missing isolation text in the console.

| Check | Result |
| --- | --- |
| `GO_IMAGE=weft-review-go:git-http-backend ./scripts/go.sh test -count=1 ./internal/{taskauth,trigger,app,api,tools,workspacejournal}` | All six packages passed. The app integration used the real sandbox factory. The trigger integration verified a persisted pause before any tracker call or draft creation. |
| `./scripts/go.sh test ./internal/workspacejournal -run 'TestImport.*(Prune\|Retry)\|TestValidateChainRejectsAnchorCursorPastSegmentEnd' -count=20` | Passed 20 repetitions. After two real prunes, the importer returned the anchor-change error and left the SQLite cursor and events unchanged. |
| `./scripts/go.sh vet ./...`, `gofmt -l` on changed Go files, and `git diff --check` | Passed. `make lint` reports existing formatting in `server/internal/api/tasks.go`, also reproduced at `965aa72`. |
| `GO_IMAGE=weft-review-go:git-http-backend make test` | Passed the full Go, Python and web suites. |
| `make build` and `make test-python` | Passed. |
| `make test-web` and `pnpm exec tsc --project tsconfig.e2e.json` | Passed. Vitest ran 444 tests in 88 files. |
| Playwright, `web/e2e/task-authorization.spec.ts` | Both tests passed using local Chrome and a disposable fixture server built from the reviewed code. |

The default Go test image lacked `git-http-backend`. The API and
repository-import failures also reproduced at `965aa72`. The local Go test
image extends `golang:1.26-alpine` with Alpine's `git-daemon` package, which
provides that executable. The web checks used the repository-pinned pnpm
10.33.4 with the frozen lockfile and the public npm registry after the local
registry returned an error. The browser check used the installed Chrome and
a Darwin fixture binary cross-built by the Go container. PROD-05 production
qualification remains pending.
