import {useEffect,useState} from 'react';
import {api,ApiError,errorMessage} from '../api/client';
import type {JiraCloudProfileInput} from '../api/types';
import {useAsync} from '../hooks/useData';
import {Badge,Btn,Empty,ErrorBox,fmtDateTime} from './common';
import {HelpField} from './FieldHelp';
import {cloudProfileLink} from './workflowEdition';
import {ConnectionFilters,emptyConnectionFilters} from './ConnectionFilters';

export function JiraCloudProfilesSection({role}:{role?:string}) {
 const list=useAsync(api.jiraCloudProfiles,[]);
 const [filters,setFilters]=useState(emptyConnectionFilters);
 const rows=list.data?.filter(p=>(!filters.status||(filters.status==='enabled'?p.enabled:!p.enabled))&&`${p.name} ${p.mcpConnection} ${p.siteUrl} ${p.projectId}`.toLowerCase().includes(filters.search.trim().toLowerCase()))??[];
 return <section className="card connections-panel" aria-labelledby="cloud-profiles-heading">
 <header className="connections-panel-head"><div><h2 id="cloud-profiles-heading">Jira Cloud workflow profiles</h2><p className="muted">Review a Cloud site and project for optional status synchronization. Credentials remain in the selected MCP connection.</p></div>{role==='admin'&&<a className="btn btn-primary" href="#/connections/jira-cloud/new">Add profile</a>}</header>
 <div className="connections-toolbar"><ConnectionFilters value={filters} onChange={setFilters} placeholder="Name, MCP, site or project ID" statusLabel="Profile status" statuses={{enabled:{label:'Enabled'},disabled:{label:'Disabled'}}}/><Btn disabled={list.loading} onClick={()=>void list.reload()}>Refresh Cloud profiles</Btn></div>
 <ErrorBox error={list.error}/><p role="status" className="small muted">{list.loading?'Loading Cloud profiles…':`${rows.length} of ${list.data?.length??0} profiles`}</p>
 {!list.loading&&!list.error&&!rows.length&&<Empty>{list.data?.length?'No Cloud profiles match these filters.':'No Cloud workflow profiles configured. Existing Jira Cloud triggers keep their current behavior.'}</Empty>}
 {!!rows.length&&<div className="connection-table connections-table" role="region" aria-label="Jira Cloud profile list" tabIndex={0}><table><thead><tr><th scope="col">Profile</th><th scope="col">MCP authority</th><th scope="col">Site and project</th><th scope="col">Status</th><th scope="col">Updated</th><th scope="col">Actions</th></tr></thead><tbody>{rows.map(p=><tr key={p.name}>
 <td><strong>{p.name}</strong><span className="small muted connection-meta">Revision {p.revision}</span></td><td>{p.mcpConnection}<span className="small muted connection-meta">MCP revision {p.mcpRevision}</span></td><td>{p.siteUrl}<span className="small muted connection-meta">Project ID {p.projectId}</span></td>
 <td><Badge tone={p.enabled&&p.projectRestricted?'ok':p.enabled?'warn':undefined}>{!p.enabled?'Disabled':p.projectRestricted?'Enabled profile':'Account review needed'}</Badge></td><td>{fmtDateTime(p.updatedAt)}</td>
 <td><div className="btn-row">{(role==='admin'||role==='operator')&&<a href={`${cloudProfileLink(p.name)}/transitions`}>Inspect transitions</a>}<a href={`${cloudProfileLink(p.name)}/mappings`}>Workflow mappings</a>{role==='admin'&&<a href={cloudProfileLink(p.name)}>Edit</a>}</div></td>
 </tr>)}</tbody></table></div>}
 </section>;
}

const empty:JiraCloudProfileInput={name:'',mcpConnection:'',mcpRevision:0,cloudId:'',siteUrl:'',projectId:'',enabled:true,projectRestricted:false};
export function JiraCloudProfileEditor({name,role}:{name?:string;role?:string}) {
 const saved=useAsync(()=>name?api.jiraCloudProfile(name):Promise.resolve(undefined),[name]);
 const connections=useAsync(api.mcpConnections,[]);
 const [draft,setDraft]=useState<JiraCloudProfileInput>(empty);
 const [busy,setBusy]=useState(false);const [error,setError]=useState<string>();const [conflict,setConflict]=useState(false);
 const [reviewed,setReviewed]=useState<string>();
 useEffect(()=>{if(saved.data){const p=saved.data;setDraft({name:p.name,mcpConnection:p.mcpConnection,mcpRevision:p.mcpRevision,cloudId:p.cloudId,siteUrl:p.siteUrl,projectId:p.projectId,enabled:p.enabled,projectRestricted:p.projectRestricted});setConflict(false);setReviewed(undefined);}},[saved.data]);
 const set=(patch:Partial<JiraCloudProfileInput>,scope=false)=>{setDraft(v=>({...v,...patch,...(scope?{projectRestricted:false}:{})}));setReviewed(undefined);setError(undefined);};
 const signature=JSON.stringify(draft);
 const offlineDisable=!!saved.data&&!draft.enabled&&Object.entries(draft).every(([key,value])=>key==='enabled'||saved.data?.[key as keyof JiraCloudProfileInput]===value);
 const selected=connections.data?.find(c=>c.id===draft.mcpConnection);
 const check=async()=>{if(busy||role!=='admin')return;setBusy(true);setError(undefined);try{await api.previewJiraCloudProfile(draft);setReviewed(signature);}catch(e){setError(errorMessage(e));}finally{setBusy(false);}};
 const save=async()=>{
  if(busy||role!=='admin'||conflict||(!offlineDisable&&reviewed!==signature)||(name&&!saved.data))return;
  const route=location.hash;setBusy(true);setError(undefined);
  try{await api.saveJiraCloudProfile(draft,saved.data?.revision);if(location.hash===route)location.hash='#/connections';}
  catch(e){setError(errorMessage(e));setConflict(e instanceof ApiError&&e.status===409);setReviewed(undefined);}finally{setBusy(false);}
 };
 return <section><header className="page-head connection-detail-head"><div><a href="#/connections">Connections</a><h1>{name?`Edit ${name}`:'New Jira Cloud workflow profile'}</h1><p className="muted">Bind an existing MCP authority to an explicit Cloud site and numeric project.</p></div></header>
 {role!=='admin'?<p>An administrator must configure workflow profiles.</p>:<>
 <ErrorBox error={saved.error||connections.error}/>{(saved.loading||connections.loading)&&<p role="status">Loading profile configuration…</p>}
 {(saved.error||connections.error)&&<Btn onClick={()=>{void saved.reload();void connections.reload();}}>Retry loading configuration</Btn>}
 {!saved.loading&&!saved.error&&(!name||(saved.data&&draft.name===name))&&<form className="card form cloud-profile-form" aria-label="Jira Cloud workflow profile" onSubmit={e=>{e.preventDefault();void save();}}>
 <ErrorBox error={error}/>{conflict&&<div role="alert"><p>This profile or its MCP authority changed. Reload the saved revision before reviewing again. Reloading discards form edits.</p><Btn disabled={busy} onClick={()=>void saved.reload()}>Reload saved profile</Btn></div>}
 <fieldset disabled={busy||conflict} className="cloud-profile-fields">
 <HelpField label="Profile name" help="A stable lowercase identifier selected explicitly by triggers. Saving does not attach a profile or a mapping to any workflow."><input required pattern="[a-z][a-z0-9_-]{0,63}" disabled={!!name} value={draft.name} onChange={e=>set({name:e.target.value})}/></HelpField>
 <HelpField label="MCP connection" help="Select an existing first-party Jira or Atlassian MCP connection with a static REST-compatible credential. OAuth and custom audiences cannot be reused for these REST operations. Compatibility is checked by Review profile."><select required disabled={connections.loading||!!connections.error} value={draft.mcpConnection} onChange={e=>{const c=connections.data?.find(v=>v.id===e.target.value);set({mcpConnection:e.target.value,mcpRevision:c?.revision??0},true);}}><option value="">Select MCP connection</option>{draft.mcpConnection&&!connections.data?.some(c=>c.id===draft.mcpConnection)&&<option value={draft.mcpConnection} disabled>{draft.mcpConnection} (unavailable)</option>}{connections.data?.map(c=><option key={c.id} value={c.id} disabled={!c.enabled}>{c.name} · {c.id} · Revision {c.revision}{c.enabled?'':' · disabled'}</option>)}</select></HelpField>
 <HelpField label="Reviewed MCP revision" help="Pins this profile to the exact saved MCP authority. Rotating credentials or changing its configuration requires another explicit profile review."><input type="number" min={1} required value={draft.mcpRevision||''} onChange={e=>set({mcpRevision:Number(e.target.value)},true)}/></HelpField>
 {selected&&selected.revision!==draft.mcpRevision&&<p role="note">The saved MCP connection is now revision {selected.revision}. Review its configuration before selecting that revision.</p>}
 <HelpField label="Jira cloud ID" help="The UUID of the Jira Cloud site. The API scope below is derived from this exact identifier; tenant URLs and Data Center URLs cannot be used."><input required pattern="[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}" value={draft.cloudId} onChange={e=>set({cloudId:e.target.value,siteUrl:e.target.value?`https://api.atlassian.com/ex/jira/${e.target.value}`:''},true)}/></HelpField>
 <HelpField label="Reviewed API scope" help="The first-party API destination for this Cloud ID. It is an API scope, not the browser address of your Jira site."><input readOnly value={draft.siteUrl}/></HelpField>
 <HelpField label="Project ID" help="The immutable numeric Jira project ID, such as 10001. A project key or board column name does not establish this scope."><input required pattern="[1-9][0-9]{0,19}" inputMode="numeric" value={draft.projectId} onChange={e=>set({projectId:e.target.value},true)}/></HelpField>
 <HelpField className="checkbox" label="I verified that the technical account can write only to this project" help="An administrator's confirmation of the remote permission scheme. Weft cannot restrict a broadly privileged account remotely. Leave unchecked until verified; read-only discovery remains available but automatic transitions require this confirmation."><input type="checkbox" checked={draft.projectRestricted} onChange={e=>set({projectRestricted:e.target.checked})}/></HelpField>
 <HelpField className="checkbox" label="Profile enabled" help="Disabling blocks new uses without deleting retained mappings or history. Changes invalidate earlier lifecycle pins; triggers require explicit reviewed consent again."><input type="checkbox" checked={draft.enabled} onChange={e=>set({enabled:e.target.checked})}/></HelpField>
 </fieldset>
 <p className="muted small">Review checks local scope and MCP compatibility. It does not contact Jira, prove project permissions, start a trigger or grant tools.</p>
 {reviewed===signature&&<p role="status">Profile reviewed. Save to persist this configuration.</p>}
 {busy&&<p role="status">Checking or saving profile…</p>}
 {offlineDisable&&<p role="note">Disabling this unchanged profile does not require contacting its MCP authority.</p>}
 <div className="btn-row"><Btn disabled={busy||conflict||connections.loading||!!connections.error} onClick={()=>void check()}>Review profile</Btn><Btn type="submit" tone="primary" disabled={busy||conflict||(!offlineDisable&&reviewed!==signature)}>Save profile</Btn>{busy?<Btn disabled>Cancel</Btn>:<a href="#/connections">Cancel</a>}</div>
 </form>}</>}
 </section>;
}
