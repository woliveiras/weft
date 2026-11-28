import { useState } from 'react';
import type { ChatPRDraftComment, ChatPRPublicationReview } from '../api/chat';
import { Btn } from './common';

const anchor = (c: ChatPRDraftComment) => `${c.path}, ${c.side === 'RIGHT' ? 'added' : 'removed'} line ${c.line}`;

export function ChatPRCommentSelection({ comments, enabled, onPrepare, unavailableExplained }: {
  comments: ChatPRDraftComment[]; enabled: boolean; unavailableExplained?: boolean; onPrepare?: (ids: number[]) => void;
}) {
  const [selected, setSelected] = useState<number[]>([]);
  return <fieldset className="chat-pr-comments">
    <legend>Draft line comments</legend>
    {!onPrepare && !unavailableExplained && <p>Line comment publication is unavailable for this pull request.</p>}
    {comments.map(comment => <div key={comment.id}>
      {onPrepare ? <label><input type="checkbox" checked={selected.includes(comment.id)} disabled={!enabled}
        onChange={event => {
          const checked = event.currentTarget.checked;
          setSelected(previous => checked ? [...previous, comment.id] : previous.filter(id => id !== comment.id));
        }}/>{' '}{comment.id}. {anchor(comment)}</label> : <strong>{comment.id}. {anchor(comment)}</strong>}
      <p>{comment.body}</p>
    </div>)}
    {onPrepare && <Btn disabled={!enabled || selected.length === 0} onClick={() => onPrepare(selected)}>Review selected comments</Btn>}
  </fieldset>;
}

const failureCode: Record<string,string> = {
  provider_rejected: 'The provider refused this comment.',
  source_unavailable: 'The PR could not be verified before publication.',
  source_changed: 'The PR changed before publication. Request a fresh analysis.',
  source_or_grant_changed: 'The reviewed source or grant is no longer current.',
  not_attempted: 'A publication attempt was not recorded.',
  tool_denied: 'The saved tool policy prevented publication.',
};

export function ChatPRPublicationCard({ review, now, canConfirm, onDecide, onReconcile }: {
  review: ChatPRPublicationReview; now: number; canConfirm: boolean;
  onDecide: (decision: 'confirm' | 'decline') => void; onReconcile: () => void;
}) {
  const { state, prPublication: { analysis, comments }, prReceipts } = review;
  const reconcile = state === 'unknown' || prReceipts?.some(r => r.state === 'unknown');
  return <section className="chat-agent-review" aria-label={`Publish comments on PR #${analysis.number}`}>
    <h3>Publish comments on PR #{analysis.number}</h3>
    <p>{analysis.repository} · {analysis.provider} · {analysis.connection}</p>
    <dl>
      <dt>Source</dt><dd><code>{analysis.head}</code></dd>
      <dt>Base</dt><dd><code>{analysis.base}</code></dd>
      <dt>Reviewer</dt><dd>{analysis.graph} · Version {analysis.version} · {analysis.node}</dd>
    </dl>
    <ol className="chat-pr-comments">{comments.map((comment, i) => {
      const receipt = prReceipts?.[i];
      return <li key={comment.id} value={comment.id}>
        <strong>{anchor(comment)}</strong><p>{comment.body}</p>
        {receipt?.state === 'published' && <p>Published · Provider comment {receipt.id}</p>}
        {receipt?.state === 'failed' && <p>Failed · {failureCode[receipt.code ?? ''] ?? 'Publication was refused.'}</p>}
        {receipt?.state === 'unknown' && <p>Unknown · Publication receipt is unconfirmed.</p>}
      </li>;
    })}</ol>
    {state === 'awaiting_confirmation' && (Date.parse(review.expiresAt) <= now
      ? <p role="status">This confirmation expired. Review the selection again.</p>
      : <><p>Confirm to publish exactly these comments at the reviewed source revision.</p>
        <Btn tone="primary" disabled={!canConfirm} onClick={() => onDecide('confirm')}>Publish {comments.length === 1 ? '1 comment' : `${comments.length} comments`}</Btn>{' '}
        <Btn disabled={!canConfirm} onClick={() => onDecide('decline')}>Decline publication</Btn></>)}
    {state === 'applying' && <p role="status">Checking the reviewed source and publishing the selected comments…</p>}
    {state === 'conflict' && <p role="status">The source or grant changed. Request a fresh analysis before publishing.</p>}
    {state === 'declined' && <p role="status">Publication declined.</p>}
    {reconcile && <Btn disabled={!canConfirm} onClick={() => onReconcile()}>Reconcile comment receipts</Btn>}
  </section>;
}
