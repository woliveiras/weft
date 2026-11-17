import { ReviewsView } from './ReviewsView';
import { useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { Approval, ApprovalStatus } from '../api/types';
import { useAsync, useThrottled } from '../hooks/useData';
import { useEventStream } from '../hooks/useEventStream';
import { Badge, Btn, PageHeading, Empty, ErrorBox, JsonView, fmtDateTime } from './common';

const KIND_HELP: Record<string, string> = {
  delegation: 'A temporary analysis child requests approval. It uses the parent model with no tools or workspace access. Rejecting returns an error to the parent; approval never expands its grants.',
  environment: 'Review the proposed environment and rationale. Admin approval saves the definition and starts its build. Tasks remain blocked until ready.',
  tool: 'An agent wants to invoke a tool that requires approval.',
  message: 'A message is held at a gated channel or node.',
  turn: 'A node requires approval before each turn.',
  question: 'An agent asked a human a question (human.ask).',
};

export function ApprovalsView({ onChanged }: { onChanged: () => void }) {
  const [status, setStatus] = useState<ApprovalStatus | ''>('pending');
  const list = useAsync(() => api.approvals({ status: status || undefined }), [status]);
  const reload = useThrottled(() => void list.reload(), 500);
  useEventStream({ prefixes: ['approval.'] }, reload);

  return (
    <div className="page approvals-page">
      <PageHeading title="Approvals" description="Review pending decisions and inspect previous outcomes.">
        <label>
          Show{' '}
          <select value={status} onChange={(e) => setStatus(e.target.value as ApprovalStatus | '')}>
            <option value="pending">pending</option>
            <option value="approved">approved</option>
            <option value="rejected">rejected</option>
            <option value="expired">expired</option>
            <option value="">all</option>
          </select>
        </label>
      </PageHeading>
      <ReviewsView />
      <ErrorBox error={list.error} />
      {(list.data ?? []).length === 0 && !list.loading && <Empty>Nothing waiting for a human.</Empty>}
      <div className="cards">
        {(list.data ?? []).map((a) => (
          <ApprovalCard
            key={a.id}
            a={a}
            onDone={() => {
              void list.reload();
              onChanged();
            }}
          />
        ))}
      </div>
    </div>
  );
}

export function ApprovalCard({ a, onDone, canDecide = true }: { a: Approval; onDone: () => void; canDecide?: boolean }) {
  const [comment, setComment] = useState('');
  const [answer, setAnswer] = useState('');
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);

  const decide = async (d: 'approve' | 'reject') => {
    setBusy(true);
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
      {a.status === 'pending' && !canDecide ? <p>Waiting for an authorized operator to decide.</p> : a.status === 'pending' ? (
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
