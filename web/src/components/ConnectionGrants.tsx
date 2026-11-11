import { api } from '../api/client';
import { useAsync } from '../hooks/useData';
import { Btn, ErrorBox } from './common';

export function ConnectionGrants({ connectionID }: { connectionID: string }) {
  const graphs = useAsync(api.listGraphs, []);
  const grants = (graphs.data ?? []).flatMap(g => (g.graph.spec.nodes ?? []).flatMap(node => {
    const tools = node.tools ?? g.graph.spec.defaults?.tools ?? [];
    const selected = tools.map(tool => typeof tool === 'string' ? tool : tool.name).filter(name => name.startsWith(`mcp.${connectionID}.`));
    return selected.length ? [{ graph: g.graph.metadata.id, graphName: g.graph.metadata.name || g.graph.metadata.id, node, selected }] : [];
  }));
  return <section aria-label="Connection agent grants" className="integration-section"><h2>Agent access</h2>
    <p>Connection checks discover tools without an agent. Running an agent is an optional end-to-end check and may invoke its granted tools. Opening configuration does not start it.</p>
    <p className="muted">Saved graph grants, not a guarantee that calls are currently permitted. Connection state, constraints and approvals are checked on every call. Running graph versions may differ.</p>
    {graphs.loading && <p role="status">Loading agent grants…</p>}
    <ErrorBox error={graphs.error} />{graphs.error && <Btn onClick={() => void graphs.reload()}>Reload agent grants</Btn>}
    {!graphs.loading && !graphs.error && !grants.length && <p>No saved agent grants for this connection.</p>}
    <ul>{grants.map(({graph, graphName, node, selected}) => <li key={`${graph}/${node.id}`}><a href={`#/g/${encodeURIComponent(graph)}/agent?agent=${encodeURIComponent(node.id)}&section=tools&connection=${encodeURIComponent(connectionID)}`}>{node.name || node.id}</a> · Configure agent in graph {graphName} · {selected.length} tools
      <details><summary>Granted tool names</summary><ul>{selected.map(t => <li key={t}><code>{t}</code></li>)}</ul></details></li>)}</ul>
  </section>;
}
