import {useEffect, useRef, useState} from 'react';
import {api, errorMessage} from '../api/client';
import type {TaskDraft, TaskDelivery, TaskProgress} from '../api/tasks';
import type {Execution} from '../api/types';
import {useAsync, useThrottled} from '../hooks/useData';
import {useOptionalEventStream} from '../hooks/useEventStream';
import {Badge, Btn, ErrorBox, fmtDateTime, since} from './common';
import {TaskRetry, TaskRetryOrigin} from './TaskRetry';
import {TaskJourney} from './TaskJourney';
import {TaskAuthorization} from './TaskAuthorization';
import {TaskDecisions} from './TaskDecisions';
import {TaskActivity} from './TaskActivity';
import {TaskCleanup} from './TaskCleanup';
import {TaskEnvironment} from './TaskEnvironment';
import {TaskEvidence} from './TaskEvidence';
import {TaskPlan} from './TaskPlan';
import {TaskCheckpoints} from './TaskCheckpoints';
import {WorkspaceHandoff} from './WorkspaceHandoff';
import {BudgetBar} from './CostsSection';
import {money} from './cost';
import {taskExecutionWindow, taskSection, taskSectionLink, taskSections, type TaskSection} from './taskDetailPresentation';

function TaskRequestContent({request}: {request: string}) {
  let issue: {key: string; body: string; replies: string[]} | undefined;
  try {
    const parsed = JSON.parse(request);
    if (parsed && typeof parsed.notice === 'string' && typeof parsed.issue === 'string'
      && typeof parsed.title === 'string' && typeof parsed.body === 'string') {
      issue = {key: parsed.issue, body: parsed.body, replies: Array.isArray(parsed.replies)
        ? parsed.replies.filter((reply: unknown): reply is string => typeof reply === 'string') : []};
    }
  } catch {
    // Ordinary task requests are plain text; content never becomes executable HTML.
  }
  return <>
    {issue && <p className="muted">Issue <code>{issue.key}</code></p>}
    <p className="task-request-text">{issue ? (issue.body || 'No description was provided for this issue.') : request}</p>
    {!!issue?.replies.length && <details><summary>Issue replies ({issue.replies.length})</summary>
      <ul>{issue.replies.map((reply, index) => <li className="task-request-text" key={index}>{reply}</li>)}</ul>
    </details>}
  </>;
}

export function TaskDeliveryView({delivery}: {delivery: TaskDelivery}) {
  return <section className="task-delivery" aria-label="Source control delivery">
    <h3>Source control</h3>
    <p>{delivery.branch ? <>Pushed <code>{delivery.branch}</code> to {delivery.repository} ({delivery.connection}).</> : `Nothing pushed to ${delivery.connection} yet.`}</p>
    {delivery.pullRequestUrl && <p>Recorded pull request <a href={delivery.pullRequestUrl} target="_blank" rel="noreferrer">#{delivery.pullRequestNumber}</a>.</p>}
    <p className="muted small">Delivery recorded by Weft. The current remote state has not been refreshed here.</p>
    {!!delivery.failures?.length && <ul role="alert" aria-label="Delivery failures">{delivery.failures.map((failure, index) => <li key={index}>{failure}</li>)}</ul>}
  </section>;
}

function TaskResult({result, delivery, changesHref}: {result?: Execution; delivery?: TaskDelivery; changesHref: string}) {
  const [expanded, setExpanded] = useState(false);
  const full = useAsync(async () => {
    if (!expanded || !result) return undefined;
    const recorded = await api.execution(result.id);
    if (recorded.id !== result.id || recorded.graphId !== result.graphId) throw new Error('The result response does not belong to this execution.');
    return recorded;
  }, [expanded, result?.id, result?.graphId]);
  const summary = expanded && full.data ? full.data.summary : result?.summary;
  return <section className="card task-result" aria-label="Result">
    <div className="task-detail-heading"><h2>Result</h2><a href={changesHref}>Review changes &amp; checks</a></div>
    <p className={`task-request-text${!expanded && (summary?.length ?? 0) > 280 ? ' task-result-preview' : ''}`} aria-label="Agent summary">{summary || 'No result recorded yet.'}</p>
    {result?.id && <Btn small aria-expanded={expanded} onClick={() => setExpanded(value => !value)}>{expanded ? 'Show less' : 'Read full result'}</Btn>}
    {expanded && full.loading && <p role="status">Loading full recorded result…</p>}
    {expanded && full.error && <><ErrorBox error={`Full result unavailable: ${full.error}`}/><Btn small onClick={() => void full.reload()}>Retry full result</Btn></>}
    {result?.summary && <p className="muted small">Agent summary. Verify the recorded commands and outcomes in Changes &amp; checks.</p>}
    {delivery && <TaskDeliveryView delivery={delivery}/>}
  </section>;
}

export function TaskTracking({task, canControl, canDecide, canRetry = false, nativeProfile}: {task: TaskDraft; canControl: boolean; canDecide: boolean; canRetry?: boolean; nativeProfile: boolean}) {
  const [progress, setProgress] = useState<TaskProgress>();
  const [updated, setUpdated] = useState<string>();
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [confirmed, setConfirmed] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const manualRefresh = useRef(false);
  const [refreshing, setRefreshing] = useState(false);
  const [refreshFeedback, setRefreshFeedback] = useState('');
  const [section, setSection] = useState<TaskSection>(() => taskSection(location.hash, task.id));
  useEffect(() => {
    const change = () => setSection(taskSection(location.hash, task.id));
    change(); window.addEventListener('hashchange', change);
    return () => window.removeEventListener('hashchange', change);
  }, [task.id]);
  useEffect(() => {
    let disposed = false; let timer: ReturnType<typeof setTimeout>;
    const load = async (first = false) => {
      const report = first && manualRefresh.current;
      try {
        const result = await api.taskProgress(task.id);
        if (result.task.id !== task.id) throw new Error('The progress response does not belong to this task.');
        if (!disposed) {
          const checkedAt = new Date().toISOString();
          setProgress(result); setUpdated(checkedAt); setError(undefined);
          if (report) setRefreshFeedback(`Task snapshot refreshed at ${fmtDateTime(checkedAt)}.`);
          else setRefreshFeedback(previous => previous.startsWith('Task refresh failed.') ? 'Task updates resumed.' : previous);
        }
      } catch (cause) {if (!disposed) {setError(errorMessage(cause)); if (report) setRefreshFeedback('Task refresh failed. The last recorded snapshot is still shown. Retry Refresh task.');}}
      finally {if (!disposed && report) {manualRefresh.current = false; setRefreshing(false);}}
      if (!disposed) timer = setTimeout(() => void load(), 3000);
    };
    void load(true); return () => {disposed = true; clearTimeout(timer);};
  }, [task.id, refresh]);
  const cancel = async () => {
    setBusy(true);
    try {await api.cancelTask(task.id); setRefresh(x => x + 1); setConfirmed(false);}
    catch (cause) {setError(errorMessage(cause));} finally {setBusy(false);}
  };
  const retryDelivery=async(alias:string)=>{
    if(!progress)return;
    setBusy(true);setError(undefined);
    try{await api.retryRepositoryDelivery(task.id,progress.task.revision,alias);setRefresh(x=>x+1);}
    catch(e){setError(errorMessage(e));}finally{setBusy(false);}
  };
  const current = progress?.task ?? task;
  const graph = current.executionGraphId;
  const budget = useAsync(() => graph ? api.budget('run', graph) : Promise.resolve(undefined), [graph]);
  const reloadBudget = useThrottled(() => void budget.reload(), 300);
  useOptionalEventStream({graph, types: ['budget.raised', 'budget.graph_transition', 'budget.ledger_invalid']}, reloadBudget);
  const active = !!progress && ['delivery_pending','running', 'starting', 'environment_pending', 'environment_build_requested', 'preparing', 'blocked', 'interrupted'].includes(progress.state);
  const terminal = !!progress && ['succeeded', 'failed'].includes(progress.state);
  const retainedCopy = useAsync(async () => {
    if (!canControl || !terminal || !graph) return undefined;
    const status = await api.status(graph);
    if (status.graphId !== graph) throw new Error('The status response does not belong to this task copy.');
    return status;
  }, [canControl, terminal, graph, refresh]);
  const stoppedCopy = !retainedCopy.error && retainedCopy.data?.graphId === graph && retainedCopy.data?.state === 'stopped' && retainedCopy.data.desiredState === 'stopped';
  const canStop = !!progress && !['cancelled', 'cancel_requested'].includes(current.status)
    && (active || (terminal && !retainedCopy.loading && !stoppedCopy));
  const executionWindow = taskExecutionWindow(progress?.executions ?? [], graph);
  const href = (tab: TaskSection) => taskSectionLink(task.id, tab);
  const raisedBudget = budget.data && <ul className="integration-grid"><BudgetBar budget={budget.data} currency={budget.data.currency || 'USD'} canRaise={canControl && !error} showPause={active} onRaised={() => budget.reload()}/></ul>;
  return <div className="task-details">
    <a className="task-back-link" href="#/tasks">Back to tasks</a>
    <TaskRetryOrigin task={current}/>
    <section className="card task-overview-header" aria-label="Task status">
      <div className="task-detail-heading">
        <div className="task-status-line"><Badge tone={error ? 'warn' : progress?.state === 'succeeded' ? 'ok' : progress?.state === 'failed' ? 'bad' : undefined}>{progress ? `Task status: ${progress.state}` : 'Loading progress…'}</Badge>
          <span className="muted small">{error ? 'Last recorded snapshot · refresh failed' : updated ? <>Updated <time dateTime={updated}>{fmtDateTime(updated)}</time></> : 'Waiting for the first snapshot'}</span></div>
        <div className="btn-row"><Btn small disabled={refreshing} aria-busy={refreshing} onClick={() => {manualRefresh.current = true; setRefreshing(true); setRefreshFeedback(''); setRefresh(x => x + 1); void budget.reload();}}>{refreshing ? 'Refreshing…' : 'Refresh task'}</Btn>
          {progress?.delivery?.pullRequestUrl && <a className="btn btn-primary" href={progress.delivery.pullRequestUrl} target="_blank" rel="noreferrer">Open pull request</a>}</div>
      </div>
      <p className="task-refresh-feedback muted small" role="status" aria-atomic="true">{refreshing ? 'Refreshing the task snapshot…' : refreshFeedback}</p>
      <dl className="task-facts">
        <div><dt>Workflow</dt><dd>{task.graphId} · v{task.graphVersion}</dd></div>
        <div><dt>Started</dt><dd>{executionWindow ? <time dateTime={executionWindow.start}>{fmtDateTime(executionWindow.start)}</time> : 'No execution recorded'}</dd></div>
        <div><dt>Execution duration</dt><dd>{executionWindow && (executionWindow.end || active) ? since(executionWindow.start, executionWindow.end) : 'Not recorded'}</dd></div>
        <div><dt>Recorded cost</dt><dd>{budget.data ? <>{money(budget.data.chargedMicros, budget.data.currency || 'USD')} spent of {money(budget.data.limitMicros, budget.data.currency || 'USD')}{budget.data.estimatedMicros > 0 && ' · includes estimated usage'}</> : budget.loading ? 'Loading cost…' : 'Unavailable'}{budget.error && <span className="muted small"> · Cost refresh failed</span>} <a href={href('settings')}>Budget details</a></dd></div>
      </dl>
      <TaskJourney progress={progress} error={error} task={current}/>
      {progress && !error && <TaskRetry key={`${current.id}:${current.revision}`} task={current} canEdit={canRetry}/>}
      <ErrorBox error={error}/>
      {current.launchError && <p role="alert">{current.launchError}</p>}
      {!!progress?.blockers.length && <ul aria-label="Task blockers">{progress.blockers.map(blocker => <li key={blocker}>{blocker}</li>)}</ul>}
      {active && budget.data?.paused && <section aria-label="Budget attention"><p role="alert">This run is paused because it reached its budget. {canControl ? 'Raise the budget to continue, or stop the task.' : 'Ask an administrator to raise the budget, or stop the task.'}</p>{raisedBudget}</section>}
      {graph && (!progress || !!error || progress.pendingApprovals == null || progress.pendingApprovals.length > 0) && <TaskDecisions graphId={graph} approvals={error ? undefined : progress?.pendingApprovals} canDecide={canDecide && !error} canAdmin={canControl && !error} onChanged={() => setRefresh(x => x + 1)}/>}
      {progress?.environment && <section className="task-environment-summary" aria-label="Environment summary"><div><h3>Task environment</h3><p>{progress.environment.state === 'ready' ? (current.status === 'environment_pending' ? 'Environment ready. Review it before starting agents.' : 'Environment ready.') : progress.environment.state === 'failed' ? 'Environment build failed. Inspect the logs before retrying.' : progress.environment.state === 'building' ? 'Preparing the environment. Agents have not started.' : 'No prepared environment is available.'}</p></div><a className="btn" href={href('environment')}>View environment &amp; logs</a></section>}
    </section>
    <nav className="section-navigation task-section-navigation" aria-label="Task sections">{Object.entries(taskSections).map(([key, label]) => <a key={key} href={href(key as TaskSection)} aria-current={section === key ? 'page' : undefined}>{label}</a>)}</nav>
    <div className="task-detail-content">
      {section === 'environment' && (progress?.environment ? <TaskEnvironment key={task.id} task={current} environment={progress.environment} canControl={canControl && !error} onChanged={() => setRefresh(x => x + 1)}/> : <section className="card" aria-label="Task environment"><h2>Environment &amp; logs</h2>{!progress ? <p role={error ? undefined : 'status'}>{error ? 'Environment snapshot unavailable. Use Refresh task to retry.' : 'Loading the task environment snapshot…'}</p> : <p>No task environment has been prepared. Build logs will appear here when preparation starts.</p>}</section>)}
      {section === 'overview' && <>
        {graph ? <TaskPlan key={`plan-${graph}`} graphId={graph}/> : <section className="card" aria-label="Agent plans"><h2>Agent plans</h2><p>No execution has started yet.</p></section>}
        {progress?.repositories?.length ? <section aria-label="Repository results"><h3>Repositories</h3><table><thead><tr><th>Repository</th><th>Checkout</th><th>Delivery</th></tr></thead><tbody>{progress.repositories.map(r=><tr key={r.alias}><th scope="row">{r.alias}</th><td>{r.preparation}</td><td>{r.delivery||'Local only'} {r.details} {r.pullRequest&&<a href={r.pullRequest.url} target="_blank" rel="noreferrer">PR #{r.pullRequest.number}</a>} {canControl&&progress.state==="delivery_pending"&&r.delivery!=="unchanged"&&r.delivery!=="pull_request confirmed"&&<Btn disabled={busy} onClick={()=>void retryDelivery(r.alias)}>Retry delivery for {r.alias}</Btn>}</td></tr>)}</tbody></table></section>:null}
        <TaskResult key={progress?.result?.id ?? 'no-result'} result={progress?.result} delivery={progress?.delivery} changesHref={href('changes')}/>
      </>}
      {section === 'request' && <section className="card task-detail-request" aria-label="Request"><h2>Request</h2><TaskRequestContent request={task.request}/></section>}
      {section === 'changes' && <>
        {graph ? <><section className="card"><TaskEvidence key={graph} graphId={graph} delivery={progress?.delivery} repositories={current.repositories?.map(r=>r.alias)} workspaces={progress?.workspaces ?? []} nativeProfile={nativeProfile} executions={progress?.executions ?? []} role={canControl ? 'admin' : canDecide ? 'operator' : 'viewer'}/></section>
          <section className="card"><TaskCheckpoints repositories={current.repositories?.map(r=>r.alias)} key={`checkpoints:${graph}`} graphId={graph} canRestore={canControl && !error}/></section>
          <nav className="btn-row" aria-label="Task evidence"><a href={`#/g/${encodeURIComponent(graph)}/runs`}>Execution details</a><a href={`#/g/${encodeURIComponent(graph)}/files`}>Workspace files</a></nav>
        </> : <p className="empty">No execution evidence recorded yet.</p>}
        {progress?.workspacePath && <details className="card"><summary>Open workspace in your editor</summary><WorkspaceHandoff path={progress.workspacePath} nativeProfile={nativeProfile}/></details>}
      </>}
      {section === 'activity' && (graph ? <TaskActivity key={graph} graphId={graph} role={canControl ? 'admin' : canDecide ? 'operator' : 'viewer'}/> : <p className="empty">No execution activity recorded yet.</p>)}
      {section === 'settings' && <>
        <section className="card task-workflow-details" aria-label="Workflow and executions"><h2>Workflow and executions</h2>
          <dl><div><dt>Source workflow</dt><dd><a href={`#/g/${encodeURIComponent(task.graphId)}`}>{task.graphId}</a></dd></div><div><dt>Version</dt><dd>{task.graphVersion}</dd></div><div><dt>Execution copy</dt><dd>{graph || 'Not prepared'}</dd></div></dl>
          <ul>{progress?.executions.map(execution => <li key={execution.id}><a href={`#/g/${encodeURIComponent(execution.graphId)}/runs?execution=${encodeURIComponent(execution.id)}&section=result`}>{execution.nodeId}: {execution.status}</a></li>)}</ul>
        </section>
        <TaskAuthorization taskId={task.id} canEdit={canControl && !error && ['delivery_pending','running', 'starting', 'environment_pending'].includes(current.status)} running={current.status !== 'environment_pending'} onChanged={() => setRefresh(x => x + 1)}/>
        {graph && <section className="card" aria-label="Task run budget"><h2>Budget</h2><ErrorBox error={budget.error}/>{budget.loading && !budget.data && <p>Loading budget…</p>}{raisedBudget}{!budget.data && !budget.loading && <p>Budget unavailable. <Btn onClick={() => void budget.reload()}>Retry budget</Btn></p>}</section>}
        {canControl && progress?.state === 'cancelled' && (current.projectId || !!current.repositories?.length) && <TaskCleanup repositories={current.repositories?.map(r=>r.alias)} taskId={task.id} workspaces={progress.workspaces ?? []} onChanged={() => setRefresh(x => x + 1)}/>}
      </>}
    </div>
    {canControl && canStop && <details className="task-stop-controls"><summary>Stop task</summary>{terminal && <p>This task has a recorded outcome, but its execution copy has not been confirmed stopped. Cancellation retains its files and history.</p>}{terminal && retainedCopy.error && <ErrorBox error={`Execution status unavailable: ${retainedCopy.error}`}/>}<p>Requests cancellation of this task's execution copy. Files and history are retained; external effects are not undone.</p><label><input type="checkbox" checked={confirmed} onChange={event => setConfirmed(event.target.checked)}/>Stop this task's execution copy and retain its data.</label><Btn tone="danger" disabled={busy || !confirmed || !!error} onClick={() => void cancel()}>Stop task</Btn></details>}
  </div>;
}
