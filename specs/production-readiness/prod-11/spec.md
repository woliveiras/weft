---
id: PROD-11
title: Required GitHub CI checks
summary: "Run the required Go, Python, web and browser checks on every change in woliveiras/weft."
status: planned
scope: [production readiness]
risk: production qualification
risk_domains: [delivery, reliability]
review_policy: Maintainer review before implementation or live qualification
navigation: [specs/README.md, specs/production-readiness/prod-11/matrix.md]
documentation: Review affected operating guides during delivery
authority:
  granted: [specification drafting]
  withheld: [implementation, push, release, deploy, production changes, destructive actions, secret changes]
dependencies: []
---

# Required GitHub CI checks

Run the required Go, Python, web and browser checks on every change in woliveiras/weft.

## Behavior and invariants

The local baseline covers `internal/repositoryimport`, the API permissions and persistence test, and the MCP authentication scenario in `e2e/mcp.spec.ts`. Live model or vendor qualification stays opt-in.

## Acceptance criteria

- **AC-001 — Complete workflow.** A change enters the configured GitHub CI path. CI reports Go, Python, web, lint, build and browser results. Model behavior uses deterministic fixtures by default.
- **AC-002 — Red checks.** The repository import or MCP browser checks fail. The failure remains visible and blocks a green result. The recorded [repository import](../../../server/internal/repositoryimport), [API permissions and persistence test](../../../server/internal/api/repository_imports_test.go) and [MCP browser scenario](../../../web/e2e/mcp.spec.ts#L18) failures are reproduced or verified resolved without weakening coverage.

## Open decisions

- Reconfirm the historical intermittent sandbox journal supervision check
  when qualifying the browser and sandbox suites. It passed on the recorded
  diagnostic and full reruns after earlier failures.

- Event and branch protection configuration, runner and dependency/cache strategy, and ownership of future check failures.

## History

- 2026-11-11. The checklist recorded repository-import and MCP authentication browser failures for reconfirmation in required CI.
- 2026-11-11. Local checks confirmed that both repository-import failures were caused by the Alpine test image missing `git-http-backend`. Commit `f6dab53` supplied it through `git-daemon`. The maintainer approved updating the MCP browser oracle to require the documented HTTP 401 guidance and an empty tool catalogue. [Local evidence](./evidence.md#local-verification-on-2026-11-11).
- 2026-11-11. The complete local browser suite passed after isolating the Activity scenarios from other journeys and refreshing the two Models references. [Follow-up evidence](./evidence.md#activity-and-models-follow-up-on-2026-11-11).

## Capability criteria

- [ENV-001 AC-001](../../workspaces-and-environments/repository-snapshot-imports/spec.md#ac-001--reviewed-snapshot).
- [INT-005 AC-003](../../providers-and-integrations/mcp-connections-and-tool-invocation/spec.md#ac-003--quotas-and-safe-history).

## Reconciliation

- Supporting references: [Makefile](../../../Makefile) and [production supply chain](../../../docs/production-hardening.md#supply-chain).
- [Behavior and oracle matrix](./matrix.md).
- [Historical CI and environment notes](./evidence.md).
- Record test review before implementation review. Capture fresh evidence when the agreed checks run.
