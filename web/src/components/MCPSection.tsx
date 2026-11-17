import { LocalMCPFields, parseProcessEnvironment } from './LocalMCPFields';
import {MCPArgumentPolicyFields} from './MCPArgumentPolicyFields';
import {MCPAtlassianPolicyFields} from './MCPAtlassianPolicyFields';
import { MCPMetadataFields } from './MCPMetadataFields';
import { canDiscover, saveAndDiscover } from './mcpDiscovery';
import { ConnectionGrants } from './ConnectionGrants';
import { connectionTabs, connectionTabTitles, connectionLink, type ConnectionTab } from './mcpNavigation';
import { MCPHistory } from './MCPHistory';
import { MCPServiceGuide } from './MCPServiceGuide';
import { oauthDefaults, serviceOptions } from './mcpServices';
import { MCPOAuthReturn, MCPOAuthActions, MCPOAuthCallbackURL } from './MCPOAuth';
import { MCPTemplateSettings } from './MCPTemplateSettings';
import { MCPTemplates } from './MCPTemplates';
import { useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { MCPConnection, MCPConnectionInput } from '../api/types';
import { useAsync } from '../hooks/useData';
import { Badge, Btn, ErrorBox, JsonView, fmtDateTime } from './common';
const empty: MCPConnectionInput = {id:'',name:'',service:'generic',endpoint:'',enabled:true};
export function MCPSection({role,connectQuery,oauthQuery,presentation,connectionID,tab='overview'}:{role?:string;connectQuery?:string;oauthQuery?:string;presentation?:'create'|'detail';connectionID?:string;tab?:ConnectionTab}) {
  const list=useAsync(api.mcpConnections,[]);
  const info=useAsync(api.info,[]);
  const [environment,setEnvironment]=useState('');
  const [reviewedProcess,setReviewedProcess]=useState('');

  const [editing,setEditing]=useState(presentation!=='detail');
  const [toolSearch,setToolSearch]=useState('');
  const [input,setInput]=useState<MCPConnectionInput>(empty);
  const processFingerprint=JSON.stringify([input.localProcess,environment]);
  const [revision,setRevision]=useState<number>();
  const [gitlabInstance,setGitlabInstance]=useState('https://gitlab.com');
  const [token,setToken]=useState('');
  const [credentialEmail,setCredentialEmail]=useState('');
  const [clientSecret,setClientSecret]=useState('');
  const [clearClientSecret,setClearClientSecret]=useState(false);
  const [clear,setClear]=useState(false);
  const [busy,setBusy]=useState(false);
  const [error,setError]=useState<string>();
  const [saveProgress,setSaveProgress]=useState<string>();
  const [savedID,setSavedID]=useState<string>();
  const [deleting,setDeleting]=useState<MCPConnection>();
  const [deleteID,setDeleteID]=useState('');
  const admin=role==='admin';const operator=admin||role==='operator';
  const edit=(c?:MCPConnection)=>{setEditing(!!c||presentation!=='detail');setEnvironment('');setReviewedProcess('');setInput(c?{localProcess:c.localProcess,id:c.id,name:c.name,service:c.service,transportProfile:c.transportProfile,atlassianPolicy:c.atlassianPolicy,argumentPolicy:c.argumentPolicy,credentialType:c.credentialType,metadataHeaders:c.metadataHeaders,endpoint:c.endpoint,enabled:c.enabled,callsPerMinute:c.callsPerMinute,template:c.template,templateParameters:c.templateParameters,oauth:c.oauth?{registrationMode:c.oauth.registrationMode,issuer:c.oauth.issuer,clientId:c.oauth.clientId,scopes:c.oauth.scopes}:undefined}:empty);setRevision(c?.revision);setGitlabInstance(c?.service==='gitlab'?c.endpoint.replace(/\/api\/v4\/mcp$/, ''):'https://gitlab.com');setToken('');setCredentialEmail('');setClientSecret('');setClearClientSecret(false);setClear(false);setError(undefined);};
  function applyDefaults(){try{setInput({...input,...oauthDefaults(input.service,gitlabInstance),credentialType:undefined,template:undefined,templateParameters:undefined});setToken('');setCredentialEmail('');setClientSecret('');setClear(false);setClearClientSecret(!!revision);setError(undefined);}catch(e){setError(errorMessage(e));}}
  async function save(){
    if(input.localProcess&&reviewedProcess!==processFingerprint){setError('Review the local process before saving.');return;}
    setBusy(true);setError(undefined);setSavedID(undefined);setSaveProgress('Saving connection…');
    try {
      const result=await saveAndDiscover({...input,processEnvironment:input.localProcess?parseProcessEnvironment(environment):undefined,atlassianPolicy:input.atlassianPolicy?{...input.atlassianPolicy,cloudIds:input.atlassianPolicy.cloudIds.map(id=>id.trim()).filter(Boolean)}:undefined,oauth:input.oauth?{...input.oauth,scopes:input.oauth.scopes.filter(Boolean)}:undefined,credential:input.oauth?undefined:clear?{clear:true}:token?{value:token,...(input.credentialType==='basic'?{username:credentialEmail}:{})}:undefined,oauthClientSecret:input.oauth?(clearClientSecret?{clear:true}:clientSecret?{value:clientSecret}:undefined):undefined},revision,c=>{
        setEnvironment('');setReviewedProcess('');setToken('');setCredentialEmail('');setClientSecret('');setSavedID(c.id);setEditing(false);
        setSaveProgress(canDiscover(c)?'Connection saved. Discovering tools…':c.enabled?'Connection saved. Authorization required before tool discovery.':'Connection saved. Disabled connections are not tested.');
      });
      await list.reload();
      if(result.discoveryError){
        setSaveProgress('Connection saved. Tool discovery could not finish. Open the connection to retry.');
        setError(result.discoveryError);
      } else {
        edit();
        setSaveProgress(undefined);
        if(presentation)location.hash=connectionLink(result.connection.id);
      }
    }catch(e){setSaveProgress(undefined);setError(errorMessage(e));}
    finally{setBusy(false);}
  }
  async function discover(id:string,action:'test'|'tools/refresh'){setBusy(true);setError(undefined);try{await api.refreshMCP(id,action);await list.reload();}catch(e){setError(errorMessage(e));}finally{setBusy(false);}}
  async function remove(){if(!deleting||deleteID!==deleting.id)return;setBusy(true);setError(undefined);try{await api.deleteMCP(deleting.id,deleting.revision);if(input.id===deleting.id)edit();setDeleting(undefined);setDeleteID('');await list.reload();if(presentation)location.hash='#/connections';}catch(e){setError(errorMessage(e));setDeleting(undefined);setDeleteID('');await list.reload();}finally{setBusy(false);}}
  return <section className="integration-section" aria-label="MCP connections">
    {presentation?<p><a href="#/connections">Connections</a> / {presentation==='create'?'New connection':list.data?.find(c=>c.id===connectionID)?.name??connectionID}</p>:<h2>MCP connections</h2>}
    {presentation!=='detail'&&<><p>Register remote HTTPS Streamable HTTP servers and discover their tools. Only public network destinations are supported. Credentials stay encrypted in Weft.</p>
    <p>Assign tools to agents in the graph editor to allow calls through the gateway. OAuth supports manual registration, DCR and CIMD with PKCE; templates provide reviewed public defaults.</p></>}
    {oauthQuery!==undefined&&<MCPOAuthReturn key={oauthQuery} query={oauthQuery} canManage={admin} onChanged={list.reload}/> }
    {presentation!=='detail'&&<MCPTemplates connectionRoutes={!!presentation} query={connectQuery} canManage={admin} onSelect={t=>{edit();setInput({id:t.id,name:t.name,service:t.service,endpoint:t.endpoint,enabled:true,template:{id:t.id,version:t.version},oauth:t.authenticationModes?.length===1&&t.authenticationModes[0]==='oauth'?oauthDefaults(t.service).oauth:undefined});}} />}
    <Btn onClick={()=>void list.reload()} disabled={list.loading}>Reload MCP connections</Btn>
    <ErrorBox error={list.error}/><ErrorBox error={error}/>
    {saveProgress&&<p role="status">{saveProgress}</p>}
    {savedID&&!busy&&saveProgress&&<a href={connectionLink(savedID)}>Open saved connection</a>}
    {deleting&&<section role="dialog" aria-label={`Delete MCP ${deleting.name}`}>
      <h3>Delete {deleting.name}?</h3>
      <p>This permanently removes the local connection, catalogue and stored credentials. The ID remains reserved so old agent grants cannot authorize another connection. Graph definitions and audit history are preserved; remove obsolete grants in the graph editor.</p>
      <p>This does not delete the remote OAuth application or registration, or guarantee upstream revocation. Already dispatched operations cannot be rolled back.</p>
      <label>Type connection ID to confirm<input value={deleteID} onChange={e=>setDeleteID(e.target.value)}/></label>
      <Btn disabled={busy||deleteID!==deleting.id} onClick={()=>void remove()}>Confirm deletion</Btn>
      <Btn disabled={busy} onClick={()=>{setDeleting(undefined);setDeleteID('');}}>Cancel deletion</Btn>
    </section>}
    {list.loading&&<p role="status">Loading connections…</p>}
    {!list.loading&&!list.error&&!list.data?.length&&<p>No MCP connections configured.</p>}
    {presentation==='detail'&&!list.loading&&!list.error&&!list.data?.some(c=>c.id===connectionID)&&<p role="alert">Connection not found. It may have been deleted.</p>}
    {presentation==='detail'&&list.data?.filter(c=>c.id===connectionID).map(c=><header className="connection-heading" key={c.id}><h1>{c.name}</h1><p><Badge>{c.status}</Badge> {c.service} · Installation connection</p></header>)}
    {presentation==='detail'&&<nav className="section-navigation" aria-label="Connection sections">{connectionTabs.map(t=><a key={t} href={connectionLink(connectionID??'',t)} aria-current={tab===t?'page':undefined}>{connectionTabTitles[t]}</a>)}</nav>}
    {presentation==='create'&&!admin&&<p>An administrator must create this connection.</p>}
    {admin&&editing&&(presentation!=='detail'||tab==='settings')&&<form className="form" onSubmit={e=>{e.preventDefault();void save();}}>
      <h3>{revision?'Edit connection':'New connection'}</h3>
      {input.template&&<MCPTemplateSettings key={`${input.template.id}-${input.template.version}-${revision??'new'}`} input={input} onChange={value=>{setInput(value);setToken('');setCredentialEmail('');setClientSecret('');}}/>}
      <label>Connection ID<input required pattern="[a-z][a-z0-9_-]{0,63}" value={input.id} disabled={!!revision||busy} onChange={e=>setInput({...input,id:e.target.value})}/></label>
      <label>Connection calls per minute<input type="number" min={0} max={60000} step={1} value={input.callsPerMinute??0} onChange={e=>setInput({...input,callsPerMinute:Number(e.target.value)})}/></label>
      <p>0 means unlimited. A fixed UTC-minute quota is shared by all agents and replicas using this connection. Admitted calls count even if they fail; approval refusals do not consume it.</p>
      <label>Connection name<input required maxLength={120} value={input.name} onChange={e=>setInput({...input,name:e.target.value})}/></label>
      {!revision&&info.data?.nativeProfile&&<label>Connection transport<select value={input.localProcess?'stdio':'http'} onChange={e=>{setInput({...empty,id:input.id,name:input.name,...(e.target.value==='stdio'?{localProcess:{command:'',args:[],directory:''}}:{})});setEnvironment('');setReviewedProcess('');setToken('');setClear(false);}}><option value="http">Remote HTTP</option><option value="stdio">Local process (stdio)</option></select></label>}
      {input.localProcess?<LocalMCPFields value={input.localProcess} onChange={localProcess=>setInput({...input,localProcess})} environment={environment} onEnvironment={setEnvironment} reviewed={reviewedProcess===processFingerprint} onReview={checked=>setReviewedProcess(checked?processFingerprint:'')} storedKeys={list.data?.find(c=>c.id===input.id)?.environmentKeys}/>:<>
      <label>Service<select disabled={!!input.template} value={input.service} onChange={e=>setInput({...input,service:e.target.value})}>{serviceOptions.map(([value,label])=><option key={value} value={value}>{label}</option>)}</select></label>
      <MCPServiceGuide service={input.service}/>
      <MCPAtlassianPolicyFields input={input} onChange={setInput}/>
      <MCPArgumentPolicyFields input={input} onChange={setInput}/>
      {!input.template&&<>
        {input.service==='gitlab'&&<label>GitLab instance URL<input type="url" value={gitlabInstance} onChange={e=>setGitlabInstance(e.target.value)}/></label>}
        <p>Apply defaults to prepare OAuth fields. This clears the client registration fields in the form; review and save to change the connection.</p>
        <Btn disabled={busy} onClick={applyDefaults}>Use OAuth defaults</Btn>
      </>}
      <label>Transport profile<select aria-label="Transport profile" disabled={!!input.template||busy} value={input.transportProfile??''} onChange={e=>setInput({...input,transportProfile:e.target.value? 'stateless-2026-07-28':undefined})}><option value="">Session-based MCP 2025-06-18</option><option value="stateless-2026-07-28">Stateless MCP 2026-07-28 (bounded support)</option></select></label>
      {input.transportProfile&&<p>Explicit protocol selection. Additional input requests and custom parameter headers are unsupported. Changing profiles clears discovery and requires OAuth authorization again. Tool access still requires an explicit agent grant; this setting does not limit query costs.</p>}
      <label>MCP endpoint<input type="url" required placeholder="https://mcp.example.com/mcp" readOnly={!!input.template} value={input.endpoint} onChange={e=>setInput({...input,endpoint:e.target.value})}/></label>
      <label><input type="checkbox" checked={!!input.oauth} onChange={e=>{setInput({...input,credentialType:undefined,oauth:e.target.checked?(input.service==='databricks'?oauthDefaults('databricks').oauth:{issuer:'',clientId:'',scopes:[]}):undefined});setToken('');setCredentialEmail('');setClear(false);setClientSecret('');setClearClientSecret(false);}}/>Use browser OAuth</label>
      {input.oauth?<fieldset><legend>OAuth client registration</legend>
        <MCPOAuthCallbackURL/><p>Review the issuer and scopes before saving.</p>{(!input.oauth.registrationMode||input.oauth.registrationMode==='manual')&&!input.oauth.clientId&&<p role="status">Configuration pending: enter a pre-registered OAuth client ID.</p>}
        <label>Client registration method<select value={input.oauth.registrationMode||'manual'} onChange={e=>{setInput({...input,oauth:{...input.oauth!,registrationMode:e.target.value,clientId:''}});setClientSecret('');setClearClientSecret(true);}}>
          <option value="manual">Manual — pre-registered client</option><option value="auto">Automatic — prefer CIMD, then DCR</option><option value="dcr">DCR — register with provider</option><option value="cimd">CIMD — public client metadata</option>
        </select></label>
        {input.oauth.registrationMode&&input.oauth.registrationMode!=='manual'&&<p>Save, then review the available mechanism before authorizing. CIMD needs a public HTTPS installation URL reachable by the provider; localhost can use DCR or manual setup. Registration never assigns agent tools.</p>}
        <label>OAuth issuer<input required type="url" value={input.oauth.issuer} onChange={e=>setInput({...input,oauth:{...input.oauth!,issuer:e.target.value}})}/></label>
        {(!input.oauth.registrationMode||input.oauth.registrationMode==='manual')&&<label>OAuth client ID<input required value={input.oauth.clientId} onChange={e=>setInput({...input,oauth:{...input.oauth!,clientId:e.target.value}})}/></label>}
        <label>OAuth scopes (space separated)<input value={input.oauth.scopes.join(' ')} onChange={e=>setInput({...input,oauth:{...input.oauth!,scopes:e.target.value.split(' ')}})}/></label>
        {(!input.oauth.registrationMode||input.oauth.registrationMode==='manual')&&<><label>OAuth client secret (optional)<input type="password" autoComplete="new-password" disabled={clearClientSecret} value={clientSecret} onChange={e=>setClientSecret(e.target.value)}/></label>
        <p>Blank preserves an existing client secret. Public clients do not need one.</p>
        <label><input type="checkbox" checked={clearClientSecret} onChange={e=>{setClearClientSecret(e.target.checked);setClientSecret('');}}/>Clear OAuth client secret</label></>}
      </fieldset>:<>
      <label>Credential type<select aria-label="Credential type" value={input.credentialType||'bearer'} onChange={e=>{setInput({...input,metadataHeaders:undefined,credentialType:e.target.value as 'bearer'|'basic'|'langsmith'});setToken('');setCredentialEmail('');setClear(false);}}><option value="bearer">Bearer token</option>{(['atlassian','jira','confluence','bitbucket'].includes(input.service)||/^https:\/\/mcp\.atlassian\.com\//i.test(input.endpoint)||input.credentialType==='basic')&&<option value="basic">Atlassian personal API token (Basic)</option>}{input.service==='generic'&&<option value="langsmith">Standalone LangSmith API key</option>}</select></label>
      {input.credentialType==='basic'&&<><label>API token owner email<input type="email" autoComplete="off" value={credentialEmail} disabled={clear} onChange={e=>setCredentialEmail(e.target.value)}/></label><p>Enter the email and token together to replace the credential. Both are encrypted and never returned after saving. Organization admin enablement is required; use agent-interface scopes and supply cloudId where required. Service account keys use Bearer instead.</p></>}
      <label>{input.credentialType==='basic'?'Personal API token':input.credentialType==='langsmith'?'LangSmith API key':'Bearer token'}<input type="password" autoComplete="new-password" value={token} disabled={clear} onChange={e=>setToken(e.target.value)}/></label>
      <p className="muted small">Leave blank to keep the existing credential. Changing the endpoint requires replacing or clearing its credential.</p>
      <label><input type="checkbox" checked={clear} onChange={e=>{setClear(e.target.checked);setToken('');setCredentialEmail('');}}/>Clear stored credential</label></>}
      <MCPMetadataFields input={input} onChange={setInput}/>
      </>}
      <label><input type="checkbox" checked={input.enabled} onChange={e=>setInput({...input,enabled:e.target.checked})}/>Connection enabled</label>
      <div className="btn-row"><Btn type="submit" disabled={busy||!!input.localProcess&&reviewedProcess!==processFingerprint} tone="primary">Save MCP connection</Btn>{revision&&<Btn disabled={busy} onClick={()=>edit()}>Cancel connection edit</Btn>}{presentation==='create'&&<a href="#/connections">Cancel</a>}</div>
    </form>}
    <div className={presentation ? "connection-detail" : "cards"}>{(list.data??[]).filter(c=>!presentation||(presentation==='detail'&&c.id===connectionID)).map(c=><article className="card" key={c.id} aria-label={`MCP ${c.name}`}>
      {presentation ? <h2>{tab==='settings'?'Connection settings':tab==='overview'?'Connection overview':connectionTabTitles[tab]}</h2> : <h3>{c.name} <Badge>{c.status}</Badge></h3>}<p>{c.localProcess?'Local process · '+c.localProcess.command:c.service+' · '+c.endpoint}</p>{c.template&&<p>Template: {c.template.id} v{c.template.version}</p>}
      <p>{c.localProcess?(c.environmentKeys?.length?'Environment configured':'No environment values'):(c.credentialConfigured?'Credential configured':'No credential')} · revision {c.revision} · catalogue {c.toolsRevision}</p>
      {!!c.argumentPolicy?.length&&<p>Tool argument restrictions: {c.argumentPolicy.length} rules. Only listed tools with matching arguments are eligible.</p>}
      {c.atlassianPolicy&&<p>Atlassian read boundary: {c.atlassianPolicy.products.join(', ')} · {c.atlassianPolicy.cloudIds.length} allowed sites. Exact agent grants remain required.</p>}
      <p>Connection quota: {c.callsPerMinute?`${c.callsPerMinute} calls per UTC minute`:'Unlimited'}</p>
      {(!presentation||tab==='overview')&&c.oauth&&<MCPOAuthActions key={c.revision} connection={c} canManage={admin} onChanged={list.reload}/>}
      {(!presentation||tab==='overview'||tab==='tools')&&<section aria-label="Connection readiness">
        <p>Connection checks discover available tools. No agent is needed and no tool is called. Local connections launch the reviewed process for discovery.</p>
        {c.status==='ready'?<p role="status">{c.tools.length} tools discovered. Grant access to agents before they can use them.</p>:
         !c.enabled?<p>Connection disabled. Discovery is paused.</p>:
         c.oauth&&c.oauth.status!=='authorized'?<p>Authorize this connection to discover its tools.</p>:
         <p>Tools are not ready. Test the connection or refresh tools to retry discovery.</p>}
        {presentation&&tab==='overview'&&<a href={connectionLink(c.id,'tools')}>View discovered tools</a>}
      </section>}
      {c.lastTest&&<p>Last discovery: {fmtDateTime(c.lastTest)}</p>}<ErrorBox error={c.error}/>
      <div className="btn-row">{admin&&(!presentation||tab==='settings')&&<Btn disabled={busy} onClick={()=>edit(c)}>Edit {c.name}</Btn>}{operator&&(!presentation||tab==='overview'||tab==='tools')&&<><Btn disabled={busy||!c.enabled} onClick={()=>void discover(c.id,'test')}>Test {c.name}</Btn><Btn disabled={busy||!c.enabled} onClick={()=>void discover(c.id,'tools/refresh')}>Refresh tools for {c.name}</Btn></>}</div>
      {admin&&(!presentation||tab==='settings')&&<><p>To delete, disable the connection and revoke OAuth access first. Deleted IDs cannot be reused.</p><Btn disabled={busy||c.enabled||!!(c.oauth&&(c.oauth.status!=='revoked'||c.oauth.revocation==='pending'))} onClick={()=>{setDeleting(c);setDeleteID('');setError(undefined);}}>Delete {c.name}</Btn></>}
      {(!presentation||tab==='tools')&&<><label>Search tools<input type="search" value={toolSearch} onChange={e=>setToolSearch(e.target.value)}/></label><ul>{c.tools.filter(t=>[t.name,t.description].some(value=>(value??'').toLowerCase().includes(toolSearch.toLowerCase()))).map(t=><li key={t.name}><code>mcp.{c.id}.{t.name}</code><p>{t.description}</p><JsonView label="Input schema" value={t.inputSchema}/></li>)}</ul>
      {c.status==='ready'&&!c.tools.length&&<p>The server returned no tools.</p>}{!!c.tools.length&&!c.tools.some(t=>[t.name,t.description].some(value=>(value??'').toLowerCase().includes(toolSearch.toLowerCase())))&&<p>No tools match this search.</p>}</>}
      {presentation&&tab==='overview'&&<ConnectionGrants connectionID={c.id}/>}
      {presentation&&tab==='history'&&<MCPHistory key={c.id} connectionID={c.id}/>}
      {presentation&&tab==='settings'&&!admin&&<p>Only administrators can edit connection settings.</p>}
    </article>)}</div>
    {!presentation&&<MCPHistory/>}
  </section>;
}
