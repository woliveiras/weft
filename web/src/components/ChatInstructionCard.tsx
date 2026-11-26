import type { ChatInstructionReview } from '../api/chat';
import { nodeLink } from './activityView';
import { Btn } from './common';

export function ChatInstructionCard({review, now, canConfirm, onDecide, onView}: {
  review: ChatInstructionReview; now: number; canConfirm: boolean;
  onDecide: (decision: 'confirm' | 'decline') => void; onView: (anchor: string) => void;
}) {
  const change=review.instructions;
  const anchor=`chat-workflow-instructions-${review.id}`;
  return <section className="chat-agent-review" aria-label="Workflow-local instructions">
    <h3>Workflow-local instructions</h3>
    <p>{change.graph} · Version {change.version} · Node {change.node}</p>
    {change.catalogId && <p>Catalog agent {change.catalogId} · Version {change.catalogVersion}</p>}
    <dl><dt>Current instructions</dt><dd>{change.before || 'None'}</dd><dt>Proposed instructions</dt><dd>{change.after || 'None'}</dd></dl>
    {review.state === 'awaiting_confirmation' && <>
      <p>Saving creates a workflow revision. Running turns and existing task copies keep their recorded inputs. Future turns of this workflow use the revised local instructions.</p>
      {now >= Date.parse(review.expiresAt) ? <p role="status">Confirmation expired. Request a fresh change.</p> : <>
        <Btn disabled={!canConfirm} tone="primary" onClick={() => onDecide('confirm')}>Save workflow instructions</Btn>{' '}
        <Btn disabled={!canConfirm} onClick={() => onDecide('decline')}>Decline</Btn></>}
    </>}
    {review.state === 'applied' && <p role="status">Workflow version {review.resultVersion} saved.</p>}
    {review.state === 'declined' && <p role="status">Proposal declined.</p>}
    {review.state === 'applying' && <p role="status">Saving the workflow revision…</p>}
    {review.state === 'conflict' && <p role="status">The workflow changed. Request a fresh review.</p>}
    {review.state === 'unknown' && <p role="status">The save outcome is unknown. Inspect the workflow before requesting a fresh review.</p>}
    <a id={anchor} onClick={() => onView(anchor)} href={nodeLink(change.graph, change.node)}>View workflow</a>
  </section>;
}
