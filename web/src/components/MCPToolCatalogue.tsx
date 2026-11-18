import { useState } from 'react';
import type { MCPConnection } from '../api/types';
import { Empty, JsonView } from './common';

export function MCPToolCatalogue({ connection }: { connection: MCPConnection }) {
  const [search, setSearch] = useState('');
  const query = search.trim().toLowerCase();
  const tools = connection.tools.filter(tool => {
    const name = `mcp.${connection.id}.${tool.name}`.toLowerCase();
    return name.includes(query) || tool.description?.toLowerCase().includes(query);
  });

  return <section className="card connections-panel" aria-label="Tool catalogue">
    <header className="connections-panel-head">
      <div><h2>Tool catalogue</h2><p className="muted">Search by name or description. Expand a tool’s input schema to inspect its parameters.</p></div>
      <p role="status" className="small muted">{tools.length} of {connection.tools.length} tools</p>
    </header>
    <label className="mcp-tool-search">Search tools<input type="search" value={search} onChange={event => setSearch(event.target.value)} placeholder="Tool name or description" /></label>
    {tools.length > 0 && <div className="mcp-tool-list" role="region" aria-label="Discovered tools" tabIndex={0}>
      <ul>{tools.map(tool => <li key={tool.name}>
        <code>mcp.{connection.id}.{tool.name}</code>
        {tool.description && <p>{tool.description}</p>}
        <JsonView label="Input schema" value={tool.inputSchema} />
      </li>)}</ul>
    </div>}
    {connection.status === 'ready' && connection.tools.length === 0 && <Empty>The server returned no tools.</Empty>}
    {connection.tools.length > 0 && tools.length === 0 && <Empty>No tools match this search.</Empty>}
  </section>;
}
