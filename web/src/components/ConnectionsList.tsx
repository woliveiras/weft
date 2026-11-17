import { useState } from 'react';
import { api } from '../api/client';
import { useAsync } from '../hooks/useData';
import { Badge, Btn, ErrorBox, fmtDateTime } from './common';
import { connectionLink, matchingConnections } from './mcpNavigation';

export function ConnectionsList({ role }: { role?: string }) {
  const list = useAsync(api.mcpConnections, []);
  const [search, setSearch] = useState('');
  const [status, setStatus] = useState('');
  const matches = matchingConnections(list.data ?? [], search, status);
  return <section aria-label="MCP connections" className="connection-list">
    <div className="section-head"><p className="muted">Connections belong to this installation. OAuth consent does not grant tools to agents.</p>
      {role === 'admin' && <a className="btn btn-primary" href="#/connections/new">Add connection</a>}</div>
    <div className="connection-filters"><label>Search connections<input type="search" value={search} onChange={e => setSearch(e.target.value)} placeholder="Name, service or ID" /></label>
      <label>Connection status<select value={status} onChange={e => setStatus(e.target.value)}><option value="">All statuses</option>{['ready', 'untested', 'unavailable', 'disabled'].map(s => <option key={s} value={s}>{s}</option>)}</select></label>
      <Btn disabled={list.loading} onClick={() => void list.reload()}>Reload MCP connections</Btn></div>
    <ErrorBox error={list.error} />
    {list.loading && <p role="status">Loading connections…</p>}
    {!list.loading && !list.error && !list.data?.length && <p>No MCP connections configured.</p>}
    {!list.loading && !!list.data?.length && !matches.length && <p>No connections match these filters.</p>}
    {!!matches.length && <div className="connection-table"><table><caption className="sr-only">Installation MCP connections</caption><thead><tr><th scope="col">Connection</th><th scope="col">Service</th><th scope="col">Status</th><th scope="col">Authorization</th><th scope="col">Last discovery</th></tr></thead><tbody>{matches.map(c => <tr key={c.id}>
      <td><a href={connectionLink(c.id)}>{c.name}</a><div className="muted small">{c.id}</div></td><td>{c.service}</td><td><Badge>{c.status}</Badge></td>
      <td>{c.oauth ? c.oauth.status : c.credentialConfigured ? 'Credential configured' : 'No credential'}</td><td>{c.lastTest ? fmtDateTime(c.lastTest) : 'Not checked'}</td>
    </tr>)}</tbody></table></div>}
    <p><a href="#/connections/history">View history across connections, including deleted connections</a></p>
  </section>;
}
