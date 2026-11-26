import type { ChatPRReview } from '../api/chat';
import { Btn } from './common';
import { nodeLink } from './activityView';
import { runLink } from './runPresentation';

export function ChatPRCard({ review, now, canConfirm, onDecide, onView }: {
  review: ChatPRReview; now: number; canConfirm: boolean;
  onDecide: (decision: 'confirm' | 'decline') => void;
  onView: (anchor: string) => void;
}) {
  const { pr, state } = review;
  const anchor = `chat-pr-${review.id}`;
  return <section className="chat-agent-review" aria-label={`Review PR #${pr.number}`}>
    <h3>Review PR #{pr.number}</h3>
    <p>{pr.repository} · {pr.provider} · {pr.connection}</p>
    <dl>
      <dt>Source</dt><dd><code>{pr.head}</code></dd>
      <dt>Base</dt><dd><code>{pr.base}</code></dd>
      <dt>Reviewer</dt><dd>{pr.graph} · Version {pr.version} · {pr.node}</dd>
      <dt>Model</dt><dd>{pr.modelProvider} / {pr.modelName}</dd>
    </dl>
    {state === 'awaiting_confirmation' && <>
      <p>The workflow sends the reviewed diff to this model and charges its budget. The draft appears here.</p>
      {Date.parse(review.expiresAt) <= now
        ? <p role="status">The confirmation expired. Request a fresh review.</p>
        : <><Btn tone="primary" disabled={!canConfirm} onClick={() => onDecide('confirm')}>Analyze PR #{pr.number}</Btn>{' '}
          <Btn disabled={!canConfirm} onClick={() => onDecide('decline')}>Decline</Btn></>}
    </>}
    {state === 'applying' && <p role="status">Checking the PR revisions and dispatching the review…</p>}
    {state === 'queued' && <p role="status">Review dispatched. Waiting for the workflow result.</p>}
    {state === 'applied' && <><h4>Draft review</h4><p className="run-summary">{review.prSummary}</p></>}
    {state === 'incomplete' && <p role="status">The workflow could not complete this review.</p>}
    {state === 'unknown' && <p role="status">The outcome is unknown. Inspect the workflow history before requesting another review.</p>}
    {state === 'conflict' && <p role="status">The destination changed or the pinned evidence is unavailable. Request a fresh review.</p>}
    {state === 'declined' && <p role="status">Proposal declined.</p>}
    <a href={nodeLink(pr.graph, pr.node)} id={anchor} onClick={() => onView(anchor)}>View workflow</a>
    {review.resultId && <> · <a href={runLink(pr.graph, review.resultId)} id={anchor + '-run'} onClick={() => onView(anchor + '-run')}>View execution</a></>}
  </section>;
}
