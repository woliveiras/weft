---
id: PROD-11
title: Quality checks for direct pushes to main
summary: "Check the commit locally before pushing to main and verify each push with Go, web, build and browser checks in GitHub Actions."
status: in-progress
scope: [production readiness]
risk: production qualification
risk_domains: [delivery, reliability]
review_policy: Maintainer review before live qualification
navigation: [specs/README.md, specs/production-readiness/prod-11/matrix.md]
documentation: Review affected operating guides during delivery
authority:
  granted: [specification update, local implementation, local validation]
  withheld: [push, release, deploy, production changes, destructive actions, secret changes]
dependencies: []
---

# Quality checks for direct pushes to main

Contributors can use `main` as their sole working branch in `woliveiras/weft`. The local hook checks the commit before upload. GitHub Actions reports the checks for the SHA received by GitHub. Direct pushes require neither a pull request nor a minimum coverage percentage.

## Behavior and invariants

The local command uses the documented Go and web toolchains. The browser smoke uses Chromium, a disposable Weft backend and deterministic fixtures. Full browser and live model or vendor suites remain separately executable. CI checks run after GitHub receives a push and do not change the remote branch or publish an artifact for release.

## Acceptance criteria

- **AC-001 — Complete workflow.** Every push to `main` triggers GitHub Actions for the pushed commit. It runs the local check command (Go formatting and vet, Go tests, web type-check and tests), the build and the Playwright smoke.
- **AC-002 — Red checks.** Repository import, API permission and persistence, and MCP authentication regressions remain visible in the check results. Execution, check or setup failures propagate as failures. The browser job retains its report and any failure traces or screenshots.
- **AC-003 — Local push protection.** A contributor enables the versioned `pre-push` hook once per clone. For an update to remote `main`, it runs `make check` against the exact outgoing commit in a disposable directory and aborts the push if the check fails. Dirty files are ignored, and the hook leaves the checkout and unrelated ref updates alone.
- **AC-004 — Browser smoke.** Using the production web build and disposable API, Chromium verifies task consent, completion after approval, persisted rejection with no task result, and MCP authentication failure with an empty catalogue.

## Open decisions

- Reconfirm the historical intermittent sandbox journal supervision check
  when qualifying the browser and sandbox suites. It passed on the recorded
  diagnostic and full reruns after earlier failures.

- Ownership of failed checks and release qualification for the exact artifact SHA. Release enforcement is outside the first increment.

## History

- 2026-11-11. The checklist recorded repository-import and MCP authentication browser failures for reconfirmation in required CI.
- 2026-11-11. Local checks confirmed that both repository-import failures were caused by the Alpine test image missing `git-http-backend`. Commit `f6dab53` supplied it through `git-daemon`. The maintainer approved updating the MCP browser oracle to require the documented HTTP 401 guidance and an empty tool catalogue. [Local evidence](./evidence.md#local-verification-on-2026-11-11).
- 2026-11-11. The complete local browser suite passed after isolating the Activity scenarios from other journeys and refreshing the two Models references. [Follow-up evidence](./evidence.md#activity-and-models-follow-up-on-2026-11-11).
- 2026-11-28. The maintainer chose direct pushes to `main`, a local check and hook, and an after-push workflow with browser smoke. The CI baseline follows the Go/Eino distribution.

## Capability criteria

- [ENV-001 AC-001](../../workspaces-and-environments/repository-snapshot-imports/spec.md#ac-001--reviewed-snapshot).
- [INT-005 AC-003](../../providers-and-integrations/mcp-connections-and-tool-invocation/spec.md#ac-003--quotas-and-safe-history).

## Reconciliation

- Supporting references: [Makefile](../../../Makefile) and [production supply chain](../../../docs/production-hardening.md#supply-chain).
- [Behavior and oracle matrix](./matrix.md).
- [Historical CI and environment notes](./evidence.md).
- Record test review before implementation review. Capture fresh evidence when the agreed checks run.
