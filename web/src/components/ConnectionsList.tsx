import { useRef, useState } from 'react';
import { api } from '../api/client';
import { useAsync } from '../hooks/useData';
import { Badge, Btn, Empty, ErrorBox, fmtDateTime, stateLabel } from './common';
import { connectionLink, connectionStatuses, matchingConnections } from './mcpNavigation';

export function ConnectionsList({ role }: { role?: string }) {
  const list = useAsync(api.mcpConnections, []);
  const [search, setSearch] = useState('');
  const [status, setStatus] = useState('');
  const searchField = useRef<HTMLInputElement>(null);
  const matches = matchingConnections(list.data ?? [], search, status);
  return <section aria-labelledby="mcp-connections-heading" className="card connections-panel">
    <header className="connections-panel-head">
      <div><h2 id="mcp-connections-heading">MCP connections</h2><p className="muted">Connect external tool servers. Grant tools to each agent in its permissions.</p></div>
      {role === 'admin' && <a className="btn btn-primary" href="#/connections/new" aria-describedby="mcp-connections-heading">Add connection</a>}
    </header>
    <div className="connections-toolbar">
      <div className="connection-filters">
        <label>Search connections<input ref={searchField} type="search" value={search} onChange={e => setSearch(e.target.value)} placeholder="Name, service or ID" /></label>
        <label>Connection status<select value={status} onChange={e => setStatus(e.target.value)}>
          <option value="">All statuses</option>
          {Object.entries(connectionStatuses).map(([key, { label }]) => <option key={key} value={key}>{label}</option>)}
        </select></label>
        {(search || status) && <Btn tone="ghost" onClick={() => {
          setSearch('');
          setStatus('');
          searchField.current?.focus();
        }}>Clear filters</Btn>}
      </div>
      <div className="btn-row">
        <a href="#/connections/history">View connection history</a>
        <Btn disabled={list.loading} onClick={() => void list.reload()}>Reload MCP connections</Btn>
      </div>
    </div>
    <ErrorBox error={list.error} />
    <p className="small muted connections-count" role="status">
      {list.loading ? 'Loading connections…' : list.error ? '' : list.data?.length ? `${matches.length} of ${list.data.length} connections` : ''}
    </p>
    {!list.loading && !list.error && !list.data?.length && <Empty>No MCP connections configured.</Empty>}
    {!list.loading && !!list.data?.length && !matches.length && <Empty>No connections match these filters.</Empty>}
    {!!matches.length && <div className="connection-table connections-table" role="region" aria-label="MCP connection list" tabIndex={0}>
      <table><caption className="sr-only">Installation MCP connections</caption>
        <thead><tr><th scope="col">Connection</th><th scope="col">Service</th><th scope="col">Status</th><th scope="col">Authorization</th><th scope="col">Last discovery</th></tr></thead>
        <tbody>{matches.map(c => <tr key={c.id}>
          <td><a href={connectionLink(c.id)}>{c.name}</a><span className="muted small connection-meta">{c.id}</span></td>
          <td>{c.service}</td><td><Badge tone={connectionStatuses[c.status]?.tone}>{connectionStatuses[c.status]?.label ?? c.status}</Badge></td>
          <td>{c.oauth ? `OAuth · ${stateLabel(c.oauth.status).replaceAll('_', ' ')}` : c.credentialConfigured ? 'Credential configured' : 'No credential'}</td>
          <td>{c.lastTest ? <time dateTime={c.lastTest}>{fmtDateTime(c.lastTest)}</time> : <span className="muted">Not checked</span>}</td>
        </tr>)}</tbody>
      </table>
    </div>}
  </section>;
}
