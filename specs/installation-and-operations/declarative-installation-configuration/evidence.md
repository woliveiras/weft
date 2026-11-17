# OPS-003 verification evidence

Qualification date: 2026-11-17. Scope: local fixtures, Go/console verification,
standard Dockerfile rebuild, and read-only export/plan against the rehearsal.
No production apply or live provider/tracker/SCM qualification was performed.

## Specification and oracle provenance

The specification and matrix were committed as `a7b0930` before implementation
(`5529157`, followed by CLI `846240c`). The nine criteria and original matrix
were derived from the maintainer request. Tests were primarily authored alongside
the implementation; they are not presented as an independent fail-first review.
The final review was performed by the implementing agent, without subagents or
an independent reviewer.

The approved decisions are recorded in OPS-003 and reconciled with INT-001,
OPS-001, RUN-006, RUN-007 and ENV-009. Provider management includes declarative
configuration. Reviewed console trigger scope edits record replacement consent;
a paused trigger stays paused. Declarative edits require the explicit consent
flag. Existing runner, readiness, authorization and network restrictions remain
in force. No acceptance criterion was relaxed to accommodate implementation.

| Criterion | Fail-first observation | Resolution |
| --- | --- | --- |
| AC-003 | `TestDeclarativeBudgetRevisionDetectsRestoredConsoleEdit` failed: after a console change followed by restoration, the original export incorrectly produced an OK plan. `/tmp/weft-ops003-budget-fail-first.log`. | `005853c` stores independent budget revision stamps, fences an absent definitions record, and preserves unrelated budgets. The regression plus pure revision and storage tests pass. |
| AC-006 | The first full Go run retained an old expectation that a reviewed console scope edit automatically pauses polling. | Updated the assertion to the explicit 2026-11-17 maintainer decision: current actor/scope consent, active/paused state preserved, readiness holds retained. Trigger unit, API and browser assertions verify the replacement behavior. |

## Acceptance coverage

All paths below are relative to the repository root.

| Criteria | Tests and observable assertions |
| --- | --- |
| AC-001 | `server/internal/installation/document_test.go`: all kinds, invalid documents, aliases, duplicate keys/identities, extensions, legacy graph ID. `TestDeclarativeKindValidationPaths`: typed invalid input for all eleven kinds, field-path diagnostics. |
| AC-002 | `TestResolverConfinement`: denied environment names and configured key names, traversal, absolute path, symlink escape, nonregular and oversized files. `TestDeclarativeFileCredentialAndRotation`: confined file credential, encryption at rest, changed-secret diff. `TestDeclarativeCreateRoundTripSecretsAndRevision`: omitted credential retained. `TestDeclarativeSecretNeverReachesLogsOrEvents`: actual nonempty captured application log, persisted events, response, plan and export contain no synthetic secret, including an upstream response that echoes it. |
| AC-003 | API create/round-trip/console-edit/object-force test; stale reviewed plan rejected even with force; discovery changes do not invalidate revisions. Budget restoration regression and `server/internal/cost/definitions_revision_test.go` prove unrelated-object stability and monotonic revisions. `TestDefinitionsAtRejectsConcurrentFirstWrite` preserves stored state after a stale absent-record write. |
| AC-004 | `TestDeclarativePartialFailureAndRetry`: injected durable-store failure leaves earlier writes applied and later objects not applied; retry completes. `TestDeclarativeNoImplicitRemoval`: omitted objects survive. All-kind import fixture and manual-environment fixture verify dependency ordering. |
| AC-005 | `TestDeclarativeAllKindsImportAndRoundTrip`: local real Git import and SQLite-backed API fixture, every kind present, no duplicate project, all exported objects unchanged on apply. Repeated export is byte-stable. Disabled providers, duplicate trigger display names and graph extensions also round-trip. Real installation export/plan: 22 unchanged objects. |
| AC-006 | `server/internal/trigger/declarative_test.go`, `TestDeclarativeTriggerConsentAndExport`, existing trigger/readiness unit and integration tests: atomic paused state without consent, actual admin actor and file mode with consent, no-op consent preservation. `web/e2e/triggers.spec.ts`: reviewed console edit retains active state and current admin consent. |
| AC-007 | Real local MCP protocol and SCM HTTP fixtures, encrypted manager persistence, all-kind Git import, manager retry tests. `TestDeclarativeMCPUnavailableReportsSanitizedDiscovery`: saved unavailable connection and sanitized error. `TestDeclarativeNetworkPrecedesManualEnvironmentWithoutBuild`: denied graph before policy, projected policy accepted before graph, manual definition retained, build warning, no Docker invocation or persisted build event. |
| AC-008 | `TestDeclarativeAuthorizationAndStalePlan`: all three routes reject anonymous/viewer/operator. CLI HTTP integration in `server/cmd/weft/config_test.go`: file/directory, dry-run, confirmation, EOF refusal, yes, force, export and legacy graph. Local symbolic links are explicitly rejected. |
| AC-009 | Guide, getting-started link, API/OpenAPI, complete example, optional Compose variables and confined read-only mount instructions. Standard image and live read-only evidence below. |

## Commands and results

No new Go/npm dependency or base image was acquired. `scripts/go.sh` successfully
used its cached Git/git-daemon test-image build. The previously cached
`leforge-e2e-runner:latest` supplied Chromium and the disposable browser/backend
fixture; it did not replace the standard runtime Dockerfile.

| Command / environment | Result | Local run artifact |
| --- | --- | --- |
| `./scripts/go.sh test ./...` | Pass, full Go unit/integration suite. Optional existing live-service tests remain environment-gated; no live integration variables were introduced. | `/tmp/weft-ops003-final-go-test.log` |
| `./scripts/go.sh test ./internal/cost ./internal/api ./cmd/weft` | Pass after budget revision correction. | `/tmp/weft-ops003-budget-correction.log` |
| Focused API manual-environment and unavailable-MCP tests | Pass. | `/tmp/weft-ops003-operational-fixtures.log` |
| `./scripts/go.sh vet ./...` and `make lint` | Pass. Lint runs gofmt and vet; final lint follows runtime corrections. | `/tmp/weft-ops003-vet.log`, `/tmp/weft-ops003-final-lint.log` |
| `docker run --rm --pull=never -v "$PWD/server:/src" -w /src golang:1.26-alpine gofmt -l .` | Pass, empty output. | `/tmp/weft-ops003-gofmt.log` |
| `make build` | Pass for all Makefile binaries. | `/tmp/weft-ops003-final-build.log` |
| Cached `golang:1.26-alpine`, module/build volumes, `CGO_ENABLED=0`: `GOOS=linux go build ./cmd/weftd ./cmd/weft` and `GOOS=darwin go build ./cmd/weftd ./cmd/weft` | Both pass. Windows is outside scope. | `/tmp/weft-ops003-platform-builds.log` |
| `make test-python` | Pass: 16 SDK, 14 agent and 10 script tests. | `/tmp/weft-ops003-python.log` |
| Cached `node:22-bookworm`, `npm_config_registry=https://registry.npmjs.org`, `leforge-web-node-modules`: `pnpm typecheck`, `pnpm exec vitest run` | Pass: 88 files, 466 tests. Non-failing existing duplicate React key warning in `AgentEnvironment.test.tsx`. | Session tool output |
| Same Node container: `pnpm build`, `pnpm exec tsc --project tsconfig.e2e.json` | Pass. | `/tmp/weft-ops003-web-build.log` |
| `./scripts/go.sh test -tags e2e ./cmd/weft-e2e` | Pass. | `/tmp/weft-ops003-e2e-go.log` |
| `pnpm exec playwright test e2e/triggers.spec.ts --config ops003.playwright.config.ts` in Node 22, connected to cached Chromium and isolated Go fixture | 6 passed. Temporary configuration changed only fixture/browser connection addresses and was removed afterward. Temporary fixture container removed; no volume deleted. | `/tmp/weft-ops003-playwright.log` |
| Existing Node `yaml` dependency: parse OpenAPI, resolve internal references, parse example | Pass: 306 references; 11 documents with distinct kinds. | `/tmp/weft-ops003-doc-validation.log` |
| `git diff --check` | Pass. | Session tool output |
| `docker compose up -d --build --no-deps weftd` | Pass using the standard Dockerfile and cached base images. No override or binary injection. | `/tmp/weft-ops003-final-compose-build.log` |
| Runtime CLI `weft export -o /tmp/ops003-roundtrip.yaml`, then `weft apply -f /tmp/ops003-roundtrip.yaml --dry-run` | Exit 0; 22 unchanged objects. | `/tmp/weft-ops003-live-roundtrip-plan.log` |
| Runtime CLI `weft apply -f /app/examples/installation/ --dry-run` | Exit 1 as expected: 5 unchanged, 3 unresolved references, 3 baseline conflicts. No apply. | `/tmp/weft-ops003-live-example-plan.log` |
| Export before/after and `cmp` | Exit 0; byte-identical configuration. | `/tmp/weft-ops003-live-preservation.log` |

The suites behind `make test` were run individually using the required toolchain
and registry settings. Live sandbox, provider, tracker, budget-spend and deployment
qualification beyond the specified read-only rehearsal were not run.

Earlier setup attempts were corrected without weakening checks: a plain Alpine
Go test container lacked Git for import fixtures; the cached test-image wrapper
provided it. An offline fallback initially used the wrong GOPATH and failed module
lookup; no modules were acquired. The browser CLI was initially addressed through
a nonexistent pnpm path, then used `node_modules/@playwright/test/cli.js`. A new
unavailable-MCP fixture initially used HTTP and was correctly rejected; it was
changed to a local TLS server, preserving the production HTTPS check.

## Real installation: read-only plan and preservation

Authentication used the existing container credential internally as `WEFT_TOKEN`;
no `.env`, key, certificate or credential value was read out or changed. Only
configuration export and dry-run were executed through the application on the real
stack. No SCM test, MCP refresh, import, environment build, task start, GitHub
write, direct Postgres write, network-policy change or real apply was performed.

Runtime image and tag match:

```text
sha256:0e6fd0468133f16ce67741c63796af57af025264d70edd1891b3c1c7bab6f6a5
weft/weftd:dev — running
```

The standard build log records the Dockerfile stages and cached base/module/npm
layers. No deployment override or replacement executable was used.

The before/after exports have the same SHA-256:

```text
8154eff2ed825183e5a4ab38daa1dc43d730c93be2290e4fe224ad5732cf2ba9
```

They cover the Gemini provider, prices, cost policy, budget definitions, MCP/SCM
connections, imported project, graph definitions, network policy and trigger
configuration/revision. No-op plan does not advance catalogue or network revisions.
The export excludes tasks, spend, transient trigger state and remote issue content;
the comparison is not a separate audit of those resources. No command targeted
those resources, and the paused trigger was never resumed. In particular, no
GitHub call was made to inspect or alter issue #1.

The final example plan follows. Existing `ready` lines describe stored status;
they do not mean the missing dedicated reference resolved or that discovery ran.

```text
unchanged InferencePlugin/gemini
error ModelProvider/gemini
  ready
  secretRef: secret reference unresolved
unchanged PriceCatalog/installation
unchanged CostPolicy/installation
unchanged Budget/installation/*
conflict NetworkPolicy/installation
  spec.egressHosts: ["proxy.golang.org","sum.golang.org"] -> ["proxy.golang.org","sum.golang.org","storage.googleapis.com"]
  object changed or has no known revision; use object-specific force
error MCPConnection/github
  ready
  secretRef: secret reference unresolved
error SCMConnection/weft-github
  secretRef: secret reference unresolved
unchanged Project/weft
conflict Graph/weft-deps
  spec.environment.network.allowedHosts: ["proxy.golang.org","sum.golang.org"] -> ["proxy.golang.org","sum.golang.org","storage.googleapis.com"]
  environment definition requires a build; apply does not build
  object changed or has no known revision; use object-specific force
conflict IssueTrigger/weft-issues
  spec.authorizationMode: "ask" -> "full"
  spec.projectId: null -> "prj_mwdrwas1588639869a967cd0b70e"
  Authorize trigger weft-issues to poll github with label weft:todo every 5 minutes, start workflow weft-deps at updater with authorization full and hand off through weft-github. Readiness enabled: true.
  consent required; trigger will be paused
  object changed or has no known revision; use object-specific force
error: plan contains errors or conflicts
```

## Documentation decision and residual work

Documentation is required because YAML, API, CLI confirmation/revision semantics,
secret references and consent are public contracts. Updated artifacts:
`docs/declarative-configuration.md`, `docs/getting-started.md`, `docs/api.md`,
`api/openapi.yaml`, `examples/installation/`, Compose and the affected specs.

The operator still needs to provision the dedicated references, review/adopt the
example's changes, explicitly decide on trigger consent, and build the graph
environment before the next operational rehearsal. These are intentionally not
performed on the preserved installation. The existing graph and network policy
lack `storage.googleapis.com`; the existing trigger uses `ask` and has no explicit
project binding. The example proposes the requested host, `full` and imported
project binding; the real state was retained.

No implicit deletion, console YAML editor, automatic environment build or optional
last-applied console badge was added. Static noneditable providers, runtime task
copies, spend history and OAuth browser tokens are not declarative objects.
Administrator writes on one server are serialized during a batch, so a long
import may delay another administrator write. Applications remain sequential
rather than transactional; earlier successful objects survive a later failure and the CLI records their revisions for retry.
External MCP/SCM availability and real provider credentials remain for the operator's
next rehearsal. No push was performed.

## Changed files

```text
.gitignore
api/openapi.yaml
docker-compose.yml
docs/api.md
docs/declarative-configuration.md
docs/getting-started.md
examples/installation/README.md
examples/installation/installation.yaml
server/cmd/weft/config.go
server/cmd/weft/config_test.go
server/cmd/weft/main.go
server/internal/api/config.go
server/internal/api/config_dependencies.go
server/internal/api/config_input.go
server/internal/api/config_snapshot.go
server/internal/api/config_test.go
server/internal/api/server.go
server/internal/api/triggers_test.go
server/internal/config/config.go
server/internal/cost/budget.go
server/internal/cost/declarative.go
server/internal/cost/definitions.go
server/internal/cost/definitions_revision_test.go
server/internal/cost/definitions_test.go
server/internal/installation/document.go
server/internal/installation/document_test.go
server/internal/installation/secrets.go
server/internal/llmconfig/declarative.go
server/internal/mcpconfig/declarative.go
server/internal/mcpconfig/manager.go
server/internal/scm/declarative.go
server/internal/scm/scm.go
server/internal/trigger/declarative.go
server/internal/trigger/declarative_test.go
server/internal/trigger/readiness_integration_test.go
server/internal/trigger/trigger.go
server/internal/trigger/trigger_test.go
specs/README.md
specs/installation-and-operations/compose-installation-and-operations/matrix.md
specs/installation-and-operations/compose-installation-and-operations/spec.md
specs/installation-and-operations/declarative-installation-configuration/evidence.md
specs/installation-and-operations/declarative-installation-configuration/matrix.md
specs/installation-and-operations/declarative-installation-configuration/spec.md
specs/providers-and-integrations/built-in-inference-plugin-management/spec.md
specs/runtime-and-delivery/issue-readiness-before-work/spec.md
specs/runtime-and-delivery/issue-references-and-polling-triggers/matrix.md
specs/runtime-and-delivery/issue-references-and-polling-triggers/spec.md
specs/workspaces-and-environments/environment-network-policy-and-egress/spec.md
web/e2e/triggers.spec.ts
web/src/components/TriggersSection.tsx
```
