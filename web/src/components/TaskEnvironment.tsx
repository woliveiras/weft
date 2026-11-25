import {useEffect, useRef, useState} from 'react';
import {api, errorMessage} from '../api/client';
import type {TaskDraft, TaskReview} from '../api/tasks';
import type {EnvironmentView} from '../api/types';
import {Badge, Btn, ErrorBox, fmtDateTime} from './common';

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

export function TaskEnvironment({task, environment, canControl, onChanged}: {task: TaskDraft; environment: EnvironmentView; canControl: boolean; onChanged: () => void}) {
  const [review, setReview] = useState<TaskReview>();
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const inspect = async () => {
    setBusy(true); setError(undefined); setReview(undefined); setConfirmed(false);
    try {setReview(await api.reviewTask(task.id));}
    catch (cause) {setError(errorMessage(cause));}
    finally {setBusy(false);}
  };
  const act = async (build: boolean) => {
    if (!confirmed || !canControl || busy) return;
    setBusy(true); setError(undefined);
    try {
      if (build) await api.prepareTaskEnvironment(task.id, task.revision);
      else if (review) await api.startTask(task.id, review.task.revision);
      setReview(undefined); onChanged();
    } catch (cause) {setError(errorMessage(cause)); onChanged();}
    finally {setConfirmed(false); setBusy(false);}
  };
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
      {task.status === 'environment_pending' && <>
        <p>{environment.state === 'building' ? 'The environment is building. Follow its output below; review and start become available after it is ready.' : environment.upToDate ? 'The environment is ready. Review the task requirements, then confirm before starting agents.' : 'Inspect the build output and any errors before reviewing or retrying preparation.'}</p>
        <div className="btn-row"><Btn disabled={busy || environment.state === 'building'} aria-busy={busy} onClick={() => void inspect()}>{busy ? 'Checking environment…' : 'Review prepared task requirements'}</Btn></div>
        {review && <section className="task-environment-requirements" aria-label="Prepared task requirements">
          {review.blockers.length > 0 && <ul>{review.blockers.map(blocker => <li key={blocker}>{blocker}</li>)}</ul>}
          <p>Workflow version {review.currentGraphVersion}. Agent permissions remain unchanged.</p>
        </section>}
        {canControl && <div className="task-environment-confirmation">
          <label className="check"><input type="checkbox" disabled={busy || environment.state === 'building'} checked={confirmed} onChange={event => setConfirmed(event.target.checked)}/>I reviewed this task environment and authorize the selected build or start action.</label>
          <div className="btn-row">
            {!environment.upToDate && <Btn disabled={busy || !confirmed || environment.state === 'building'} onClick={() => void act(true)}>Retry environment build</Btn>}
            <Btn tone="primary" disabled={busy || !confirmed || !review?.canStart || !environment.upToDate} onClick={() => void act(false)}>Start prepared task</Btn>
          </div>
        </div>}
      </>}
    </section>
    <TaskBuildLog environment={environment}/>
  </div>;
}
