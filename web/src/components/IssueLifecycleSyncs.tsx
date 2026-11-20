import {useState} from 'react';
import {api,ApiError,errorMessage} from '../api/client';
import type {LifecycleSync,WorkflowEdition} from '../api/types';
import {useAsync} from '../hooks/useData';
import {Badge,Btn,ErrorBox,fmtDateTime} from './common';
import {HelpField} from './FieldHelp';

export function IssueLifecycleSyncs({connection,role,edition='jira-dc'}:{connection:string;role?:string;edition?:WorkflowEdition}){
 const [after,setAfter]=useState<string>();const [previous,setPrevious]=useState<(string|undefined)[]>([]);
 const page=useAsync(()=>api.lifecycleSyncs(connection,after,edition==='jira'?'jira':undefined),[connection,after,edition]);
 const [query,setQuery]=useState('');const [status,setStatus]=useState('');
 const [busy,setBusy]=useState<string>();const [error,setError]=useState<string>();const [conflict,setConflict]=useState(false);
 const [message,setMessage]=useState<string>();
 const rows=page.data?.items.filter(v=>(!status||v.state===status)&&`${v.issueKey} ${v.taskId} ${v.mapping.name} ${v.stage}`.toLowerCase().includes(query.toLowerCase()))??[];
 const recover=async(v:LifecycleSync,retry:boolean)=>{
  if(busy||conflict)return;setBusy(v.id);setError(undefined);setMessage(undefined);
  try{
   const out=retry?await api.retryLifecycleSync(v.id,v.revision):await api.reconcileLifecycleSync(v.id,v.revision);
   setMessage(retry?'Preflight retry queued. Task and PR delivery are unchanged.':out.state==='unknown'?'The outcome remains uncertain. No transition was repeated.':'The retained synchronization evidence was updated.');
   await page.reload();
  }catch(e){setError(errorMessage(e));setConflict(e instanceof ApiError&&e.status===409);}finally{setBusy(undefined);}
 };
 const refresh=async()=>{await page.reload();setConflict(false);setError(undefined);};
 return <section className="card connections-panel" aria-label="Tracker synchronization">
 <header className="connections-panel-head"><div><h2>Tracker synchronization</h2><p className="muted small">Optional Jira updates have their own history. These outcomes do not change a delivered task or pull request.</p></div><Btn disabled={!!busy||page.loading} onClick={()=>void refresh()}>Refresh synchronization</Btn></header>
 <div className="btn-row">
 <HelpField label="Search synchronization" help="Filter this page by issue, task, mapping or lifecycle stage."><input type="search" value={query} onChange={e=>setQuery(e.target.value)}/></HelpField>
 <HelpField label="Synchronization status" help="Target observed and already at target describe retained evidence, not attribution or a guarantee of the issue’s current status. Unknown outcomes must be reconciled by reading before another action."><select value={status} onChange={e=>setStatus(e.target.value)}><option value="">All statuses</option>{['queued','failed','unknown','already_target','target_observed','superseded'].map(s=><option key={s} value={s}>{s.replaceAll('_',' ')}</option>)}</select></HelpField>
 </div>
 <ErrorBox error={page.error||error}/>{message&&<p role="status">{message}</p>}
 {page.loading&&<p role="status">Loading synchronization history…</p>}
 {conflict&&<p role="alert">The synchronization changed. Refresh and review its latest outcome before another recovery action.</p>}
 {!page.loading&&!page.error&&!rows.length&&<p className="empty">No matching synchronizations on this page.</p>}
 {!!rows.length&&!page.loading&&!page.error&&<div className="connection-table connections-table" role="region" aria-label="Synchronization table" tabIndex={0}><table><thead><tr><th scope="col">Issue and task</th><th scope="col">Milestone</th><th scope="col">Mapping</th><th scope="col">Outcome</th><th scope="col">Updated</th><th scope="col">Recovery</th></tr></thead><tbody>{rows.map(v=><tr key={v.id}>
 <td><strong>{v.issueKey}</strong><span className="connection-meta small"><a href={`#/tasks/${encodeURIComponent(v.taskId)}`}>{v.taskId}</a></span></td>
 <td>{v.stage==='work_started'?'Runtime admission':'PR delivered'}</td>
 <td>{v.mapping.name}<span className="connection-meta small muted">Revision {v.mapping.revision} · Type {v.issueTypeId}</span></td>
 <td><Badge tone={v.state==='failed'?'bad':v.state==='unknown'||v.state==='queued'?'warn':v.state==='target_observed'||v.state==='already_target'?'ok':undefined}>{v.state.replaceAll('_',' ')}</Badge>{v.error&&<p className="small">{v.error}</p>}</td>
 <td><time dateTime={v.updatedAt}>{fmtDateTime(v.updatedAt)}</time></td>
 <td><LifecycleRecovery value={v} role={role} busy={busy} conflict={conflict} onRecover={recover}/></td>
 </tr>)}</tbody></table></div>}
 <footer className="btn-row"><Btn disabled={!previous.length||!!busy||page.loading} onClick={()=>{setAfter(previous.at(-1));setPrevious(p=>p.slice(0,-1));}}>Previous page</Btn><Btn disabled={!page.data?.next||!!busy||page.loading} onClick={()=>{setPrevious(p=>[...p,after]);setAfter(page.data!.next);}}>Next page</Btn><span className="muted small">Filters apply to this page. Use the page controls for more history.</span></footer>
 </section>;
}

function LifecycleRecovery({value:v,role,busy,conflict,onRecover}:{value:LifecycleSync;role?:string;busy?:string;conflict:boolean;onRecover:(v:LifecycleSync,retry:boolean)=>Promise<void>}){
 const [open,setOpen]=useState(false);
 const evidence=useAsync(()=>open?api.lifecycleSyncEvidence(v.id):Promise.resolve(undefined),[open,v.id,v.revision]);
 const intent=evidence.data?.transition?.intent;
 const changed=!!evidence.data&&evidence.data.sync.revision!==v.revision;
 const issueUrl=v.source.kind!=='jira'&&v.source.siteUrl?`${v.source.siteUrl}/browse/${encodeURIComponent(v.issueKey)}`:undefined;
 return <details onToggle={e=>setOpen(e.currentTarget.open)}><summary>Review recovery</summary>
 {issueUrl&&<p><a href={issueUrl} target="_blank" rel="noreferrer">Open {v.issueKey} in Jira</a></p>}
 <p className="small">Issue ID {v.issueId} · Connection revision {v.connectionRevision}</p>
 <p className="small">Evidence: <code>{v.evidence}</code></p><p className="small">Trigger: <code>{v.triggerId}</code></p>
 {evidence.loading&&open&&<p role="status">Loading retained transition intent…</p>}
 <ErrorBox error={evidence.error}/>{evidence.error&&<Btn small onClick={()=>void evidence.reload()}>Reload retained intent</Btn>}
 {intent&&!evidence.loading&&!evidence.error&&<p>Intended status {intent.fromStatusId} → {intent.targetStatusId} · Transition ID {intent.transitionId} · Mapping {intent.mappingName} revision {intent.mappingRevision}</p>}
 {open&&!evidence.loading&&!evidence.error&&!intent&&<p>No dispatch intent was retained. The original mapping may have changed before dispatch; review this operation manually. Its destination is not inferred from the current definition.</p>}
 {changed&&<p role="alert">This outcome has changed. Refresh synchronization before recovery.</p>}
 {(v.state==='unknown'||v.state==='queued')&&<p className="small">Read the retained outcome. An uncertain write cannot be retried.</p>}
 {v.state==='failed'&&<p className="small">Retry only a failure confirmed before dispatch. The original consent, connection and mapping must still match.</p>}
 {(role==='admin'||role==='operator')&&(v.state==='unknown'||v.state==='queued')&&<Btn small disabled={!!busy||conflict||changed||evidence.loading||!!evidence.error||!intent} onClick={()=>void onRecover(v,false)}>{busy===v.id?'Checking…':'Reconcile by reading'}</Btn>}
 {role==='admin'&&v.state==='failed'&&<Btn small disabled={!!busy||conflict||changed||evidence.loading||!!evidence.error} onClick={()=>void onRecover(v,true)}>{busy===v.id?'Queuing…':'Retry preflight'}</Btn>}
 </details>;
}
