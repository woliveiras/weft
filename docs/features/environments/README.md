# Environments and command execution

For a saved task, Chat can review a build, request it and report its readiness. Starting the task against that build requires a separate review. This guide describes the environment and its full manual controls.

An environment prepares the toolchain image and setup required by Docker-isolated
commands. It is distinct from model-provider execution, repository storage,
task authorization and the host where Weft itself runs.

## Reusable workflow environment

1. Open **Workflow → Settings → Environment**
   (`#/g/<id>/settings/environment`).
2. Use **Discover environment** against the existing repository checkout, or
   select/enter a supported source definition. Review discovered candidates
   before accepting them. Discovery does not silently save/build the graph.
3. Configure image/toolchain/setup inputs and network requirements. Save the
   graph draft before building definitions that changed in the editor.
4. Review build/setup versus agent network requirements within the installation
   policy. `none`, `bridge` and `egress` have different contracts; permitted
   build networking is not automatically agent networking.
5. Start the explicit build and inspect phase/logs/failure. Build uses the
   configured sandbox daemon, not the browser machine. Compatible prepared
   images can be reused; changed inputs or explicit rebuild require new work.
6. Point agent Docker isolation at the prepared environment or its explicit
   image. A node image can change which environment image applies.

Graph build is an operator operation; policy changes and environment reset are
administrative. Discovery/read-only views do not grant authority to build,
download dependencies or change policy outside the installation's approval rules.

## Task preparation and start

Task setup's environment selector does not change the agent's isolation/grants:
workflow inherits them, Sandbox requires Docker for every enabled agent, and
local worktree requires native Weft. A process-isolated command in containerized
Weft executes inside the server container; it is not a dedicated task sandbox.

When task admission finds that preparation is needed:

1. Use **Run task** to save current choices and inspect that exact saved
   configuration. This does not interpret the request or start a build/agent.
   The setup summary's environment description is configuration guidance; the
   saved task review establishes whether preparation is needed and allowed.
2. Review the proposed task environment and explicitly prepare it.
3. The task creates its execution copy and may request `task_environment`
   approval. Inspect the actual build/setup scope before approval.
4. Follow **Environment & logs** on the task. Loading, failed snapshot, pending,
   building, failed build and ready are distinguishable. The bounded log panel
   can follow the latest output without expanding the whole page indefinitely.
5. If a build fails, inspect the failing phase before explicitly retrying. A
   policy rejection may need an installation change; a toolchain failure may
   need a corrected definition. Repeated refresh cannot fix either.
6. On ready, the task automatically inspects its saved requirements and shows
   access, grants and limits. Select **Run task** to start after inspection.
   A changed scope is shown for a new explicit Run decision; inspection failure
   blocks start and offers retry.
   Interactive preparation does not auto-start agents on build completion.

After start, command isolation and network policy remain enforced independently
of Ask/Workspace/Full task authorization. Logs are diagnostics, not proof that
the agent has run tests or delivered a PR.

## Retention and recovery

Environment capacity can queue admission; quota/capacity is different from a
build error. Suspended/stopped sandboxes can retain resources. Reset/replacement
and reviewed shared-resource cleanup are explicit operations with ownership
checks; they are not equivalent to deleting all Docker images or volumes.
Existing running networks retain their configuration until their documented
restart/re-admission boundary. See the owning guide before changing live policy.

## Implementation and verification map

- [AgentEnvironment](../../../web/src/components/AgentEnvironment.tsx),
  [TaskEnvironment](../../../web/src/components/TaskEnvironment.tsx),
  [GraphSettings](../../../web/src/components/GraphSettings.tsx).
- [Task environment UI tests](../../../web/src/components/TaskEnvironment.test.tsx),
  [workflow environment UI tests](../../../web/src/components/AgentEnvironment.test.tsx),
  [API routes](../../../server/internal/api/server.go).
- [Environment guide](../../environments.md), [sandbox runtime](../../sandbox-runtime.md),
  [execution model](../../execution-model.md), [administration](../administration/README.md).
