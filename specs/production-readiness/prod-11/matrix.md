# PROD-11 behavior and oracle matrix

| Criterion | Scenario | Invariant | Observable oracle | Oracle provenance | Planned verification | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| AC-001 | A change enters the configured GitHub CI path. | Complete workflow | CI reports Go, Python, web, lint, build and browser results. Model behavior uses deterministic fixtures by default. | independent (backlog and operating contracts, before new implementation) | Review workflow coverage and observe a complete authorized GitHub run. | Pending |
| AC-002 | The repository import or MCP browser checks fail. | Red checks | The failure remains visible and blocks a green result. The previously recorded the linked repository-import and MCP browser checks failures are reproduced or verified resolved without weakening coverage. | independent (backlog and operating contracts, before new implementation) | Run the existing checks and verify a deliberately failing fixture propagates to the workflow result. | Pending |

Capability criterion owners: [ENV-001 AC-001](../../workspaces-and-environments/repository-snapshot-imports/spec.md#ac-001--reviewed-snapshot), [INT-005 AC-003](../../providers-and-integrations/mcp-connections-and-tool-invocation/spec.md#ac-003--quotas-and-safe-history).
