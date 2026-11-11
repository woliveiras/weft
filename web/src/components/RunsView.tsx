import { ExecutionTreeView } from './ExecutionTreeView';
import { useEffect, useState } from 'react';
import { api } from '../api/client';
import type { Execution } from '../api/types';
import { useAsync } from '../hooks/useData';
import { Badge, Btn, ErrorBox, JsonView, fmtDateTime } from './common';
import { ApprovalCard } from './ApprovalsView';
import { belongsToExecution, executionProgress } from './activityPresentation';
import { describeEvent } from './describe';
import { blockedToolCalls, matchingRuns, runEvents, runLink, runSections, runSectionTitles, type RunSection } from './runPresentation';

function useRunData<T>(load:()=>Promise<T>) {
 const result=useAsync(load,[]);
 useEffect(()=>{
  if (result.loading) return;
  const timer=setTimeout(()=>void result.reload(),5000);
  return()=>clearTimeout(timer);
 },[result.loading,result.reload]);
 return result;
}
export function RunsView({graphId,executionId,section='result',role}:{graphId:string;executionId?:string;section?:RunSection;role?:string}) {
 return executionId ? <RunDetail key={`${graphId}/${executionId}`} graphId={graphId} executionId={executionId} section={section} role={role}/> : <RunsList key={graphId} graphId={graphId}/>;
}
function RunsList({graphId}:{graphId:string}) {
 const list=useRunData(()=>api.executions(graphId,{limit:100}));
 const [query,setQuery]=useState('');const [status,setStatus]=useState('');
 const rows=matchingRuns(list.data??[],graphId,query,status);
 return <section className="page fill" aria-label="Runs"><div className="page-head"><div><h1>Runs</h1><p className="muted">Recent execution records for this graph. Up to 100 records, refreshed every five seconds; this is not the complete history.</p></div><Btn disabled={list.loading} onClick={()=>void list.reload()}>Refresh runs</Btn></div>
  <div className="connection-filters"><label>Search runs<input type="search" value={query} onChange={e=>setQuery(e.target.value)} placeholder="Agent, execution ID or result"/></label><label>Run status<select value={status} onChange={e=>setStatus(e.target.value)}><option value="">All statuses</option>{['pending','running','awaiting_approval','succeeded','failed','cancelled','interrupted'].map(s=><option key={s} value={s}>{s}</option>)}</select></label></div>
  <ErrorBox error={list.error}/>{list.loading&&<p role="status">Refreshing runs…</p>}
  {!list.error&&<>{!list.loading&&!rows.length&&<p>{list.data?.length?'No runs match these filters.':'No executions recorded yet.'}</p>}
  {!!rows.length&&<div className="connection-table"><table><caption className="sr-only">Recent runs</caption><thead><tr><th>Execution</th><th>Agent</th><th>Status</th><th>Started</th><th>Result</th></tr></thead><tbody>{rows.map(r=><tr key={r.id}><td><a href={runLink(graphId,r.id)}>{r.id}</a></td><td>{r.nodeId}{r.parentExecutionId && <span className="muted"> · Delegated analysis</span>}</td><td><Badge>{executionProgress(r)}</Badge></td><td>{fmtDateTime(r.startedAt)}</td><td>{r.summary||'No result summary recorded.'}</td></tr>)}</tbody></table></div>}</>}
 </section>;
}
function RunDetail({graphId,executionId,section,role}:{graphId:string;executionId:string;section:RunSection;role?:string}) {
 const run=useRunData(()=>api.execution(executionId));
 const execution=run.data;
 const valid=execution?.graphId===graphId&&execution.id===executionId;
 return <section className="page fill run-detail" aria-label="Run detail">
  <p><a href={runLink(graphId)}>Runs</a> / Execution</p><Btn disabled={run.loading} onClick={()=>void run.reload()}>Refresh run</Btn>
  <ErrorBox error={run.error}/>{run.loading&&<p role="status">Refreshing run…</p>}
  {!run.error&&!run.loading&&!execution&&<p role="alert">No execution record returned.</p>}
  {!run.error&&execution&&!valid&&<p role="alert">This execution does not belong to the selected graph.</p>}
  {!run.error&&valid&&<><header className="page-head"><div><h1>{execution.nodeId}</h1><p><Badge>{executionProgress(execution)}</Badge> · Graph version {execution.graphVersion} · {fmtDateTime(execution.startedAt)}</p><p className="muted">Execution outcome and individual tool outcomes may differ.</p></div></header>
   {execution.parentExecutionId && <aside className="card" aria-label="Delegation context"><strong>Delegated analysis</strong><p>Template: {execution.delegationTemplate}. {execution.delegationAudit?.effectiveGrants.length ? "Tools restricted to inherited capabilities; see the audit snapshot." : "No tools or workspace access."} Reported child usage is included in the parent total; do not add both totals.</p>{execution.delegationConfig && <p>Policy revision {execution.delegationConfig.policy.revision} · Profile revision {execution.delegationConfig.profile.revision} · Approval mode: {execution.delegationConfig.policy.policy.approvalMode}</p>}<details><summary>Delegation audit</summary><p>Objective: {execution.delegationObjective || 'Not recorded'}</p><p>Model: {execution.delegationModel ? `${execution.delegationModel.provider}/${execution.delegationModel.name}` : 'Not recorded'}</p>{execution.delegationAudit ? <><p>Decision: {execution.delegationAudit.decision}{execution.delegationAudit.decidedBy && ` · ${execution.delegationAudit.decidedBy}`}</p><p>Decision time: {execution.delegationAudit.decidedAt ? fmtDateTime(execution.delegationAudit.decidedAt) : 'Not recorded'}</p><p>Runtime: {execution.delegationAudit.runtime} · Output limit: {execution.delegationAudit.maxOutputTokens} tokens</p><p>Effective grants: {execution.delegationAudit.effectiveGrants.length ? execution.delegationAudit.effectiveGrants.map(g=>g.name).join(', ') : 'None'}</p><p>Workspace access: {execution.delegationAudit.workspaceAccess ? 'Yes' : 'No'} · Recursive access: {execution.delegationAudit.recursiveAccess ? 'Yes' : 'No'}</p></> : <p>Access and decision snapshot not recorded for this historical execution.</p>}<p>These records describe this execution, not the current agent configuration.</p></details><a href={runLink(graphId,execution.parentExecutionId)}>View parent execution</a></aside>}
   <nav className="section-navigation" aria-label="Run sections">{runSections.map(s=><a key={s} href={runLink(graphId,executionId,s)} aria-current={section===s?'page':undefined}>{runSectionTitles[s]}</a>)}</nav>
   {section==='result'&&<><article className="card"><h2>Result summary</h2><p className="muted">Agent-generated summary. Inspect tool outcomes below to verify it.</p>{execution.summary?<p className="run-summary">{execution.summary}</p>:<p>No result summary recorded.</p>}<ErrorBox error={execution.error}/><p>{execution.toolCalls} tool attempts recorded · {execution.usage?.inputTokens??0} input / {execution.usage?.outputTokens??0} output tokens</p><details><summary>About tool counts</summary><p>The recorded total counts attempts, including failures. Historical native runs may have counted gateway calls twice; records are preserved. The observed history below is a bounded window and may omit attempts.</p></details><p><a href={`#/g/${encodeURIComponent(graphId)}/files`}>Inspect graph workspace</a></p><p className="muted">Workspace files reflect the current graph workspace, not a snapshot attributed to this run.</p></article><RunEvents execution={execution} toolsOnly summaryOnly/><RunApprovals key={executionId} execution={execution} role={role}/></>}
   {(section==='timeline'||section==='tools')&&<RunEvents key={`${executionId}/${section}`} execution={execution} toolsOnly={section==='tools'}/>}
   {section==='tree'&&<ExecutionTreeView key={executionId} graphId={graphId} executionId={executionId} role={role}/>}
   {section==='messages'&&<RunMessages key={executionId} execution={execution}/>}
   {section==='diagnostics'&&<article className="card"><h2>Diagnostics</h2><dl className="kv"><dt>Execution</dt><dd>{execution.id}</dd><dt>Run group</dt><dd>{execution.runId}</dd><dt>Agent</dt><dd>{execution.nodeId}</dd><dt>Replica</dt><dd>{execution.replica}</dd><dt>Iteration</dt><dd>{execution.iteration}</dd><dt>Completed</dt><dd>{execution.completedAt?fmtDateTime(execution.completedAt):'Not recorded'}</dd><dt>Trace</dt><dd>{execution.traceId||'Not recorded'}</dd><dt>Signal</dt><dd>{execution.signal||'Not recorded'}</dd></dl><a href={`#/g/${encodeURIComponent(graphId)}/agent?agent=${encodeURIComponent(execution.nodeId)}&section=general`}>View current agent configuration</a><p className="muted">Current configuration may differ from the version used by this execution.</p></article>}
  </>}
 </section>;
}
function RunApprovals({execution,role}:{execution:Execution;role?:string}) {
 const approvals=useRunData(()=>api.approvals({graph:execution.graphId}));
 const records=(approvals.data??[]).filter(a=>a.graphId===execution.graphId&&a.executionId===execution.id);
 return <section className="card" aria-label="Run approvals"><h2>Approvals</h2><ErrorBox error={approvals.error}/><Btn disabled={approvals.loading} onClick={()=>void approvals.reload()}>Refresh run approvals</Btn>
 {!approvals.error&&<>{records.map(a=><ApprovalCard key={a.id} a={a} canDecide={(role==='admin'||role==='operator')&&(a.kind!=='environment'||role==='admin')} onDone={()=>void approvals.reload()}/>)}{!approvals.loading&&!records.length&&<p>No approvals linked to this execution.</p>}</>}
 </section>;
}
export function RunEvents({execution,toolsOnly,summaryOnly=false}:{execution:Execution;toolsOnly:boolean;summaryOnly?:boolean}) {
 const events=useRunData(()=>api.events(execution.graphId,{node:execution.nodeId,limit:300}));
 const linked=runEvents(events.data??[],execution);const blocked=blockedToolCalls(linked);
 const invoked=linked.filter(e=>e.type==='tool.invoked').length;
 const visible=toolsOnly?linked.filter(e=>e.type.startsWith('tool.')):linked;
 return <section className="card" aria-label={summaryOnly?'Run tool outcomes':toolsOnly?'Run tool calls':'Run timeline'}><h2>{summaryOnly?'Tool outcomes':toolsOnly?'Tool calls':'Timeline'}</h2><p className="muted">Linked records within the latest 300 agent events. Missing entries do not prove that no activity occurred.</p><Btn disabled={events.loading} onClick={()=>void events.reload()}>Refresh run events</Btn><ErrorBox error={events.error}/>
 {!events.error&&<><p>{invoked} tool invocations observed · {linked.filter(e=>e.type.startsWith('tool.')).length} tool lifecycle events in this window. These are not additional calls.</p>{blocked>0&&<p role="note">{blocked} blocked tool calls in this window. The execution status remains {executionProgress(execution)}.</p>}{summaryOnly?<a href={runLink(execution.graphId,execution.id,'tools')}>Inspect tool calls</a>:<ol>{visible.map(e=><li key={e.id}><time>{fmtDateTime(e.time)}</time> · <strong>{e.type}</strong><p>{describeEvent(e)}</p></li>)}</ol>}{!events.loading&&!visible.length&&<p>No linked events in this recent window.</p>}</>}
 </section>;
}
function RunMessages({execution}:{execution:Execution}) {
 const messages=useRunData(()=>api.messages(execution.graphId,{limit:100}));
 const linked=(messages.data??[]).filter(m=>belongsToExecution(m,execution));
 return <section className="card" aria-label="Run messages"><h2>Messages</h2><p className="muted">Only explicit execution/input/output IDs link messages. Showing matches within the latest 100 graph messages.</p><Btn disabled={messages.loading} onClick={()=>void messages.reload()}>Refresh run messages</Btn><ErrorBox error={messages.error}/>
 {!messages.error&&<>{linked.map((m,i)=><article key={`${m.message.id}/${m.channel}/${i}`}><h3>{m.message.type}</h3><p>{m.status} · {m.channel} · {fmtDateTime(m.createdAt)}</p><ErrorBox error={m.error}/><JsonView value={m.message.payload} label="Message content"/></article>)}{!messages.loading&&!linked.length&&<p>No linked messages in this recent window.</p>}</>}
 </section>;
}
