# TASK-004 historical evidence

These results were recorded at the time of the run. The documentation migration did not rerun them.

- 2026-10-17. The user-observed tool count prompted assertions that distinguish recorded attempts from lifecycle records. Earlier feedback documents the pre-change presentation.

## Task-copy navigation regression — 2026-11-23

The console filtered task-owned execution copies out of its editable workflow inventory, then resolved run/tool deep links against that filtered inventory. A completed task therefore linked to a false “Graph not found” state even though its task and execution records remained available.

Task-owned run routes now use a dedicated evidence page. It checks both the persisted task ID and exact executionGraphId before loading run records; the existing RunsView still rejects execution records belonging to another graph. The same owner check precedes read-only workspace inspection. Task-copy routes do not mount the workflow editor, graph controls, approval controls or execution interruption controls. Unsupported editor destinations link back to the owning task. Backend authorization is unchanged.

Workspace inspection loads the matching graph status before mounting its file browser. Replica selectors preserve both configured slots and recorded replica identities after scale-down, without inventing historical indices between retained replicas. Missing/foreign status or omitted worktree nodes is an explicit unavailable/incomplete state with retry; historical task inspection does not silently assume replica zero.

Validation command, using the approved web test toolchain:

```sh
docker run --rm -e npm_config_registry=https://registry.npmjs.org \
  -v "$PWD/web:/app" -v leforge-web-node-modules:/app/node_modules \
  -w /app node:22-bookworm sh -ec \
  'npm test -- src/components/taskExecutionOwner.test.ts src/components/TaskExecutionPage.test.tsx src/components/workspaceReplicas.test.ts src/components/FilesView.test.tsx src/App.navigation.test.tsx src/components/RunsView.test.tsx src/routes.test.ts'
```

Result: 48 tests in seven files passed, as did `npm run typecheck` in the same Docker toolchain and `git diff --check`. Coverage includes identifier/ownership and replica-inventory unit cases; direct App navigation with no task copy in the workflow list; real API-client collaboration with task lookup, persisted execution and event rendering; missing/foreign records; dependency failure/retry; workspace/status response mismatch; worktree replica-one selection and exact node/replica scope in file-list/content requests; retained replicas after scale-down; incomplete historical inventory; and absence of mutation calls. This fixture-based UI integration does not replace real-browser qualification of the locally rebuilt stack.
