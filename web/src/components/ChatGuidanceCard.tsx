import type { ChatGuidanceReview } from '../api/chat';
import { Btn } from './common';

export function ChatGuidanceCard({review, now, canConfirm, onDecide}: {
  review: ChatGuidanceReview; now: number; canConfirm: boolean;
  onDecide: (decision: 'confirm' | 'decline') => void;
}) {
  const {guidance: g, state} = review;
  return <section className="chat-agent-review" aria-label="One-time agent guidance">
    <h3>One-time guidance</h3>
    <dl><dt>Workflow</dt><dd>{g.graph} · Version {g.version}</dd><dt>Run</dt><dd>{g.run}</dd>
      <dt>Node</dt><dd>{g.node}</dd><dt>Replica</dt><dd>{g.replica}</dd></dl>
    <blockquote>{g.text}</blockquote>
    {state === 'awaiting_confirmation' && <>
      <p>This message is visible in the node's operator chat. A matching replica can pick it up between turns of this run. Sending it wakes that replica and withdraws its pending stop declaration.</p>
      {now >= Date.parse(review.expiresAt) ? <p role="status">Confirmation expired. Request fresh guidance.</p> : <>
        <Btn tone="primary" disabled={!canConfirm} onClick={() => onDecide('confirm')}>Send guidance</Btn>{' '}
        <Btn disabled={!canConfirm} onClick={() => onDecide('decline')}>Decline</Btn></>}
    </>}
    {state === 'applying' && <p role="status">Checking delivery…</p>}
    {state === 'queued' && <p role="status">Queued for a matching turn.</p>}
    {state === 'consumed' && <p role="status">The runtime picked up the message{review.consumedAt && <> at <time dateTime={review.consumedAt}>{new Date(review.consumedAt).toLocaleTimeString()}</time></>}. The receipt records pickup only.</p>}
    {state === 'run_ended' && <p role="status">The selected run ended before pickup.</p>}
    {state === 'unknown' && <p role="status">Delivery is temporarily unknown. Inspect the node's operator chat.</p>}
    {state === 'conflict' && <p role="status">The target changed. Request fresh guidance for the current run.</p>}
    {state === 'declined' && <p role="status">Proposal declined.</p>}
  </section>;
}
