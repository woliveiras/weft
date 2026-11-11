import { api } from '../api/client';
import type { ToolGrant } from '../api/types';
import { useAsync } from '../hooks/useData';
import { Btn, ErrorBox } from './common';
export function MCPGrants({grants,onChange}:{grants:(ToolGrant|string)[];onChange:(grants:(ToolGrant|string)[])=>void}) {
 const tools=useAsync(api.mcpTools,[]);
 return <section aria-label="MCP permissions"><h4>MCP permissions</h4>
  <p>Only explicitly selected MCP tools can run. Permissions and connection status are checked on every call. Save the graph to persist changes.</p>
  <Btn small onClick={()=>void tools.reload()} disabled={tools.loading}>Reload MCP tools</Btn><ErrorBox error={tools.error}/>
  {(tools.data??[]).map(t=><label className="tool-option" key={t.name}><input type="checkbox" checked={grants.some(g=>(typeof g==='string'?g:g.name)===t.name)} onChange={e=>onChange(e.target.checked?[...grants,{name:t.name}]:grants.filter(g=>(typeof g==='string'?g:g.name)!==t.name))}/><code>{t.name}</code></label>)}
  {grants.filter(g=>{const name=typeof g==='string'?g:g.name;return name.startsWith('mcp.')&&!tools.data?.some(t=>t.name===name);}).map(g=>{const name=typeof g==='string'?g:g.name;return <label className="tool-option" key={name}><input type="checkbox" checked onChange={()=>onChange(grants.filter(item=>(typeof item==='string'?item:item.name)!==name))}/><code>{name}</code> · Unavailable in the current catalogue; uncheck to remove this grant.</label>;})}
  {!tools.loading&&!tools.error&&!tools.data?.length&&<p>No enabled, discovered MCP tools. Configure connections in Integrations.</p>}
 </section>;
}
