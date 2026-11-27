import { FieldHelp } from './FieldHelp';
import { useRef, useState } from 'react';
import { parse } from 'yaml';
import { ApiError, api, errorMessage } from '../api/client';
import type { Graph, GraphAction, GraphSummary } from '../api/types';
import { Badge, Btn, Empty, ErrorBox, Icon, Modal, PageHeading, fmtDateTime } from './common';

import { listedWorkflows, workflowStates } from './workflowPresentation';

export function GraphList({
  graphs,
  dirty,
  canDelete = false,
  canCreate = true,
  canControl = true,
  loading = false,
  error,
  onOpen,
  onChanged,
  onAgent,
}: {
  graphs: GraphSummary[];
  dirty?: Record<string, boolean>;
  canDelete?: boolean;
  canCreate?: boolean;
  canControl?: boolean;
  loading?: boolean;
  error?: string;
  onOpen: (id: string) => void;
  onChanged: () => void;
  onAgent?: (graph: string, agent: string) => void;
}) {
  const [search, setSearch] = useState('');
  const [status, setStatus] = useState('');
  const [page, setPage] = useState(0);
  const [pendingAction, setPendingAction] = useState<{ id: string; action: GraphAction }>();
  const actionLock = useRef(false);
  const searchField = useRef<HTMLInputElement>(null);
  const all = listedWorkflows(graphs);
  const matches = listedWorkflows(graphs, search, status);
  const pageSize = 20;
  const lastPage = Math.max(0, Math.ceil(matches.length / pageSize) - 1);
  const currentPage = Math.min(page, lastPage);
  const visible = matches.slice(currentPage * pageSize, (currentPage + 1) * pageSize);
  const [actionError, setActionError] = useState<string>();
  const [budgetError, setBudgetError] = useState<{ graphId: string; message: string }>();
  const [deletion, setDeletion] = useState<{ graph: GraphSummary; reason: string }>();
  const [deletionReason, setDeletionReason] = useState('');
  const [deleting, setDeleting] = useState(false);

  const act = async (id: string, a: GraphAction) => {
    if (!canControl || actionLock.current) return;
    actionLock.current = true;
    setPendingAction({ id, action: a });
    setBudgetError(undefined);
    setActionError(undefined);
    try {
      await api.graphAction(id, a);
      setActionError(undefined);
      onChanged();
    } catch (e) {
      if (e instanceof ApiError && e.code === 'budget_resume_blocked') {
        setBudgetError({ graphId: id, message: e.message });
      } else {
        const name = graphs.find(item => item.graph.metadata.id === id)?.graph.metadata.name || id;
        setActionError(`${name}: ${errorMessage(e)}`);
      }
    } finally {
      actionLock.current = false;
      setPendingAction(undefined);
    }
  };

  const del = async (s: GraphSummary, force = false) => {
    if (!canDelete || deleting) return;
    if (!force && !confirm(`Delete graph “${s.graph.metadata.name || s.graph.metadata.id}”? Its history stays in the event log.`)) return;
    setDeleting(true);
    setActionError(undefined);
    try {
      if (force && deletionReason.trim()) {
        await api.deleteGraph(s.graph.metadata.id, true, deletionReason.trim());
      } else {
        await api.deleteGraph(s.graph.metadata.id, force);
      }
      setDeletion(undefined);
      onChanged();
    } catch (e) {
      if (e instanceof ApiError && e.code === 'environment_cleanup_incomplete') {
        setDeletionReason('');
        setDeletion({ graph: s, reason: errorMessage(e) });
      } else {
        setDeletion(undefined);
        setActionError(errorMessage(e));
      }
    } finally {
      setDeleting(false);
    }
  };

  return (
    <div className="page workflows-page">
      <PageHeading title="Workflows" description="Find workflows, configure their agents and manage execution.">
        <div className="btn-row">
          <Btn disabled={loading} onClick={onChanged}>Refresh workflows</Btn>
          {canCreate && <Btn tone="primary" onClick={() => { location.hash = '#/graphs/new'; }}>New workflow</Btn>}
        </div>
      </PageHeading>
      <section className="card connections-panel" aria-label="Workflow list" aria-busy={loading}>
        <div className="workflow-filters">
          <label>Search workflows<input ref={searchField} type="search" placeholder="Name, description or ID" value={search}
            onChange={event => { setSearch(event.target.value); setPage(0); }} /></label>
          <label>Workflow status<select value={status} onChange={event => { setStatus(event.target.value); setPage(0); }}>
            <option value="">All statuses</option>
            {Object.entries(workflowStates).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
          </select></label>
          <Btn tone="ghost" disabled={!search && !status} onClick={() => {
            setSearch(''); setStatus(''); setPage(0); searchField.current?.focus();
          }}>Clear filters</Btn>
        </div>
        <p className="muted small workflow-list-count" role="status">{loading ? 'Refreshing workflows…' : error ? 'Workflow list could not be refreshed.' : `${matches.length} of ${all.length} workflows · Most recently updated first`}</p>
        <ErrorBox error={error ?? actionError} />
        {!loading && !error && !matches.length && <Empty>{all.length ? 'No workflows match these filters.' : 'No workflows yet. Create one to configure agents and channels.'}</Empty>}
        {matches.length > 0 && <div className="connection-table workflow-table" role="region" aria-label="Workflows" tabIndex={0}>
          <table><caption className="sr-only">Workflows</caption>
            <thead><tr><th scope="col">Workflow</th>
              <th scope="col"><span className="field-help-caption">Status <FieldHelp label="Workflow status">Running means the workflow is active. Agents may be waiting for messages or events; it does not mean a task is currently executing. Paused workflows wait for resume. Stopping means shutdown is in progress. Open the workflow's Activity or Runs to inspect actual work and results.</FieldHelp></span></th>
              <th scope="col">Version</th><th scope="col">Agents</th><th scope="col">Channels</th>
              <th scope="col" aria-sort="descending">Updated <span aria-hidden="true">↓</span></th><th scope="col">Actions</th></tr></thead>
            <tbody>{visible.map(s => {
              const g = s.graph;
              const id = g.metadata.id;
              const name = g.metadata.name || id;
              const state = s.record.state;
              const busy = pendingAction?.id === id;
              const disabled = !!pendingAction || deleting || loading || !!error || state === 'stopping';
              return <tr key={id} className="workflow-row" onClick={event => {
                if ((event.target as HTMLElement).closest('button, a, summary, details') || window.getSelection()?.toString()) return;
                onOpen(id);
              }}>
                <td><button type="button" className="link workflow-open" onClick={() => onOpen(id)}>{name}</button>
                  {dirty?.[id] && <Badge tone="warn">Unsaved draft</Badge>}
                  {g.metadata.description && <p className="muted small workflow-description">{g.metadata.description}</p>}
                  <code className="small muted connection-meta">{id}</code></td>
                <td><Badge tone={state === 'running' ? 'ok' : state === 'failed' ? 'bad' : state === 'paused' ? 'warn' : undefined}>{workflowStates[state] ?? state}</Badge></td>
                <td>v{s.record.version}</td>
                <td>{g.spec.nodes.length ? <details className="workflow-agents">
                  <summary>{g.spec.nodes.length} {g.spec.nodes.length === 1 ? 'agent' : 'agents'}<span className="sr-only"> in {name}</span></summary>
                  <ul aria-label={`Agents in ${name}`}>{g.spec.nodes.map(node => <li key={node.id}>
                    <button type="button" className="link" onClick={() => onAgent ? onAgent(id, node.id) : onOpen(id)}>Configure {node.name || node.id}</button>
                  </li>)}</ul>
                </details> : '0 agents'}</td>
                <td>{g.spec.channels?.length ?? 0}</td>
                <td>{s.record.updatedAt ? <time dateTime={s.record.updatedAt}>{fmtDateTime(s.record.updatedAt)}</time> : 'Date unavailable'}
                  {s.record.startedAt && <span className="muted small connection-meta">Started {fmtDateTime(s.record.startedAt)}</span>}</td>
                <td><div className="btn-row workflow-actions" aria-label={`Actions for ${name}`}>
                  {canControl && <a className="btn btn-sm" href={`#/tasks/new?workflow=${encodeURIComponent(id)}`}>New task<span className="sr-only"> for {name}</span></a>}
                  {canControl && (state === 'stopped' || state === 'failed' ?
                    <Btn small tone="primary" disabled={disabled} onClick={() => void act(id, 'start')}><Icon name="play" />{busy ? 'Starting…' : 'Start'}</Btn> : <>
                      <Btn small disabled={disabled} onClick={() => void act(id, 'stop')}><Icon name="stop" />{busy && pendingAction.action === 'stop' ? 'Stopping…' : 'Stop'}</Btn>
                      <Btn small disabled={disabled} onClick={() => void act(id, state === 'paused' ? 'resume' : 'pause')}><Icon name="pause" />{busy && pendingAction.action !== 'stop' ? (state === 'paused' ? 'Resuming…' : 'Pausing…') : state === 'paused' ? 'Resume' : 'Pause'}</Btn>
                    </>)}
                  {canDelete && <details className="workflow-more"><summary aria-label={`More actions for ${name}`}>More actions</summary>
                    <Btn small tone="danger" disabled={state === 'running' || state === 'stopping' || !!pendingAction || deleting || loading || !!error}
                      onClick={event => { event.currentTarget.closest('details')?.removeAttribute('open'); void del(s); }}>Delete</Btn>
                  </details>}
                  {!canControl && !canDelete && <span className="muted small">Read-only</span>}
                </div>
                {budgetError?.graphId === id && <ErrorBox error={budgetError.message} />}</td>
              </tr>;
            })}</tbody>
          </table>
        </div>}
        {lastPage > 0 && <nav className="btn-row workflows-pagination" aria-label="Workflow pages">
          <Btn disabled={currentPage === 0} onClick={() => setPage(currentPage - 1)}>Previous page</Btn>
          <span className="muted small">Page {currentPage + 1} of {lastPage + 1}</span>
          <Btn disabled={currentPage === lastPage} onClick={() => setPage(currentPage + 1)}>Next page</Btn>
        </nav>}
      </section>
      {canDelete && deletion && (
        <Modal title={`Delete graph “${deletion.graph.graph.metadata.name || deletion.graph.graph.metadata.id}”?`}
          onClose={() => { if (!deleting) setDeletion(undefined); }}>
          <ErrorBox error={deletion.reason} />
          <p>The graph ID stays reserved until cleanup or an administrator releases it. Cleanup requires the original sandbox daemon. Manage the reservation in Administration → Shared images and caches.</p>
          <label>Reason for deferring cleanup (optional)
            <input maxLength={500} value={deletionReason} disabled={deleting} onChange={event => setDeletionReason(event.target.value)} />
          </label>
          <div className="btn-row">
            <Btn autoFocus disabled={deleting} onClick={() => setDeletion(undefined)}>Keep graph</Btn>
            <Btn tone="danger" disabled={deleting} onClick={() => void del(deletion.graph, true)}>
              {deleting ? 'Deleting…' : 'Delete and defer cleanup'}
            </Btn>
          </div>
        </Modal>
      )}

    </div>
  );
}

// Re-exported for tests / other screens that want to parse documents the same way.
export function parseGraphText(text: string): Graph {
  const t = text.trim();
  return (t.startsWith('{') ? JSON.parse(t) : parse(t)) as Graph;
}
