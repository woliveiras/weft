# REPO-GUIDANCE evidence

The maintainer-approved spec and matrix were committed as `47c4d37` before
test or implementation changes. The verification below used disposable
workspaces, local Git clones/worktrees, scripted inference, and the existing
gateway and storage adapters. The acceptance oracles are spec-derived.

## Fail-first

| Criterion | Verification | Observed failure before implementation |
| --- | --- | --- |
| AC-001, AC-004 | `./scripts/go.sh test ./internal/instructions ./internal/engine -run 'TestRepository(Preview\|GuidanceNative)' -count=1` | The preview had no root fallback and no skill catalogue. |
| AC-003, AC-004, AC-007 | Same run, native engine integration | The first model request contained the selected console body. Repository catalogue, root content, on-demand reads and their historical provenance were absent. |

## Acceptance evidence

| Criterion | Unit or component coverage | Integration coverage |
| --- | --- | --- |
| AC-001 | `TestRepositoryPreviewUsesRootFallbackAndCataloguesWithoutBodies`, `TestRepositoryBoundsAndInvalidRoot` | `TestRepositoryGuidanceNativeTurn`, `TestRepositoryGuidanceUsesCheckoutRootForAgentSubdirectory`, `TestRepositoryGuidanceWorktreesUseCommittedSkillAndReplicaBytes` |
| AC-002 | `TestRepositoryGuidanceValidatesSourceFamilies`, `TestRepositoryPolicyAndAliasResolution` | `TestRepositoryGuidanceGraphPolicyAndIncompatibleRuntime` and the Chromium admin save/preview flow |
| AC-003 | `TestRepositoryPolicyAndAliasResolution`, `TestRepositoryBoundsAndInvalidRoot` | `TestRepositoryGuidanceNativeTurn` observes ordered model context after reverse-order activation. Chromium asserts the console collision reason. |
| AC-004 | `TestRepositoryActivationBudgetAndAuditRecovery`, `TestRepositoryBoundsAndInvalidRoot` | `TestRepositoryGuidanceNativeTurn` observes catalogue-first delivery, one requested reference and the absence of unused content. |
| AC-005 | `TestRepositoryPolicyAndAliasResolution`, `TestRepositoryLockIsAdvisory` | `TestRepositoryGuidanceWorktreesUseCommittedSkillAndReplicaBytes` clones Weft, prepares two Git worktrees, verifies replica-specific root hashes, and loads the committed SDD skill and reference through a catalogue that deduplicates its alias. |
| AC-006 | `TestRepositoryPolicyAndAliasResolution`, `TestRepositoryReferenceCannotBeSwappedToSymlinkOrFIFO` | `TestRepositoryGuidanceNativeTurn` verifies ungranted shell and file writes are denied while a separately granted write succeeds. `TestRepositoryGuidanceCannotGrantReferenceApproval` verifies mandatory human approval and rejection on a reference read. |
| AC-007 | `TestRepositoryActivationBudgetAndAuditRecovery` checks hashes, idempotence and delivery states across a store reopen. | `TestRepositoryGuidanceGraphPolicyAndIncompatibleRuntime` checks Viewer and anonymous access. The Chromium Activity journey opens the saved execution as Viewer, checks paths, revisions, hashes, reads and omissions, then reloads. |
| AC-008 | `TestRepositoryPreviewUsesRootFallbackAndCataloguesWithoutBodies`, `TestRepositoryActivationBudgetAndAuditRecovery` | `TestRuntimeInstructionsCompatibilityBudgetAndPermissions`, `TestInstructionFailuresDoNotCallRuntimeOrConsumeOperatorTask`, `TestRepositoryGuidanceGraphPolicyAndIncompatibleRuntime`, `TestRepositoryGuidanceNeverDispatchesAnUnrecordedRead` |

## Verification runs

All runs below were local on 2026-11-11.

| Command or configuration | Result |
| --- | --- |
| `make test` | Passed the Go, Python and web suites. Web type-check and 444 Vitest tests passed. |
| `make test-go lint build` | Passed the final production-code Go suite, formatting check, vet and build targets. |
| `go -C server test ./internal/{instructions,engine} -run TestRepository -count=1` | Passed after the final lock and composition assertions were added. |
| `CGO_ENABLED=1 go -C server test -race ./internal/{instructions,engine} -run TestRepository -count=1` | Passed on macOS using the host Go toolchain. The container Go wrapper sets CGO to zero, so the host was used for the race check. |
| `pnpm -C web exec playwright test --config playwright.instructions.config.ts` | Both Chromium journeys passed, including the Activity Viewer scenario. |
| `playwright.activity.config.ts`, `playwright.oauth-cimd.config.ts`, `playwright.provider-management.config.ts` | All passed when run separately after the main browser command stopped. |
| `playwright.visual.config.ts` in the local Linux Playwright container | All 10 visual checks passed against the tracked Linux snapshots. The macOS attempt had no matching tracked baselines. |
| `make test-e2e` | Go E2E fixture checks and browser type-check passed. The main Chromium suite reported 112 passed, 7 skipped by its existing configuration, and one failure. The repository import journey timed out waiting for the Project combobox. |
| Main Chromium suite on an isolated archive of `47c4d37` | Reproduced the same Project combobox timeout at `repository-imports.spec.ts:26` with the pre-implementation code. That test passed when run by itself on the same revision. |

## Documentation

Updated [operator guidance](../../docs/instructions.md), [API](../../docs/api.md),
[native runtime](../../docs/native-coding-runtime.md), [runtime protocol](../../docs/runtime-protocol.md),
[graph schema](../../schemas/graph.schema.json) and
[instruction snapshot schema](../../schemas/instructions.schema.json).

## Review notes

The model selector is scoped to an execution's admitted catalogue. References
use the normal filesystem gateway, including path constraints and human
approval. Ordinary file reads remain ordinary tool operations. New audit
records persist read outcomes before context delivery and keep body content
out of the durable manifest. The selector makes no grant, credential or
approval changes.

The lock version is an advisory declaration. The loader validates its format
and records its status without recursively hashing unrequested package files.
The independent SHA-256 values identify the metadata and file contents
actually used. Live checkout validation detects changed metadata and package
identity. It can observe a changed body with stable metadata, as approved.

## Independent review of `965aa72..93a7b43` on 2026-11-12

| Review area | Regression evidence |
| --- | --- |
| Source framing | `TestProgressiveFrameQuotesAllPayloadLines` and `TestRepositoryGuidanceNativeTurn` failed before framing. The native model boundary now sees the apparent graph header, closer and task inside both the root instruction and the skill frames. |
| Checkout-relative grants | `TestRepositoryGuidanceUsesCheckoutRootForAgentSubdirectory` failed before the fix because both reference requests were denied. It now reads the granted path and denies its sibling. `TestGuidanceReferenceChecksCheckoutRelativeGrant` verifies the path grant before opening. |
| Console collisions | `TestRepositoryConsoleShadowingUsesSelectedAdmittedSkills` reproduced unwanted shadowing by a default, by a default after an agent opt-out, and by a console entry omitted for budget. These now leave the repository selection eligible. `TestAdmitRepositorySkillWhenSelectedConsoleDoesNotFit` exercises admission directly, and the native turn checks `shadowed_by_console` in persisted history. |
| Reference outcomes and telemetry | `TestGuidanceReferenceReportsReadFailure` verifies the gateway completion and the separate guidance reference error counter. The native subdirectory test checks a missing reference in the event store. `TestGuidanceLoadTelemetryUsesBoundedLabels` checks histogram samples and child spans for catalogue and body loads. |
| Safe and bounded Activity outcomes | `TestGuidanceCheckoutRootRedactsLookupFailure` and `TestRepositoryWorkspaceUnavailableSnapshotUsesRelativeSource` verify safe diagnostics. `TestToolCallBudgetRefusalCanBeDistinguishedFromYield` and `TestRepositoryGuidanceToolLimitRecordsActivityOmission` verify the exact refusal boundary and the historical omission without a digest. |
| Discovery and authorization | `TestDiscoveryRejectsSymlinksAndSpecialFiles` covers a symlink and FIFO at each of `AGENTS.md`, `.agents` and `SKILL.md`. `TestRepositoryCatalogueLimitAcrossConsoleAndCheckout` reaches `catalogue_limit` with bytes left. `TestGuidanceReadTextLimits` and the native subdirectory test cover oversized, invalid UTF-8 and NUL references. The native turn uses the registered SCM gateway to verify push and PR denials. `TestRepositoryGuidancePreservesReviewedAuthorizationMode` verifies persisted Ask, Workspace and Full consent and their approval behavior after loading repository text. |
| Trigger target validation | `TestEditRejectsMissingWorkflowOrAgent` and `TestEditingTriggerWithRemovedTargetReturns422` verify the field errors and persistence boundary. Before the correction the API returned 404 for a removed workflow and 200 for a removed result agent. |
| Readiness specification | RUN-007 D-08, D-09.1 and D-09.2 and their matrix rows record options for the rubric, slot occupancy and advancement of other issues. All three choices remain open. |

The explicit build matrix is recorded in [OPS-002 evidence](../installation-and-operations/standalone-installation-and-recovery/evidence.md).
The review reused the local Docker image with `--pull=never`. The Chromium
runs used the cached browser and the native E2E fixture cross-built in Docker
for macOS. The frozen web dependencies were restored from the local pnpm store.

| Verification | Result |
| --- | --- |
| `go test -count=1 ./...` in Docker on Linux | Passed the full default Go suite with a fresh run of all changed packages. |
| `go test -count=1 -tags e2e ./cmd/weft-e2e` in Docker on Linux | Passed the E2E fixture tests. |
| `go build ./...` and `go vet ./...`, with `GOOS=linux` and `GOOS=darwin` | All four commands passed in Docker. |
| `gofmt -l .` in the Go module | No output. |
| `make test-python` | 40 tests passed across the SDK, agents and scripts. |
| `pnpm -C web exec tsc --noEmit`, `pnpm -C web test`, `pnpm -C web exec tsc --project tsconfig.e2e.json` | Both type checks and 444 Vitest tests passed. |
| `pnpm -C web exec playwright test --config playwright.activity.config.ts` | Three Chromium scenarios passed. |
| `pnpm -C web exec playwright test --config playwright.instructions.config.ts` | Two Chromium scenarios passed, including the historical guidance view as Viewer. |

## Independent review follow-up on 2026-11-12

| Finding | Fail-first result | Verification after correction |
| --- | --- | --- |
| Reference authorization with `workspace.subdir` | The native fixture denied the contained workdir-relative grant and accepted references outside `src` under both open grants. | `TestGuidanceGrantPathPreservesPhysicalWorkdir` verifies mapping and confinement, including a workdir alias. `TestGuidanceReferenceChecksWorkdirRelativeGrant` checks the gateway. The native subdirectory fixtures now allow a contained grant and refuse outside references for both omitted `paths` and `**`. Discovery still sees the checkout root. |
| Body budget at activation | The unit and native fixtures kept the console catalogue active and did not offer the repository skill after refusing the console body. | `TestConsoleBodyBudgetOffersRepositoryFallback` verifies a fitting and non-fitting replacement, cumulative framing cost and repeat selection. `TestRepositoryGuidanceFallsBackAfterConsoleBodyBudget` verifies the initial shadow, console omission, replacement catalogue and explicitly selected body through provider requests and persisted Activity. |
| Precedence for a selected global skill | A lower-ranked default consumed the available catalogue budget. The native fixture also delivered the explicitly selected body at installation precedence. | `TestSelectedGlobalSkillUsesGraphBudgetPriority` verifies tight-budget admission and the installation duplicate. `TestRepositoryGuidanceSelectedGlobalSkillKeepsHigherPrecedence` verifies body order and the graph scope in historical evidence. |
| Journal write diagnostics | Re-running the journal unit and native execution tests with the old journal through a Go overlay showed the absolute path and raw cause in the returned error. | `TestGuidanceJournalRedactsStorageFailure` checks body and tool-limit writes without advancing delivery state. `TestRepositoryGuidanceNeverDispatchesAnUnrecordedRead` verifies the safe execution error and event, and that the body never reaches the model. |

| Verification | Result |
| --- | --- |
| `go test -count=1 ./...` and `go test -count=1 -tags e2e ./cmd/weft-e2e` | Passed in the locally cached Go image on Linux. |
| `go build ./...`, `go vet ./...` with `GOOS=linux` and `GOOS=darwin`, and `gofmt -l .` | Both arm64 builds and vet checks passed in Docker. The formatting check returned no files. |
| `pnpm -C web exec tsc --noEmit`, `pnpm -C web test`, `pnpm -C web exec tsc --project tsconfig.e2e.json` | Both type checks and 444 Vitest tests passed. |
| `pnpm -C web exec playwright test --config playwright.activity.config.ts` and `pnpm -C web exec playwright test --config playwright.instructions.config.ts` | Three Activity scenarios and two instruction scenarios passed. |

Go and the cross-built browser fixture used the cached
`weft-review-go:git-http-backend` image with `--pull=never`.
