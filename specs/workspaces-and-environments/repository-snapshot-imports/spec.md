---
id: ENV-001
title: "Repository Snapshot Imports"
summary: "Import reviewed Git snapshots and retain safe retry, authentication and worktree boundaries."
status: implemented
scope: ["repository snapshot imports"]
navigation: ["specs/README.md","specs/workspaces-and-environments/repository-snapshot-imports/matrix.md"]
documentation: ["docs/task-workflows.md","docs/api.md","docs/security.md"]
dependencies: ["TASK-008"]
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Repository Snapshot Imports

Import reviewed Git snapshots and retain safe retry, authentication and worktree boundaries.

## Behavior and invariants

- An explicit import pins a supported HTTPS Git reference to a commit, records progress, and reuses compatible concurrent requests. A task receives a separate reviewed worktree.

## Acceptance criteria

### AC-001 — Reviewed snapshot

An explicit import pins a supported HTTPS Git reference to a commit, records progress, and reuses compatible concurrent requests. A task receives a separate reviewed worktree.

### AC-002 — Credential boundary

Private access requires explicit authentication for the chosen endpoint. Native helpers require the native profile and explicit selection. Credentials are absent from persisted settings, Git config, read responses and audit payloads.

### AC-003 — Durable retry and cancellation

A failed or cancelled attempt remains inspectable. A revision-fenced retry uses a fresh attempt, handles restart and cannot duplicate a project after uncertain publication.

## Explicit exclusions

- Live private repositories, OS keychains and corporate restrictions require separate qualification. A registered local repository is governed by TASK-008.

## History

- 2026-10-19. PROMPT-03 introduced public snapshots. PROMPT-04 added reviewed private access and explicit native credential options.
- 2026-11-11. PROD-11 reproduced the Go package and API failures in an Alpine image without `git-http-backend`. Both passed on main with the `git-daemon` package provided by `f6dab53`. [Verification](./evidence.md#prod-11-local-verification).

## Reconciliation

- [task-workflows.md](../../../docs/task-workflows.md), [api.md](../../../docs/api.md), [security.md](../../../docs/security.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
