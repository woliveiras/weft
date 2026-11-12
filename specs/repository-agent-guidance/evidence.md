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
