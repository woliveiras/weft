import { useState, type ReactNode } from 'react';
import { api } from '../api/client';
import type { TaskDraft } from '../api/tasks';
import type { GraphSummary, Info } from '../api/types';
import { useAsync } from '../hooks/useData';
import { Badge, Btn, ErrorBox, fmtDateTime } from './common';

const statusLabels: Record<string, string> = {
  draft: 'Draft', preparing: 'Preparing', environment_pending: 'Waiting for environment',
  environment_build_requested: 'Environment build requested', environment_cancel_requested: 'Cancelling environment',
  starting: 'Starting', running: 'Running', awaiting_approval: 'Awaiting approval', blocked: 'Blocked',
  succeeded: 'Succeeded', failed: 'Failed', interrupted: 'Interrupted', cancel_requested: 'Stopping', cancelled: 'Cancelled',
};
const statusTones: Record<string, string> = {
  running: 'info', awaiting_approval: 'warn', blocked: 'warn', succeeded: 'ok', failed: 'bad', interrupted: 'warn',
};

const taskStatus = (task: TaskDraft) => task.observedState || task.status;
const taskIssue = (task: TaskDraft) => task.triggerGitHubIssue || task.source?.reference;
const statusLabel = (status: string) => statusLabels[status] ?? status.replaceAll('_', ' ');

export function TasksList({ graphs, role, children }: {
  graphs: GraphSummary[];
  role?: NonNullable<Info['identity']>['role'];
  children?: ReactNode;
}) {
  const [cursors, setCursors] = useState<string[]>([]);
  const [query, setQuery] = useState('');
  const [status, setStatus] = useState('');
  const [workflow, setWorkflow] = useState('');
  const after = cursors.at(-1);
  const list = useAsync(() => api.tasks(after), [after]);
  const items = list.data?.items ?? [];
  const workflowNames = new Map(graphs.filter(({ graph }) => !graph.metadata.id.startsWith('task-run-'))
    .map(({ graph }) => [graph.metadata.id, graph.metadata.name || graph.metadata.id]));
  for (const id of [workflow, ...items.map(task => task.graphId)]) {
    if (id && !workflowNames.has(id)) workflowNames.set(id, id);
  }
  const workflows = [...workflowNames].sort(([idA, nameA], [idB, nameB]) => nameA.localeCompare(nameB) || idA.localeCompare(idB));
  const statuses = new Set([...Object.keys(statusLabels), ...items.map(taskStatus).filter(Boolean), ...(status ? [status] : [])]);
  const search = query.trim().toLowerCase();
  const rows = items.filter(task =>
    (!status || taskStatus(task) === status) && (!workflow || task.graphId === workflow) &&
    (!search || [task.title, task.id, taskIssue(task)].some(value => value?.toLowerCase().includes(search))),
  );
  const next = list.data?.next;

  return <section className="page fill tasks-list-page" aria-label="Tasks">
    <header className="page-head">
      <div><h1>Tasks</h1><p className="muted">Review saved requests and follow their progress.</p></div>
      <div className="btn-row">
        <Btn disabled={list.loading} onClick={() => void list.reload()}>Refresh tasks</Btn>
        {(role === 'admin' || role === 'operator') && <a className="btn btn-primary" href="#/tasks/new">New task</a>}
      </div>
    </header>
    {children}
    <div className="connection-filters tasks-filters">
      <label>Search tasks<input type="search" value={query} onChange={event => setQuery(event.target.value)} placeholder="Title, task ID or issue" /></label>
      <label>Task status<select value={status} onChange={event => setStatus(event.target.value)}>
        <option value="">All statuses</option>
        {[...statuses].map(value => <option value={value} key={value}>{statusLabel(value)}</option>)}
      </select></label>
      <label>Workflow<select value={workflow} onChange={event => setWorkflow(event.target.value)}>
        <option value="">All workflows</option>
        {workflows.map(([id, name]) => <option value={id} key={id}>{name === id ? id : `${name} (${id})`}</option>)}
      </select></label>
      {(query || status || workflow) && <Btn onClick={() => { setQuery(''); setStatus(''); setWorkflow(''); }}>Clear filters</Btn>}
    </div>
    <ErrorBox error={list.error} />
    <p className="small muted task-list-count" role="status">
      {list.loading ? 'Loading tasks…' : list.error ? '' : `${rows.length} of ${items.length} tasks on this page`}
    </p>
    {!list.loading && !list.error && <>
      {rows.length > 0 ? <div className="connection-table tasks-table" role="region" aria-label="Task history" tabIndex={0}>
        <table>
          <caption className="sr-only">Task history</caption>
          <thead><tr>
            <th scope="col">Task</th><th scope="col">Status</th><th scope="col">Workflow</th>
            <th scope="col">Issue</th><th scope="col">Created</th><th scope="col" aria-sort="descending">Updated <span aria-hidden="true">↓</span></th>
          </tr></thead>
          <tbody>{rows.map(task => {
            const state = taskStatus(task);
            const workflowName = workflowNames.get(task.graphId);
            return <tr key={task.id}>
              <td><a href={`#/tasks/${encodeURIComponent(task.id)}`}>{task.title}</a><code className="small muted">{task.id}</code></td>
              <td><Badge tone={statusTones[state]}>{state ? statusLabel(state) : 'Unknown'}</Badge>
                {task.status === 'draft' && task.resolution && task.resolution.state !== 'resolved' &&
                  <span className="small muted">{task.resolution.message}</span>}</td>
              <td>{workflowName || '—'}{task.graphVersion > 0 && <span className="small muted">Version {task.graphVersion}</span>}</td>
              <td>{taskIssue(task) || '—'}</td>
              <td><TaskDate value={task.createdAt} />{task.createdBy && <span className="small muted">{task.createdBy}</span>}</td>
              <td><TaskDate value={task.updatedAt} /></td>
            </tr>;
          })}</tbody>
        </table>
      </div> : <p>{items.length ? 'No tasks match these filters on this page.' : after ? 'No tasks on this page.' : 'No tasks yet.'}</p>}
    </>}
    {(after || (!list.loading && !list.error && next)) && <nav className="btn-row tasks-pagination" aria-label="Task pages">
      <Btn disabled={!after || list.loading} onClick={() => setCursors(current => current.slice(0, -1))}>Previous page</Btn>
      <span className="small muted">Page {cursors.length + 1}</span>
      <Btn disabled={!next || list.loading || !!list.error} onClick={() => { if (next) setCursors(current => [...current, next]); }}>Next page</Btn>
    </nav>}
  </section>;
}

function TaskDate({ value }: { value?: string }) {
  return value ? <time dateTime={value}>{fmtDateTime(value)}</time> : <>—</>;
}
