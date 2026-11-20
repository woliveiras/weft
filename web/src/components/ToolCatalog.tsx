import {useState} from 'react';
import {api} from '../api/client';
import {useAsync} from '../hooks/useData';
import {Btn,ErrorBox} from './common';
import {AdminHeader,AdminPages,adminPage} from './AdministrationUI';
import {filterAdminTools} from './administrationData';

export function ToolCatalog() {
 const tools=useAsync(api.tools,[]);const [query,setQuery]=useState('');const [capability,setCapability]=useState('');const [page,setPage]=useState(0);
 const rows=filterAdminTools(tools.data??[],query,capability);
 return <section className="integration-section admin-section" aria-label="Tool catalog">
  <AdminHeader title="Tool catalog" description="Discover available tools and the capability each one requires. Configure access in each agent's permissions."><Btn disabled={tools.loading} onClick={()=>void tools.reload()}>{tools.loading?'Loading tools…':'Refresh tools'}</Btn></AdminHeader>
  <ErrorBox error={tools.error?`Could not load tool catalog: ${tools.error}`:undefined}/>
  {tools.error&&<Btn disabled={tools.loading} onClick={()=>void tools.reload()}>Retry tool catalog</Btn>}
  <div className="card admin-card"><div className="admin-filters"><label>Search tools<input type="search" placeholder="Name, description or capability" value={query} onChange={event=>{setQuery(event.target.value);setPage(0);}}/></label><label>Capability<select value={capability} onChange={event=>{setCapability(event.target.value);setPage(0);}}><option value="">All capabilities</option>{[...new Set(tools.data?.map(item=>item.capability))].sort().map(value=><option key={value}>{value}</option>)}</select></label><Btn onClick={()=>{setQuery('');setCapability('');setPage(0);}}>Clear filters</Btn></div>
  {tools.loading&&!tools.data&&<p role="status">Loading tool catalog…</p>}
  {tools.data&&<><p className="muted small">{rows.length} of {tools.data.length} tools · Name A–Z{tools.error?' · Previous result; refresh failed':''}</p>{rows.length?<div className="admin-table-scroll" tabIndex={0} role="region" aria-label="Available tools"><table className="table admin-tools-table"><thead><tr><th>Tool</th><th>Description</th><th>Required capability</th></tr></thead><tbody>{adminPage(rows,page).map(tool=><tr key={tool.name}><td><code>{tool.name}</code></td><td>{tool.description||'No description provided.'}</td><td><code>{tool.capability}</code></td></tr>)}</tbody></table></div>:<p className="empty">{tools.data.length?'No tools match these filters.':'No tools registered.'}</p>}<AdminPages page={page} total={rows.length} onPage={setPage}/></>}
  </div><p className="muted small">MCP connections are managed in <a href="#/connections">Connections</a>. A listed tool still requires an explicit agent grant.</p>
 </section>;
}
