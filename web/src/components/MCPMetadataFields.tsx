import type {MCPConnectionInput} from '../api/types';

export function MCPMetadataFields({input,onChange}:{input:MCPConnectionInput;onChange:(input:MCPConnectionInput)=>void}) {
 if(input.service!=='generic')return null;
 const langsmith=input.credentialType==='langsmith';
 const set=(name:string,value:string)=>{
  const headers={...input.metadataHeaders};
  if(value)headers[name]=value;else delete headers[name];
  onChange({...input,metadataHeaders:Object.keys(headers).length?headers:undefined});
 };
 return <details><summary>{langsmith?'LangSmith workspace settings':'dbt environment settings (optional)'}</summary>
  <p>These non-secret settings are saved with the connection. Changing them requires replacing or clearing an existing credential. Agent permissions remain explicit.</p>
  {langsmith?<>
   <label>LangSmith workspace ID<input value={input.metadataHeaders?.['langsmith-workspace-id']??''} onChange={e=>set('langsmith-workspace-id',e.target.value)} placeholder="Workspace UUID"/></label>
   <label>LangSmith API region<select aria-label="LangSmith API region" value={input.metadataHeaders?.['langsmith-endpoint']??''} onChange={e=>set('langsmith-endpoint',e.target.value)}><option value="">Server default</option><option value="https://api.smith.langchain.com">US</option><option value="https://eu.api.smith.langchain.com">EU</option></select></label>
   <p>This API-key profile targets the standalone LangSmith MCP server behind public HTTPS. The hosted LangSmith MCP OAuth flow is separate. Custom upstream URLs are not supported.</p>
  </>:<>
   {([['x-dbt-prod-environment-id','dbt production environment ID'],['x-dbt-dev-environment-id','dbt development environment ID'],['x-dbt-user-id','dbt user ID']] as const).map(([name,label])=><label key={name}>{label}<input inputMode="numeric" pattern="[1-9][0-9]{0,19}" value={input.metadataHeaders?.[name]??''} onChange={e=>set(name,e.target.value)}/></label>)}
   <p>Use numeric IDs, not URLs. The remote dbt endpoint uses Bearer tokens; SQL execution requires a personal token and the development environment/user IDs. Service tokens do not authorize execute_sql.</p>
  </>}
 </details>;
}
