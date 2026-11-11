# PROD-11 historical verification notes

- 2026-10-17. The UX checkpoint observed an intermittent sandbox journal
  supervision failure in a full run and one isolated repetition. A diagnostic
  repetition and the final full run passed. The cause remained open.
- 2026-11-11. The local-first record used the Docker Go wrapper and reported
  known repository-import and API failures. Browser checks used a local image
  with Playwright 1.64.1, Go 1.26 and Git, and recorded the MCP authentication
  failure on main. The recorded Node 22 setup used a separate dependency volume
  and the public registry after the corporate registry responded with HTTP 403.
- These environment notes describe the historical runs. Current CI
  qualification and any dependency acquisition remain pending.

## Local verification on 2026-11-11

The baseline was main at `4971068`. The runs used cached Docker images and
disposable fixtures. The Go wrapper built its test layer from
`golang:1.26-alpine`. Web unit checks used `node:22-bookworm` with
`leforge-web-node-modules` and `npm_config_registry=https://registry.npmjs.org`.
Browser runs used the cached `leforge-e2e-runner:latest` (`8310b460ffb6`) with
Playwright 1.64.1, Go 1.26.10, Git 2.43.0 and pnpm 10.33.4 installed via npm
inside the disposable container. The tagged backend was built before starting
Playwright to warm the Go cache.

### Repository import baseline

On main the Docker wrapper passed both checks before any changes. Repeated
uncached runs produced the following output.

```text
$ ./scripts/go.sh test ./internal/repositoryimport/... -count=10
ok  	github.com/woliveiras/weft/server/internal/repositoryimport	2.965s
?   	github.com/woliveiras/weft/server/internal/repositoryimport/importtest	[no test files]
$ ./scripts/go.sh test ./internal/api -run '^TestRepositoryImportAPIPermissionsAndPersistence$' -count=1 -v
=== RUN   TestRepositoryImportAPIPermissionsAndPersistence
--- PASS: TestRepositoryImportAPIPermissionsAndPersistence (0.05s)
PASS
ok  	github.com/woliveiras/weft/server/internal/api	0.056s
$ ./scripts/go.sh test ./internal/api -run '^TestRepositoryImportAPIPermissionsAndPersistence$' -count=20
ok  	github.com/woliveiras/weft/server/internal/api	2.067s
```

To verify the historical environment failure, the same tests ran without cache
in local Alpine image `001abdd57849` (Go 1.26.9 and Git 2.54.0). That image lacked
the CGI executable. The fixture's HTTP requests could not serve Git smart HTTP,
and the API import recorded state `failed` instead of reaching `ready`.
Selected output from those runs follows.

```text
git-http-backend-missing
2026/11/11 20:10:38 CGI error: fork/exec git-http-backend: no such file or directory
--- FAIL: TestPrivateImportRetryAndNoPersistedCredentials (0.07s)
--- FAIL: TestImportReuseConcurrentRequestsAndWorktreeIsolation (0.04s)
--- FAIL: TestBridgeWithRealGit (0.03s)
--- FAIL: TestCancelAndRetryAfterManagerRestart (0.06s)
--- FAIL: TestRecoversUncertainPublicationWithoutDuplicatingProject (5.02s)
FAIL	github.com/woliveiras/weft/server/internal/repositoryimport	5.324s
--- FAIL: TestRepositoryImportAPIPermissionsAndPersistence (0.06s)
FAIL	github.com/woliveiras/weft/server/internal/api	0.076s
package=1 api=1
```

In the current wrapper image, `apk info --who-owns` reports
`/usr/libexec/git-core/git-http-backend is owned by git-daemon-2.54.1-r0`.
Commit `f6dab53` already added that package. The importer and its API tests
required no changes.

### MCP browser baseline and review

`pnpm exec playwright test e2e/mcp.spec.ts:18` failed in Chromium. Selected
output follows.

```text
Error: expect(locator).toContainText(expected) failed
Expected substring: "Discovery failed"
Timeout: 15000ms
1 failed
```

The received card showed `unavailable` and `MCP credentials rejected (401).
Open the connection and reauthorize OAuth or replace its credential, then test
the connection.` The [remote access contract](../../../docs/mcp.md#remote-access-failures)
and the existing Go and browser access-diagnostics tests call for specific
guidance on 401. The old browser assertion required the generic discovery
message and omitted the empty-catalogue check named in the scenario.

The maintainer approved updating the assertion after reproduction. The test
keeps the real fixture and the manual **Test No auth** action. It checks the
response to that specific request for `unavailable` and `tools: []`, requires
the full 401 diagnostic in the visible alert and checks that the card renders
zero tool entries. The existing unit test `TestAccessGuidanceAllowlist` and
integration test `TestRemoteAccessDiagnosticsThroughGatewayAndDiscovery`
exercise the server's error mapping and catalogue invalidation.

### Verification results

| Command | Result |
| --- | --- |
| `./scripts/go.sh test ./...` | Passed. |
| `./scripts/go.sh test ./... -count=1` | Passed without cache. `internal/api` took 5.823 s and `internal/repositoryimport` took 0.550 s. |
| `./scripts/go.sh vet ./...` | Passed with no diagnostics. |
| `docker run --rm -v "$PWD:/repo" -w /repo/server golang:1.26-alpine gofmt -l internal` | Passed with no files listed. |
| `make lint` and `make build` | Passed. |
| `make test-python` | Passed all 40 tests across the SDK, agent and script suites. |
| `pnpm exec vitest run` | Passed 444 tests in 88 files. |
| `pnpm typecheck` | Passed. |
| `./scripts/go.sh test ./internal/mcpconfig ./internal/api -run 'TestAccessGuidanceAllowlist\|TestRemoteAccessDiagnosticsThroughGatewayAndDiscovery\|TestMCP' -count=1` | Passed. |
| `go test -tags e2e ./cmd/weft-e2e` and `pnpm exec tsc --project tsconfig.e2e.json` | Passed. |
| `pnpm exec playwright test e2e/mcp.spec.ts:18` | 1 passed (3.6 s). The same case passed in the full Chromium run. |
| `pnpm exec playwright test --config playwright.config.ts` | 114 passed, 1 failed and 7 fixture-gated cases skipped (3.1 min). All four repository-import scenarios passed. |
| `pnpm exec playwright test --config playwright.oauth-cimd.config.ts` | 1 passed. |
| `pnpm exec playwright test --config playwright.provider-management.config.ts` | 1 passed. |
| `pnpm exec playwright test --config playwright.visual.config.ts` | 8 passed and 2 failed. |
| `WEFT_E2E_PROMPT_MODEL=1 pnpm exec playwright test e2e/task-prompts.spec.ts e2e/prompt-qualification.spec.ts` | 8 passed, including all seven cases gated in the standard configuration. |
| `pnpm exec playwright test e2e/work-activity.spec.ts:66` | 1 passed in isolation. |

The four Playwright configurations are those invoked by `pnpm run test:e2e`.
All four ran independently so failures in the main configuration did not
prevent the other configurations from running. No fixture gates were changed.

The following failures outside the three scoped checks were present on the
baseline and again after the MCP test correction.

- [Activity](../../../web/e2e/work-activity.spec.ts#L66) expected its fixture's
  link in `Slowest` and reported `Error: element(s) not found`. The final
  trace showed the fixture in the Agent health table with p90 0 ms, while the
  Slowest board was full with ten slower entries from earlier scenarios. The
  same check passed against a fresh backend in isolation.
- [Models visual snapshots](../../../web/e2e/visual.spec.ts#L20) failed in both
  themes. The final report recorded `31321 pixels (ratio 0.04 of all image pixels)
  are different.` for light and `32071 pixels (ratio 0.04 of all image pixels)
  are different.` for dark. The baselines require review.

GitHub workflow and branch protection qualification remain open. The local
browser suite still reports the Activity ranking and Models snapshot failures.
