import { useEffect, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { ApiError, api, errorMessage } from '../api/client';
import type { Approval } from '../api/types';
import { useAsync, useThrottled } from '../hooks/useData';
import { useEventStream } from '../hooks/useEventStream';
import { Badge, Btn, PageHeading, Empty, ErrorBox, JsonView, fmtDateTime } from './common';
import { ReviewCard } from './ReviewsView';
import { approvalKinds, approvalStatuses, decisionEntries, defaultApprovalFilters, matchingDecisions, type DecisionEntry } from './approvalPresentation';

const KIND_HELP: Record<string, string> = {
  delegation: 'A temporary analysis child requests approval. It uses the parent model with no tools or workspace access. Rejecting returns an error to the parent; approval never expands its grants.',
  task_environment: 'Review the saved task environment. Admin approval permits preparation; the trigger starts the task after the environment is ready. Tool grants and required approvals still apply.',
  environment: 'Review the proposed environment and rationale. Admin approval saves the definition and starts its build. Tasks remain blocked until ready.',
  tool: 'An agent wants to invoke a tool that requires approval.',
  message: 'A message is held at a gated channel or node.',
  turn: 'A node requires approval before each turn.',
  question: 'An agent asked a human a question (human.ask).',
};

export function ApprovalsView({ onChanged, role }: { onChanged: () => void; role?: string }) {
  const [filters, setFilters] = useState(defaultApprovalFilters);
  const list = useAsync(() => api.approvals({ status: filters.status || undefined }), [filters.status]);
  const canDecide = role === 'admin' || role === 'operator';
  const reviews = useAsync(async () => {
    if (!canDecide) return { entries: [], available: false };
    try {
      return { entries: await api.reviews(), available: true };
    } catch (error) {
      if (error instanceof ApiError && error.status === 404) return { entries: [], available: false };
      throw error;
    }
  }, [canDecide]);
  const [selected, setSelected] = useState<DecisionEntry>();
  const [page, setPage] = useState(0);
  const [notice, setNotice] = useState('');
  const searchField = useRef<HTMLInputElement>(null);
  const trigger = useRef<HTMLButtonElement | null>(null);
  const refresh = () => { void list.reload(); void reviews.reload(); };
  const reload = useThrottled(refresh, 500);
  useEventStream({ prefixes: ['approval.', 'review.'] }, reload);
  useEffect(() => {
    if (!canDecide) return;
    const timer = window.setInterval(() => void reviews.reload(), 3000);
    return () => window.clearInterval(timer);
  }, [canDecide, reviews.reload]);

  const entries = decisionEntries(list.data ?? [], reviews.data?.entries ?? []);
  const matches = matchingDecisions(entries, filters);
  const workflows = Array.from(new Set(entries.map(entry => entry.graphId))).sort();
  const limit = 20;
  const lastPage = Math.max(0, Math.ceil(matches.length / limit) - 1);
  const currentPage = Math.min(page, lastPage);
  const visible = matches.slice(currentPage * limit, (currentPage + 1) * limit);
  const detail = selected && (entries.find(entry => entry.key === selected.key) ?? selected);
  const updateFilters = (patch: Partial<typeof filters>) => {
    setFilters(current => ({ ...current, ...patch }));
    setPage(0);
  };
  const decisionSaved = () => {
    setNotice(`Decision recorded for ${detail?.title ?? 'the request'}.`);
    setSelected(undefined);
    refresh();
    onChanged();
  };

  return <div className="page approvals-page">
    <PageHeading title="Approvals" description="Review requests and inspect recorded decisions.">
      <Btn disabled={list.loading || reviews.loading} onClick={refresh}>Refresh approvals</Btn>
    </PageHeading>
    <section className="card connections-panel" aria-label="Approval requests" aria-busy={list.loading || reviews.loading}>
      <div className="approval-filters">
        <label>Search requests<input ref={searchField} type="search" placeholder="Title, ID, workflow, agent or reviewer"
          value={filters.search} onChange={event => updateFilters({ search: event.target.value })} /></label>
        <label>Approval status<select value={filters.status} onChange={event => updateFilters({ status: event.target.value })}>
          <option value="">All statuses</option>
          {Object.entries(approvalStatuses).map(([status, title]) => <option key={status} value={status}>{title}</option>)}
        </select></label>
        <label>Request type<select value={filters.kind} onChange={event => updateFilters({ kind: event.target.value })}>
          <option value="">All types</option>
          {Object.entries(approvalKinds).map(([kind, title]) => <option key={kind} value={kind}>{title}</option>)}
        </select></label>
        <label>Workflow<select value={filters.workflow} onChange={event => updateFilters({ workflow: event.target.value })}>
          <option value="">All workflows</option>
          {filters.workflow && !workflows.includes(filters.workflow) && <option value={filters.workflow}>{filters.workflow}</option>}
          {workflows.map(id => <option key={id} value={id}>{id}</option>)}
        </select></label>
        <Btn tone="ghost" disabled={!filters.search && !filters.status && !filters.kind && !filters.workflow}
          onClick={() => { setFilters({ search: '', status: '', kind: '', workflow: '' }); setPage(0); searchField.current?.focus(); }}>Clear filters</Btn>
      </div>
      <p className="muted small approval-scope">Newest requests first. Up to 200 approval requests {filters.status ? 'for the selected status' : 'across all statuses'}, plus application reviews.</p>
      <ErrorBox error={list.error ? `Approval requests: ${list.error}` : undefined} />
      <ErrorBox error={reviews.error ? `Application reviews: ${reviews.error}` : undefined} />
      {reviews.data && !reviews.data.available && <p className="muted small">{canDecide
        ? 'Application reviews are not configured on this installation.'
        : 'Application reviews require an operator or administrator.'}</p>}
      <p role="status" className="small muted connections-count">
        {list.loading || reviews.loading ? 'Refreshing requests…' : `${matches.length} requests in this recent scope`}{notice && ` · ${notice}`}
      </p>
      {!list.loading && !reviews.loading && !matches.length && <Empty>{list.error || reviews.error
        ? 'Some requests could not be loaded. Refresh approvals to retry.'
        : 'No requests match these filters.'}</Empty>}
      {matches.length > 0 && <div className="connection-table approvals-table" role="region" aria-label="Approval history" tabIndex={0}>
        <table><caption className="sr-only">Approval history</caption>
          <thead><tr><th scope="col">Request</th><th scope="col">Type</th><th scope="col">Status</th>
            <th scope="col">Workflow / agent</th><th scope="col" aria-sort="descending">Requested <span aria-hidden="true">↓</span></th><th scope="col">Decision</th></tr></thead>
          <tbody>{visible.map(entry => <tr key={entry.key}>
            <td><button type="button" className="link approval-request-link" onClick={event => {
              trigger.current = event.currentTarget;
              setSelected(entry);
            }}>{entry.title}</button><code className="small muted connection-meta">{entry.value.id}</code></td>
            <td>{approvalKinds[entry.kind] ?? entry.kind}</td>
            <td><Badge tone={entry.status === 'approved' ? 'ok' : entry.status === 'pending' ? 'warn' : undefined}>{approvalStatuses[entry.status] ?? entry.status}</Badge></td>
            <td><a href={`#/g/${encodeURIComponent(entry.graphId)}/canvas`}>{entry.graphId}</a>
              {entry.nodeId && <span className="small muted connection-meta">{entry.nodeId} · replica {entry.value.replica}</span>}</td>
            <td>{entry.createdAt ? <time dateTime={entry.createdAt}>{fmtDateTime(entry.createdAt)}</time> : 'Date unavailable'}</td>
            <td>{entry.value.decidedBy ?? (entry.status === 'pending' ? 'Awaiting decision' : '—')}
              {entry.value.decidedAt && <time className="small muted connection-meta" dateTime={entry.value.decidedAt}>{fmtDateTime(entry.value.decidedAt)}</time>}</td>
          </tr>)}</tbody>
        </table>
      </div>}
      {lastPage > 0 && <nav className="btn-row approvals-pagination" aria-label="Approval pages">
        <Btn disabled={currentPage === 0} onClick={() => setPage(currentPage - 1)}>Previous page</Btn>
        <span className="small muted">Page {currentPage + 1} of {lastPage + 1}</span>
        <Btn disabled={currentPage === lastPage} onClick={() => setPage(currentPage + 1)}>Next page</Btn>
      </nav>}
    </section>
    {detail && <DecisionDialog key={detail.key} title={detail.title} trigger={trigger.current} onClose={() => setSelected(undefined)}>
      {detail.source === 'approval'
        ? <ApprovalCard a={detail.value} canDecide={canDecide && (!['environment', 'task_environment'].includes(detail.value.kind) || role === 'admin')} onDone={decisionSaved} />
        : <ReviewCard review={detail.value} canDecide={canDecide} reload={() => { void reviews.reload(); onChanged(); }} />}
    </DecisionDialog>}
  </div>;
}

function DecisionDialog({ title, trigger, onClose, children }: {
  title: string; trigger: HTMLButtonElement | null; onClose: () => void; children: ReactNode;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    dialog.current?.showModal();
    return () => { trigger?.focus(); };
  }, [trigger]);
  return createPortal(<dialog ref={dialog} className="modal approval-dialog" aria-label={title} onClose={onClose}>
    <div className="modal-head"><h2>{title}</h2><Btn autoFocus onClick={() => dialog.current?.close()}>Close</Btn></div>
    <div className="modal-body">{children}</div>
  </dialog>, document.body);
}

export function ApprovalCard({ a, onDone, canDecide = true }: { a: Approval; onDone: () => void; canDecide?: boolean }) {
  const [comment, setComment] = useState('');
  const [answer, setAnswer] = useState('');
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);

  const decide = async (d: 'approve' | 'reject') => {
    setBusy(true);
    setError(undefined);
    try {
      await api.decide(a.id, d, { comment: comment || undefined, answer: answer || undefined });
      onDone();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <article className={`card approval kind-${a.kind}`}>
      <header>
        <Badge tone={a.kind === 'question' ? 'info' : 'warn'}>{a.kind}</Badge>
        <strong>{a.title}</strong>
      </header>
      <p className="muted small">
        {KIND_HELP[a.kind]} · {a.graphId}
        {a.nodeId ? ` / ${a.nodeId} n${a.replica}` : ''} · {fmtDateTime(a.createdAt)}
        {a.expiresAt ? ` · expires ${fmtDateTime(a.expiresAt)}` : ''}
      </p>
      {a.detail !== undefined && a.detail !== null && <JsonView value={a.detail} label="detail" open={a.kind !== 'question'} />}
      {a.status === 'pending' && !canDecide ? <p>{['environment', 'task_environment'].includes(a.kind) ? 'Administrator approval is required.' : 'Waiting for an authorized operator to decide.'}</p> : a.status === 'pending' ? (
        <div className="form">
          {a.kind === 'question' && (
            <label>
              answer
              <textarea rows={3} value={answer} onChange={(e) => setAnswer(e.target.value)} />
            </label>
          )}
          <label>
            comment
            <input value={comment} onChange={(e) => setComment(e.target.value)} placeholder="optional, recorded in the audit log" />
          </label>
          <div className="btn-row">
            <Btn tone="primary" disabled={busy || (a.kind === 'question' && !answer.trim())} onClick={() => decide('approve')}>
              {a.kind === 'question' ? 'Answer' : 'Approve'}
            </Btn>
            <Btn tone="danger" disabled={busy} onClick={() => decide('reject')}>
              Reject
            </Btn>
          </div>
          <ErrorBox error={error} />
        </div>
      ) : (
        <p className="small">
          <Badge tone={a.status === 'approved' ? 'ok' : 'bad'}>{a.status}</Badge> by {a.decidedBy ?? '?'}{' '}
          {fmtDateTime(a.decidedAt)}
          {a.comment ? ` – “${a.comment}”` : ''}
          {a.answer ? ` – answer: “${a.answer}”` : ''}
        </p>
      )}
    </article>
  );
}
