---
id: RUN-003
title: "Native Coding Runtime"
summary: "Keep coding turns, tool execution and verification within Weft's native runtime and gateways."
status: implemented
scope: ["native coding runtime"]
navigation: ["specs/README.md","specs/runtime-and-delivery/native-coding-runtime/matrix.md"]
documentation: ["docs/native-coding-runtime.md","docs/tool-permissions.md","docs/execution-model.md"]
dependencies: ["TASK-006","ENV-002","INT-002"]
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Native Coding Runtime

Keep coding turns, tool execution and verification within Weft's native runtime and gateways.

## Behavior and invariants

- Native calls retain the task, system prompt, plan and current results while applying context limits, elision receipts and bounded compaction. Failures and compactions remain observable.

## Acceptance criteria

### AC-001 — Bounded model context

Native calls retain the task, system prompt, plan and current results while applying context limits, elision receipts and bounded compaction. Failures and compactions remain observable.

### AC-002 — Scoped edits and navigation

Edit tools enforce workspace grants and report failed matches. Multi-edit and patch operations preserve their atomic contract; navigation remains bounded by path and result limits.

### AC-003 — Verification plans and checkpoints

Declared checks run in the selected worktree under shell grants and expose outcome evidence. Plans belong to the execution. Worktree checkpoints are recorded before applicable edits and a reviewed restore preserves protected Git state.

### AC-004 — Streaming and accounting

Supported provider output streams to the task while completion, usage and budget accounting retain their existing semantics. Interruption is visible and cannot turn partial output into successful completion.

### AC-005 — Factory stages and role guidance

Software Factory v2 composes planning, implementation, read-only review, verification and SCM hand-off under the relevant grants. Small-model guidance is opt-in and the evaluation records whether the workflow completes.

## Explicit exclusions

- OpenCode was rejected. Model benchmark acceptance and production thresholds are RUN-004 and PROD-13.

## Open decisions

- The early local-first progress calls streaming future work. NAT-09 is recorded delivered in 29cf70a. NAT-10's local solve-rate expectation remains open despite delivered stages and scripted verification.

## History

- 2026-11-10. The maintainer chose Weft's native loop as the coding agent and set the evaluation-first delivery order.
- 2026-11-11. NAT-01–08 and NAT-10 were recorded delivered; streaming followed in 29cf70a.

## Reconciliation

- [native-coding-runtime.md](../../../docs/native-coding-runtime.md), [tool-permissions.md](../../../docs/tool-permissions.md), [execution-model.md](../../../docs/execution-model.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
