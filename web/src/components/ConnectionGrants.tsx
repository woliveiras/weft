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
  return <section aria-label="Connection agent grants" className="card connections-panel"><h2>Agent access</h2>
    <p className="muted">Grants come from saved workflow versions. Weft checks connection state, restrictions and approvals on every call. Open an agent to review its configuration.</p>
    {graphs.loading && <p role="status">Loading agent grants…</p>}
    <ErrorBox error={graphs.error} />{graphs.error && <Btn onClick={() => void graphs.reload()}>Reload agent grants</Btn>}
    {!graphs.loading && !graphs.error && !grants.length && <p>No saved agent grants for this connection.</p>}
    {grants.length > 0 && <div className="connection-table connections-table" role="region" aria-label="Agent grant list" tabIndex={0}>
      <table><caption className="sr-only">Saved agent grants</caption>
        <thead><tr><th scope="col">Agent</th><th scope="col">Workflow</th><th scope="col">Tool grants</th></tr></thead>
        <tbody>{grants.map(({graph, graphName, node, selected}) => <tr key={`${graph}/${node.id}`}>
          <td><a href={`#/g/${encodeURIComponent(graph)}/agent?agent=${encodeURIComponent(node.id)}&section=tools&connection=${encodeURIComponent(connectionID)}`}>{node.name || node.id}</a></td>
          <td>{graphName}</td>
          <td><details><summary>{selected.length} {selected.length === 1 ? 'tool' : 'tools'}</summary>
            <ul className="connection-granted-tools">{selected.map(t => <li key={t}><code>{t}</code></li>)}</ul>
          </details></td>
        </tr>)}</tbody>
      </table>
    </div>}
  </section>;
}
