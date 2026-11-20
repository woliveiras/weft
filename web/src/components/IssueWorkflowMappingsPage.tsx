import {useEffect,useState} from 'react';
import {api, ApiError, errorMessage} from '../api/client';
import type {IssueConnection, IssueTransition, IssueTransitionDiscovery, IssueWorkflowMapping, IssueWorkflowMappingInput} from '../api/types';
import {useAsync} from '../hooks/useData';
import {Badge, Btn, ErrorBox, fmtDateTime} from './common';
import {HelpField} from './FieldHelp';
import {issueConnectionLink} from './IssueConnectionsSection';

export const mappingInput=(v:IssueWorkflowMapping):IssueWorkflowMappingInput=>({name:v.name,connection:v.connection,connectionRevision:v.connectionRevision,sampleIssueKey:v.sampleIssueKey,sampleIssueId:v.sampleIssueId,issueTypeId:v.issueTypeId,fromStatusId:v.fromStatusId,transitionId:v.transitionId,targetStatusId:v.targetStatusId,issueUpdated:v.issueUpdated,enabled:v.enabled});
export function canMapTransition(t:IssueTransition,current:string):boolean{return t.to.id!==current&&t.fields!==null&&Object.values(t.fields).every(f=>f.required===false);}
export function IssueWorkflowMappingsPage({name,route,role}:{name:string;route:{name?:string;create?:boolean};role?:string}){
 const connection=useAsync(()=>api.issueConnection(name),[name]);
 const base=`${issueConnectionLink(name)}/mappings`;
 return <section><header className="page-head connection-detail-head"><div><a href="#/connections">Connections</a><h1>{route.create?'New workflow mapping':route.name?'Review workflow mapping':'Jira workflow mappings'}</h1><p className="muted">Select actual Jira transition IDs. Saving a definition does not attach it to a workflow or execute it.</p></div>
 {!route.create&&!route.name&&role==='admin'&&<a className="btn btn-primary" href={`${base}/new`}>New mapping</a>}</header>
 <ErrorBox error={connection.error}/>{connection.loading&&<p role="status">Loading issue connection…</p>}
 {connection.error&&<Btn onClick={()=>void connection.reload()}>Retry loading connection</Btn>}
 {connection.data&&!connection.loading&&!connection.error&&<><p>{name} · {connection.data.siteUrl} · Project ID {connection.data.projectId} · Revision {connection.data.revision}</p>
 {route.create||route.name?<MappingEditor key={route.name??'new'} connection={connection.data} mappingName={route.name} role={role}/>:<MappingList connection={connection.data} role={role}/>}</>}
 </section>;
}
function MappingList({connection,role}:{connection:IssueConnection;role?:string}){
 const list=useAsync(()=>api.issueWorkflowMappings(connection.name),[connection.name]);
 const [busy,setBusy]=useState(false);const [error,setError]=useState<string>();
 const disable=async(v:IssueWorkflowMapping)=>{
  if(busy||role!=='admin')return;setBusy(true);setError(undefined);
  try{await api.saveIssueWorkflowMapping({...mappingInput(v),enabled:false},v.revision);await list.reload();}
  catch(e){setError(errorMessage(e));}finally{setBusy(false);}
 };
 return <section className="card connections-panel" aria-label="Workflow mapping list">
 <header className="connections-panel-head"><h2>Saved definitions</h2><Btn disabled={busy||list.loading} onClick={()=>void list.reload()}>Refresh mappings</Btn></header>
 <ErrorBox error={list.error||error}/>{list.loading&&<p role="status">Loading mappings…</p>}
 {!list.loading&&!list.error&&!list.data?.length&&<p className="empty">No mappings for this connection. Without a selected mapping, Weft keeps the comment and label lifecycle.</p>}
 {!!list.data?.length&&<div className="connection-table connections-table" role="region" aria-label="Workflow mapping table" tabIndex={0}><table><thead><tr><th scope="col">Mapping</th><th scope="col">Transition</th><th scope="col">Scope</th><th scope="col">Status</th><th scope="col">Updated</th><th scope="col">Actions</th></tr></thead><tbody>{list.data.map(v=><tr key={v.name}>
 <td><strong>{v.name}</strong><span className="connection-meta small muted">Revision {v.revision}</span></td>
 <td>{v.transitionName} → {v.targetStatusName}<span className="connection-meta small muted">Transition {v.transitionId} · Status {v.fromStatusId} → {v.targetStatusId}</span></td>
 <td>Type {v.issueTypeId}<span className="connection-meta small muted">Project {v.projectId} · Connection revision {v.connectionRevision}</span></td>
 <td><Badge tone={!v.enabled?undefined:v.connectionRevision!==connection.revision?'warn':'ok'}>{!v.enabled?'Disabled':v.connectionRevision!==connection.revision?'Connection changed':'Enabled definition'}</Badge></td>
 <td><time dateTime={v.updatedAt}>{fmtDateTime(v.updatedAt)}</time></td><td><div className="btn-row"><a href={`${issueConnectionLink(connection.name)}/mappings/c/${encodeURIComponent(v.name)}`}>Review definition</a>{role==='admin'&&v.enabled&&<Btn small disabled={busy} onClick={()=>void disable(v)}>{busy?'Saving…':'Disable'}</Btn>}</div></td>
 </tr>)}</tbody></table></div>}
 </section>;
}
function MappingEditor({connection,mappingName,role}:{connection:IssueConnection;mappingName?:string;role?:string}){
 const saved=useAsync(()=>mappingName?api.issueWorkflowMapping(mappingName):Promise.resolve(undefined),[mappingName]);
 const [name,setName]=useState('');const [sample,setSample]=useState('');
 const [enabled,setEnabled]=useState(true);
 useEffect(()=>{if(saved.data)setEnabled(saved.data.enabled);},[saved.data]);
 const [discovery,setDiscovery]=useState<IssueTransitionDiscovery>();const [transition,setTransition]=useState('');
 const [busy,setBusy]=useState(false);const [conflict,setConflict]=useState(false);const [error,setError]=useState<string>();
 const base=`${issueConnectionLink(connection.name)}/mappings`;
 const d=discovery?.discovery;const picked=d?.transitions.find(t=>t.id===transition);
 const qualified=connection.enabled&&connection.projectRestricted&&connection.credentialConfigured;
 const wrongConnection=!!saved.data&&saved.data.connection!==connection.name;
 const inspect=async()=>{
  if(busy||!qualified||wrongConnection)return;
  if(!/^[A-Z][A-Z0-9_]{0,63}-[1-9][0-9]{0,9}$/.test(sample.trim())){setError('Enter an issue key such as APP-7.');return;}
  setBusy(true);setError(undefined);setDiscovery(undefined);setTransition('');
  try{setDiscovery(await api.discoverIssueTransitions(connection.name,connection.revision,sample.trim()));}
  catch(e){setError(errorMessage(e));}finally{setBusy(false);}
 };
 const save=async()=>{
  if(role!=='admin'||busy||conflict||wrongConnection||!d||!picked||!canMapTransition(picked,d.current.id)||(mappingName&&!saved.data))return;
  const chosenName=mappingName??name.trim();
  if(!/^[a-z][a-z0-9_-]{0,63}$/.test(chosenName)){setError('Choose a lowercase mapping name beginning with a letter, up to 64 characters.');return;}
  const hash=location.hash;setBusy(true);setError(undefined);
  try{await api.saveIssueWorkflowMapping({name:chosenName,connection:connection.name,connectionRevision:discovery!.revision,sampleIssueKey:d.issueKey,sampleIssueId:d.issueId,issueTypeId:d.issueTypeId,fromStatusId:d.current.id,transitionId:picked.id,targetStatusId:picked.to.id,issueUpdated:d.updated,enabled},saved.data?.revision);if(location.hash===hash)location.hash=base;}
  catch(e){setError(errorMessage(e));setConflict(e instanceof ApiError&&e.status===409);}finally{setBusy(false);}
 };
 return <><p><a href={base}>Back to mappings</a></p>
 {saved.loading&&mappingName&&<p role="status">Loading saved mapping…</p>}<ErrorBox error={saved.error}/>
 {saved.error&&<Btn onClick={()=>void saved.reload()}>Retry loading mapping</Btn>}
 {saved.data&&<aside className="card"><h2>{saved.data.name} · Revision {saved.data.revision}</h2><p>Type {saved.data.issueTypeId} · Status {saved.data.fromStatusId} → {saved.data.targetStatusId} · Transition {saved.data.transitionId}</p><p className="muted small">Saved sample {saved.data.sampleIssueKey}. Rediscover a sample in the intended source state before enabling or replacing this definition.</p></aside>}
 {wrongConnection&&<p role="alert">This mapping belongs to another connection. Open it from its own connection.</p>}
 {role!=='admin'?<p>Only administrators can configure mappings.</p>:(!mappingName||saved.data)&&!saved.error&&!wrongConnection&&<form className="card form" aria-label="Workflow mapping configuration" onSubmit={e=>{e.preventDefault();void save();}}>
 <ErrorBox error={error}/>
 {!qualified&&<p role="note">Enable the connection and review its project-restricted technical account before configuring a mapping.</p>}
 {conflict&&<div><p>The mapping, sample issue or connection changed. Reload this page and discover again before saving.</p><Btn onClick={()=>location.reload()}>Reload saved configuration</Btn></div>}
 <fieldset disabled={busy||conflict||!qualified}>
 <HelpField label="Mapping name" help="Stable reference selected explicitly by a workflow. Lowercase letters, digits, underscores and hyphens; begin with a letter. This name is not a Jira status or board column."><input value={mappingName??name} disabled={!!mappingName} onChange={e=>setName(e.target.value)} maxLength={64}/></HelpField>
 <HelpField label="Sample issue key" help="Choose an issue of the intended type in the intended source status. Jira availability is checked for that actual issue and account; each future use must revalidate its own issue."><input placeholder={saved.data?.sampleIssueKey??'APP-7'} value={sample} onChange={e=>{setSample(e.target.value);setDiscovery(undefined);setTransition('');}} maxLength={75}/></HelpField>
 <div className="btn-row"><Btn onClick={()=>void inspect()}>{busy?'Working…':'Discover available transitions'}</Btn></div>
 {d&&<>{!d.transitions.length&&<p>No transitions are available for this sample.</p>}<p>Sample {d.issueKey} · Type {d.issueTypeId} · Current status {d.current.name} ({d.current.id})</p>
 <HelpField label="Transition" help="Choose a discovered transition ID and its destination status ID. Equal names can identify different transitions. This initial profile supports transitions without required field updates and excludes same-status actions."><select value={transition} onChange={e=>setTransition(e.target.value)}><option value="">Select transition</option>{d.transitions.map(t=><option key={t.id} value={t.id} disabled={!canMapTransition(t,d.current.id)}>{t.name} ({t.id}) → {t.to.name} ({t.to.id}){canMapTransition(t,d.current.id)?'':' · unsupported fields or same status'}</option>)}</select></HelpField>
 <HelpField label="Mapping enabled" help="An enabled definition can be selected by a workflow. Disabling does not erase historical snapshots or change Jira. Workflow attachment and execution still require separate reviewed consent."><input type="checkbox" checked={enabled} onChange={e=>setEnabled(e.target.checked)}/></HelpField>
 <div className="btn-row"><Btn type="submit" tone="primary" disabled={!picked||!canMapTransition(picked,d.current.id)}>Save mapping</Btn></div></>}
 </fieldset>
 <p className="muted small">Saving rechecks the sample and records a definition. It does not start a task, execute a transition or attach the mapping automatically.</p>
 </form>}
 </>;
}
