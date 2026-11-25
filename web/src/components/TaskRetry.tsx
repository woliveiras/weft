import {useEffect, useRef, useState} from 'react';
import {api, ApiError, errorMessage} from '../api/client';
import type {TaskDraft} from '../api/tasks';
import {Btn, ErrorBox} from './common';

/** Presentation eligibility; the server enforces the source revision and status. */
export function canRetryTask(task?: TaskDraft): boolean {
  return !!task && task.status === 'failed' && !task.triggerGitHubIssue && !task.triggerIssueOrigin;
}

export function TaskRetryOrigin({task}: {task: TaskDraft}) {
  const origin = useRef<HTMLElement>(null);
  useEffect(() => {
    if (task.status === 'draft' && task.retryOfTaskId) origin.current?.focus();
    // Only a newly mounted task draft moves focus, never a progress refresh.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  if (!task.retryOfTaskId) return null;
  return <aside ref={origin} tabIndex={-1} className="card task-retry-origin" aria-label="Attempt origin">
    <p>This is a separate attempt from <a href={`#/tasks/${encodeURIComponent(task.retryOfTaskId)}`}>Original failed task</a> (revision {task.retryOfRevision}). Its history and costs remain separate.</p>
    {task.status === 'draft' && <p>Review this attempt before starting. Permissions, approvals and budget are checked again.</p>}
  </aside>;
}

export function TaskRetry({task, canEdit}: {task: TaskDraft; canEdit: boolean}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const pending = useRef(false);
  const mounted = useRef(true);
  useEffect(() => {mounted.current = true; return () => {mounted.current = false;};}, []);
  const retry = async () => {
    if (!canEdit || !canRetryTask(task) || pending.current) return;
    pending.current = true; setBusy(true); setError(undefined);
    try {
      const attempt = await api.retryTask(task.id, task.revision);
      if (!/^tsk_[a-z0-9]{1,80}$/.test(attempt.id) || attempt.id === task.id
        || attempt.retryOfTaskId !== task.id || attempt.retryOfRevision !== task.revision) {
        throw new Error('The returned attempt does not belong to this failed task snapshot.');
      }
      // Replays can return the same attempt after it has been edited or started.
      if (mounted.current) location.hash = `#/tasks/${encodeURIComponent(attempt.id)}`;
    } catch (cause) {
      if (mounted.current) setError(cause instanceof ApiError && cause.status === 409
        ? `Could not create an attempt: ${errorMessage(cause)} Refresh the task and review its current state before trying again.`
        : `Could not confirm the new attempt: ${errorMessage(cause)} Try again to recover the same attempt; no agents start from this action.`);
    } finally {
      pending.current = false;
      if (mounted.current) setBusy(false);
    }
  };
  if (!canEdit || !canRetryTask(task)) return null;
  return <section className="task-retry-controls" aria-label="Try task again">
    <div><h3>Try again with a new attempt</h3>
      <p>Create a draft with the same request, workflow version and repository commit. It will use a new workspace and its own approvals when you start it.</p>
      <p className="muted small">The original task and costs remain available. Review existing changes and remote actions before running again. Creating this draft does not start agents.</p>
    </div>
    <div className="btn-row"><Btn tone="primary" disabled={busy} aria-busy={busy} onClick={() => void retry()}>{busy ? 'Creating attempt…' : 'Try again'}</Btn></div>
    <p role="status" aria-atomic="true">{busy ? 'Creating a new attempt. No agents are starting…' : ''}</p>
    <ErrorBox error={error}/>
  </section>;
}
