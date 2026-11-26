import type { ChatConfigurationReview } from '../api/chat';
import { Btn } from './common';

export function ChatConfigurationCard({review, now, canConfirm, onDecide}: {
  review: ChatConfigurationReview;
  now: number;
  canConfirm: boolean;
  onDecide: (decision: 'confirm' | 'decline') => void;
}) {
  const config = review.configuration;
  const expired = now >= Date.parse(review.expiresAt);
  return <section className="chat-agent-review" aria-label="Installation configuration review">
    <h3>Installation configuration</h3>
    {config.objects.map(object => <div key={object.key}>
      <h4>{object.key}</h4>
      <p>{object.action} · Revision <code>{object.revision}</code></p>
      {object.changes.length > 0 && <ul>{object.changes.map(change =>
        <li key={change.path}><code>{change.path}</code>{' '}
          <code>{JSON.stringify(change.before) ?? 'absent'}</code> → <code>{JSON.stringify(change.after) ?? 'absent'}</code></li>
      )}</ul>}
      {object.consent && <p>{object.consent}</p>}
      {object.warning && <p>{object.warning}</p>}
    </div>)}
    <details><summary>Proposed configuration</summary><pre>{config.document}</pre></details>
    {review.state === 'awaiting_confirmation' && (expired
      ? <p role="status">Confirmation expired. Request a fresh installation plan.</p>
      : <><p>Review expires at <time dateTime={review.expiresAt}>{new Date(review.expiresAt).toLocaleTimeString()}</time>.</p>
        <Btn tone="primary" disabled={!canConfirm} onClick={() => onDecide('confirm')}>
          {config.intakeConsent ? 'Consent to intake and apply configuration' : 'Apply configuration'}
        </Btn> <Btn disabled={!canConfirm} onClick={() => onDecide('decline')}>Decline</Btn></>)}
    {review.state === 'applying' && <p role="status">Applying the reviewed configuration…</p>}
    {review.state === 'applied' && <p role="status">Configuration applied.</p>}
    {review.state === 'incomplete' && <p role="status">Configuration stopped before completing the plan. Check each object before requesting a fresh plan.</p>}
    {review.state === 'unknown' && <p role="status">Installation outcome unknown. Inspect the current installation before requesting a fresh plan.</p>}
    {review.state === 'conflict' && <p role="status">The installation changed. Request a fresh plan.</p>}
    {review.state === 'declined' && <p role="status">Proposal declined.</p>}
    {review.configReceipts && <ul aria-label="Configuration results">{review.configReceipts.map(receipt =>
      <li key={receipt.key}>{receipt.key} · {receipt.result}{receipt.revision && <> · <code>{receipt.revision}</code></>}</li>
    )}</ul>}
  </section>;
}
