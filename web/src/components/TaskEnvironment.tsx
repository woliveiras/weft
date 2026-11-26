import {useCallback, useEffect, useRef, useState} from 'react';
import {api, errorMessage} from '../api/client';
import type {TaskAuthorization, TaskDraft, TaskReview} from '../api/tasks';
import type {EnvironmentView} from '../api/types';
import {Badge, Btn, ErrorBox, fmtDateTime} from './common';
import {money} from './cost';
import {taskLaunchIdentity, type TaskLaunchReview} from './taskLaunch';

function TaskBuildLog({environment}: {environment: EnvironmentView}) {
  const output = useRef<HTMLPreElement>(null);
  const [follow, setFollow] = useState(true);
  useEffect(() => {
    if (follow && output.current) output.current.scrollTop = output.current.scrollHeight;
  }, [environment.log, follow]);
  return <section className="card task-build-log" aria-label="Build logs">
    <div className="task-detail-heading"><h2>Build logs</h2>
      <label className="check"><input type="checkbox" checked={follow} onChange={event => setFollow(event.target.checked)}/>Follow latest output</label>
    </div>
    <p className="muted small">Logs update with the task snapshot every three seconds. Turn off follow to inspect earlier output.</p>
    {environment.log ? <pre ref={output} tabIndex={0} aria-label="Build output">{environment.log}</pre>
      : <p className="empty">No build log recorded yet. Output will appear when the build produces logs.</p>}
  </section>;
}

/** A review belongs to one saved task revision, never to a later polled snapshot. */
export function canRunPreparedTask(task: TaskDraft, environment: EnvironmentView, review?: TaskReview, authorization?: TaskAuthorization) {
  return task.status === 'environment_pending' && environment.state === 'ready' && environment.upToDate === true
    && review?.task.id === task.id && review.task.revision === task.revision && review.canStart === true
    && !!authorization?.scope && (!authorization.record || (authorization.record.taskId === task.id && authorization.record.scope === authorization.scope));
}

export function TaskEnvironment({task, environment, canControl, onChanged}: {task: TaskDraft; environment: EnvironmentView; canControl: boolean; onChanged: () => void}) {
  const [checked, setChecked] = useState<TaskLaunchReview & {key: string}>();
  const [confirmed, setConfirmed] = useState(false);
  const [reviewing, setReviewing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [feedback, setFeedback] = useState<string>();
  const request = useRef(0);
  const acting = useRef(false);
  const key = JSON.stringify([task.id, task.revision, task.status, environment.state, environment.upToDate, environment.image]);
  const current = checked?.key === key ? checked : undefined;
  const review = current?.review;
  const authorization = current?.authorization;
  const loadChecks = useCallback(async (): Promise<TaskLaunchReview> => {
    const [review, authorization, budget] = await Promise.all([
      api.reviewTask(task.id), api.taskAuthorization(task.id), api.budgetPreview(task.graphId,task.projectId,...(task.repositories?.length?[task.repositories]:[])),
    ]);
    if (review.task.id !== task.id || review.task.revision !== task.revision) {
      throw new Error('The task changed while checking requirements. Refresh the task to review its latest settings.');
    }
    if (!authorization.scope) throw new Error('Authorization details are incomplete. Check requirements again before running.');
    let workspace: TaskLaunchReview['workspace'];
    if (!review.project && !task.projectId) {
      const pinned = await api.getGraph(task.graphId, task.graphVersion);
      if (pinned.graph.metadata.id !== task.graphId || pinned.graph.metadata.version !== task.graphVersion) {
        throw new Error('The pinned workflow could not be verified. Check requirements again before running.');
      }
      workspace = pinned.graph.spec.workspace ?? {};
    }
    return {review, authorization, budget, workspace};
  }, [task.id, task.revision, task.graphId, task.graphVersion, task.projectId]);
  const inspect = useCallback(async () => {
    const generation = ++request.current;
    setReviewing(true); setError(undefined); setFeedback(undefined); setChecked(undefined); setConfirmed(false);
    try {
      const checks = await loadChecks();
      if (generation !== request.current) return;
      setChecked({key, ...checks});
    } catch (cause) {if (generation === request.current) setError(errorMessage(cause));}
    finally {if (generation === request.current) setReviewing(false);}
  }, [key, loadChecks]);
  useEffect(() => {
    if (task.status === 'environment_pending' && environment.state !== 'building') void inspect();
    else {setChecked(undefined); setConfirmed(false); setReviewing(false);}
    return () => {request.current++;};
  }, [inspect, task.status, environment.state]);
  const budgetBlocked = current?.budget.budgets.some(item => item.reached || item.paused || item.invalid) ?? false;
  const canRun = canRunPreparedTask(task, environment, review, authorization) && !budgetBlocked;
  // canPrepareEnvironment is the draft-to-prepared admission flag; pending copies retry their retained definition.
  const canBuild = !!review?.environmentDefinition && task.status === 'environment_pending' && environment.state !== 'building' && !environment.upToDate;
  const staleAuthorization = authorization?.record && authorization.record.scope !== authorization.scope;
  const act = async (build: boolean) => {
    if (!canControl || acting.current || reviewing || !review || (build ? !confirmed || !canBuild : !canRun)) return;
    const generation = request.current;
    acting.current = true; setBusy(true); setError(undefined); setFeedback(undefined);
    try {
      const fresh = await loadChecks();
      if (generation !== request.current) return;
      if (!current || !taskLaunchIdentity(current) || taskLaunchIdentity(current) !== taskLaunchIdentity(fresh)) {
        setChecked({key, ...fresh});
        setFeedback('Task requirements changed. Review the updated access, environment and budget below, then choose the action again.');
        return;
      }
      if (build ? !fresh.review.environmentDefinition : !canRunPreparedTask(task, environment, fresh.review, fresh.authorization)) {
        setChecked({key, ...fresh});
        setFeedback('The task has not started. Resolve the current requirements before continuing.');
        return;
      }
      if (build) await api.prepareTaskEnvironment(task.id, fresh.review.task.revision);
      else await api.startTask(task.id, fresh.review.task.revision);
      setChecked(undefined); onChanged();
    } catch (cause) {setError(errorMessage(cause)); setChecked(undefined); onChanged();}
    finally {setConfirmed(false); setBusy(false); acting.current = false;}
  };
  const agents = review?.agents ?? (review ? [{id: task.nodeId, isolation: review.isolation, grants: review.grants ?? []}] : []);
  const mode = authorization?.record?.mode ?? 'ask';
  return <div className="task-environment-panel">
    <section className="card task-environment-controls" aria-label="Task environment preparation">
      <div className="task-detail-heading"><h2>Environment preparation</h2><Badge tone={environment.state === 'failed' ? 'bad' : environment.state === 'ready' ? 'ok' : undefined}>{environment.state}</Badge></div>
      <p>Environment: {environment.state}{environment.phase ? ` · ${environment.phase}` : ''}. Agents do not start automatically.</p>
      <dl className="task-environment-facts">
        {environment.startedAt && <div><dt>Build started</dt><dd>{fmtDateTime(environment.startedAt)}</dd></div>}
        {environment.finishedAt && <div><dt>Build finished</dt><dd>{fmtDateTime(environment.finishedAt)}</dd></div>}
        {environment.image && <div><dt>Image</dt><dd><code>{environment.image}</code></dd></div>}
      </dl>
      <ErrorBox error={error}/><ErrorBox error={environment.error}/>
      {feedback && <p role="status">{feedback}</p>}
      {task.status === 'environment_pending' && <>
        <p role="status">{environment.state === 'building' ? 'The environment is building. Follow its output below. Requirements will be checked when it is ready; agents will not start automatically.' : reviewing ? 'Checking task requirements…' : canRun ? 'Ready to run. Run task starts this saved configuration with the access and limits shown below.' : 'The task has not started. Resolve the requirements below before running.'}</p>
        {review && <section className="task-environment-requirements" aria-label="Prepared task requirements">
          {review.blockers.length > 0 && <ul>{review.blockers.map(blocker => <li key={blocker}>{blocker}</li>)}</ul>}
          {staleAuthorization && <p role="alert">Saved authorization is out of date. Review access in Settings & access before running.</p>}
          {budgetBlocked && <p role="alert">A spending limit prevents this task from starting. Review the budget limits before running.</p>}
          <dl className="task-environment-facts">
            <div><dt>Workflow</dt><dd>{task.graphId} · Version {review.task.graphVersion}</dd></div>
            <div><dt>Agents</dt><dd>{task.nodeId} → {task.resultNodeId || task.nodeId}</dd></div>
            <div><dt>Repository</dt><dd>{review.project ? <>{review.project.name}{(review.project.repositoryUrl || review.project.path) && <> · {review.project.repositoryUrl || review.project.path}</>}{task.baseCommit && <> · Commit <code>{task.baseCommit}</code></>}</> : task.projectId || current?.workspace?.repo || 'Empty managed workspace — no repository selected'}</dd></div>
            <div><dt>Access</dt><dd>{mode === 'ask' ? 'Ask for approval' : mode === 'workspace' ? 'Workspace access' : 'Full access'}</dd></div>
          </dl>
          {current && <p>{current.budget.taskRunBudgetMicros > 0 ? `Task budget: ${money(current.budget.taskRunBudgetMicros, current.budget.currency)}.` : 'No per-task spending limit is configured.'} {current.budget.budgets.map(budget => `${budget.scope}: ${money(budget.limitMicros, current.budget.currency)}`).join(' · ')}</p>}
          <p>Granted tools: {agents.flatMap(agent => agent.grants ?? []).map(grant => grant.name).join(', ') || 'None'}. Mandatory approvals and task/installation budgets still apply.</p>
          <details><summary>Execution boundaries and tool restrictions</summary>{agents.map(agent => <section key={agent.id}>
            <h3>{agent.id}</h3><p>Isolation: {agent.isolation}. Process and Kubernetes commands run with the Weft server’s available access; Docker commands run in the dedicated sandbox.</p>
            <pre>{JSON.stringify(agent.grants ?? [], null, 2)}</pre>
          </section>)}</details>
          {canBuild && <details><summary>Environment build definition</summary><pre>{JSON.stringify(review.environmentDefinition ?? {}, null, 2)}</pre></details>}
        </section>}
        {canControl && <div className="task-environment-confirmation">
          {canBuild && <label className="check"><input type="checkbox" disabled={busy || reviewing} checked={confirmed} onChange={event => setConfirmed(event.target.checked)}/>I reviewed the environment build definition and authorize retrying this build.</label>}
          <div className="btn-row">
            {canBuild && <Btn disabled={busy || reviewing || !confirmed || budgetBlocked} onClick={() => void act(true)}>{busy ? 'Preparing environment…' : 'Retry environment build'}</Btn>}
            {environment.state !== 'building' && <Btn tone="primary" disabled={busy || reviewing || !canRun} aria-busy={busy} onClick={() => void act(false)}>{busy && !canBuild ? 'Starting task…' : 'Run task'}</Btn>}
            {(error || (review && !canRun)) && <Btn disabled={busy || reviewing} onClick={() => void inspect()}>Check requirements again</Btn>}
          </div>
        </div>}
        {!canControl && <p className="muted">An administrator can run the task or retry its environment build.</p>}
      </>}
    </section>
    <TaskBuildLog environment={environment}/>
  </div>;
}
