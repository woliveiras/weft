# 0005 — Default-deny capability grants enforced in a gateway

**Status:** Accepted

## Context
Agents run unattended with tools that change files, run commands and call
networks. The agent runtime receives a tool list and requests
directory scoping in the prompt. We need enforcement that does not depend on
the agent or framework behaving.

## Decision
Every tool call — from native and external agents — goes through the tool
gateway. A node's `tools` grants are capabilities or tool names with
wildcards; anything not granted is denied. Grants narrow further with
`paths` (jailed to the workspace, symlink-safe), `commands` (argv allowlist;
shell metacharacters rejected when restricted), `hosts` (with an SSRF guard),
`rateLimit` and `requiresApproval`. Models are only shown granted tools.
Every decision is audited in the event log and metered.

## Consequences
- Permissions are reviewable in the graph document and enforced uniformly.
- Commands still run with the sandbox's privileges: `shell.exec` is code
  execution; strong isolation comes from docker/gVisor/Kata sandboxes.
- New tools must declare a capability and apply constraints via the policy helpers.

## Alternatives
- **Prompt-level restrictions:** not enforcement.
- **OS-level only (containers):** necessary for defence in depth, but too
  coarse to express "may write src/** but not deploy".
