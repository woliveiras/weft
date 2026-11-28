import {workflowAPI} from './workflowEdition';
import {useState} from 'react';
import {errorMessage} from '../api/client';
import type {IssueTransition, IssueTransitionDiscovery, WorkflowEdition} from '../api/types';
import {useAsync} from '../hooks/useData';
import {Badge, Btn, ErrorBox, fmtDateTime} from './common';
import {HelpField} from './FieldHelp';

export function transitionRequirements(t: IssueTransition): string {
  if (t.fields === null) return 'Field metadata unavailable';
  const required = Object.entries(t.fields).filter(([,field]) => field.required);
  return required.length ? `${required.length} required field${required.length === 1 ? '' : 's'}` : 'No required fields reported';
}
export function IssueTransitionInspector({name, role, edition='jira-dc'}: {name: string; role?: string; edition?:WorkflowEdition}) {
  const workflow=workflowAPI(edition);
  const connection=useAsync(()=>workflow.connection(name),[name,edition]);
  const [issue,setIssue]=useState('');
  const [busy,setBusy]=useState(false);
  const [result,setResult]=useState<IssueTransitionDiscovery>();
  const [error,setError]=useState<string>();
  const [page,setPage]=useState(0);
  const allowed=role==='admin'||role==='operator';
  const inspect=async()=>{
    if(!allowed||busy||connection.loading||connection.error||!connection.data?.enabled)return;
    if(!/^[A-Z][A-Z0-9_]{0,63}-[1-9][0-9]{0,9}$/.test(issue.trim())){setError('Enter an issue key such as APP-7.');return;}
    setBusy(true);setError(undefined);setResult(undefined);setPage(0);
    try{setResult(await workflow.discover(name,connection.data.revision,issue.trim()));}
    catch(e){setError(errorMessage(e));}finally{setBusy(false);}
  };
  const d=result?.revision===connection.data?.revision?result?.discovery:undefined;
  const pages=Math.max(1,Math.ceil((d?.transitions.length??0)/20));
  const current=Math.min(page,pages-1);
  return <section>
    <header className="page-head connection-detail-head"><div><a href="#/connections">Connections</a><h1>Inspect Jira transitions</h1><p className="muted">Read the transitions available for one issue in its current state.</p></div>
      <Btn disabled={busy||connection.loading} onClick={()=>{setResult(undefined);setError(undefined);void connection.reload();}}>Reload connection</Btn>
    </header>
    <ErrorBox error={connection.error}/>
    {connection.loading&&<p role="status">Loading issue connection…</p>}
    {connection.data&&<p>{name} · {connection.data.siteUrl} · Project ID {connection.data.projectId} · Revision {connection.data.revision}</p>}
    {!allowed?<p>An operator or administrator must request discovery.</p>:<form className="card form" aria-label="Inspect issue transitions" onSubmit={event=>{event.preventDefault();void inspect();}}>
      <fieldset disabled={busy||connection.loading||!!connection.error||!connection.data?.enabled}>
        <HelpField label="Issue key" help="Use an existing issue in the connection's project. Available transitions depend on this issue's type, current status, workflow and the technical account's permissions. Board column names are not transition IDs."><input value={issue} maxLength={75} placeholder="APP-7" onChange={event=>{setIssue(event.target.value);setResult(undefined);setError(undefined);}}/></HelpField>
        <div className="btn-row"><Btn type="submit" tone="primary">{busy?'Discovering…':'Discover transitions'}</Btn></div>
      </fieldset>
      {connection.data&&!connection.data.enabled&&<p role="note">Enable this connection before discovering transitions.</p>}
      <p className="muted small">Discovery only reads Jira. It does not change the issue, save a lifecycle mapping or grant tools. Results are specific to this issue and current connection revision.</p>
      {busy&&<p role="status">Reading the issue and checking its available transitions…</p>}
      <ErrorBox error={error}/>
    </form>}
    {d&&<section className="card connections-panel" aria-label="Discovered transitions">
      <header><h2>{d.issueKey} · {d.current.name}</h2><p className="muted small">Issue ID {d.issueId} · Type ID {d.issueTypeId} · Current status ID {d.current.id} · Issue updated <time dateTime={d.updated}>{fmtDateTime(d.updated)}</time></p></header>
      <p>Names are labels for people. IDs identify the transition and destination; equal names may refer to different statuses.</p>
      {d.transitions.length?<div className="connection-table connections-table" role="region" aria-label="Transition table" tabIndex={0}><table>
        <thead><tr><th scope="col">Transition</th><th scope="col">Destination status</th><th scope="col">Fields</th></tr></thead>
        <tbody>{d.transitions.slice(current*20,(current+1)*20).map(t=><tr key={t.id}>
          <td><strong>{t.name}</strong><span className="connection-meta small muted">Transition ID {t.id}</span></td>
          <td>{t.to.name}<span className="connection-meta small muted">Status ID {t.to.id}</span></td>
          <td><Badge tone={t.fields===null?'warn':undefined}>{transitionRequirements(t)}</Badge>{t.fields!==null&&Object.keys(t.fields).length>0&&<details><summary>Field requirements</summary><ul>{Object.entries(t.fields).map(([id,f])=><li key={id}><strong>{f.name||id}</strong> ({id}) · {f.required?'Required':'Optional'} · {f.schema.type}{f.schema.items?` of ${f.schema.items}`:''}{f.hasDefaultValue?' · Default reported':''}{f.allowedValues?` · ${f.allowedValues.length} listed choices`:''}</li>)}</ul></details>}</td>
        </tr>)}</tbody></table></div>:<p className="empty">No transitions are available for this issue and account in the current state.</p>}
      {pages>1&&<nav className="btn-row" aria-label="Transition pages"><Btn disabled={current===0} onClick={()=>setPage(current-1)}>Previous</Btn><span>Page {current+1} of {pages}</span><Btn disabled={current===pages-1} onClick={()=>setPage(current+1)}>Next</Btn></nav>}
    </section>}
  </section>;
}
