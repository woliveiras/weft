# PROD-11 behavior and oracle matrix

| Criterion | Scenario | Invariant | Observable oracle | Oracle provenance | Planned verification | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| AC-001 | A change enters the configured GitHub CI path. | Complete workflow | CI reports Go, Python, web, lint, build and browser results. Model behavior uses deterministic fixtures by default. | independent ([2026-11-11 production checklist](../../historical-source-map.md#tasksmd); proposed production qualification) | Review workflow coverage and observe a complete authorized GitHub run. | Pending |
| AC-002 | The repository import or MCP browser checks fail. | Red checks | The failure remains visible and blocks a green result. The historical failures are reproduced or verified resolved without weakening coverage. | independent ([2026-11-11 production checklist](../../historical-source-map.md#tasksmd); proposed production qualification) | Run both Go checks with real Git smart HTTP and the browser scenario with a fixture returning HTTP 401. Verify a deliberately failing fixture propagates to the workflow result during GitHub qualification. | [Local verification](./evidence.md#local-verification-on-2026-11-11). GitHub qualification pending. |

Capability criterion owners: [ENV-001 AC-001](../../workspaces-and-environments/repository-snapshot-imports/spec.md#ac-001--reviewed-snapshot), [INT-005 AC-003](../../providers-and-integrations/mcp-connections-and-tool-invocation/spec.md#ac-003--quotas-and-safe-history).
