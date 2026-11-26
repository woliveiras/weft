import { useRef, useState } from 'react';
import type { ChatPRReview } from '../api/chat';
import { api, errorMessage } from '../api/client';
import { Btn, ErrorBox } from './common';
import { nodeLink } from './activityView';
import { runLink } from './runPresentation';
import { ChatPRCommentSelection } from './ChatPRPublicationCard';

export function ChatPRCard({ sessionId, review, now, canConfirm, onDecide, onView, onPrepare }: {
  sessionId: string; review: ChatPRReview; now: number; canConfirm: boolean;
  onDecide: (decision: 'confirm' | 'decline') => void;
  onView: (anchor: string) => void;
  onPrepare?: (ids: number[]) => void;
}) {
  const { pr, state } = review;
  const anchor = `chat-pr-${review.id}`;
  const [diff, setDiff] = useState<{state:'loading' | 'ready' | 'error'; text?:string}>();
  const diffRequest = useRef(0);
  const loadDiff = async () => {
    const id = ++diffRequest.current;
    setDiff({state:'loading'});
    try {
      const result = await api.chatPRDiff(sessionId, review.id);
      if (result.head !== pr.head || result.base !== pr.base) throw new Error('The returned diff did not match the reviewed revisions.');
      if (id === diffRequest.current) setDiff({state:'ready',text:result.diff});
    } catch (err) {
      if (id === diffRequest.current) setDiff({state:'error',text:errorMessage(err)});
    }
  };
  const toggleDiff = () => {
    if (!diff) { void loadDiff(); return; }
    diffRequest.current++;
    setDiff(undefined);
  };
  return <section className="chat-agent-review" aria-label={`Review PR #${pr.number}`}>
    <h3>Review PR #{pr.number}</h3>
    <p>{pr.repository} · {pr.provider} · {pr.connection}</p>
    <dl>
      <dt>Source</dt><dd><code>{pr.head}</code></dd>
      <dt>Base</dt><dd><code>{pr.base}</code></dd>
      <dt>Reviewer</dt><dd>{pr.graph} · Version {pr.version} · {pr.node}</dd>
      <dt>Model</dt><dd>{pr.modelProvider} / {pr.modelName}</dd>
    </dl>
    {state !== 'conflict' && state !== 'declined' && <>
      <Btn aria-expanded={!!diff} aria-controls={diff ? anchor + '-diff' : undefined}
        onClick={toggleDiff}>{diff ? 'Hide pinned diff' : 'Inspect pinned diff'}</Btn>
      {diff && <section id={anchor + '-diff'} aria-label="Pinned pull request diff">
        {diff.state === 'loading' && <p role="status">Loading pinned diff…</p>}
        {diff.state === 'error' && <><ErrorBox error={diff.text}/><Btn onClick={() => void loadDiff()}>Retry diff</Btn></>}
        {diff.state === 'ready' && <pre className="chat-pr-diff" tabIndex={0}>{formatDiff(diff.text ?? '')}</pre>}
      </section>}
    </>}
    {state === 'awaiting_confirmation' && <>
      <p>The workflow sends the reviewed diff to this model and charges its budget. The draft appears here.</p>
      {Date.parse(review.expiresAt) <= now
        ? <p role="status">The confirmation expired. Request a fresh review.</p>
        : <><Btn tone="primary" disabled={!canConfirm} onClick={() => onDecide('confirm')}>Analyze PR #{pr.number}</Btn>{' '}
          <Btn disabled={!canConfirm} onClick={() => onDecide('decline')}>Decline</Btn></>}
    </>}
    {state === 'applying' && <p role="status">Checking the PR revisions and dispatching the review…</p>}
    {state === 'queued' && <p role="status">Review dispatched. Waiting for the workflow result.</p>}
    {state === 'applied' && <><h4>Draft review</h4><p className="run-summary">{review.prSummary}</p>
      {!!review.prComments?.length && <ChatPRCommentSelection comments={review.prComments} enabled={canConfirm}
        onPrepare={pr.provider === 'github' && !pr.draft ? onPrepare : undefined}/>}
    </>}
    {state === 'incomplete' && <p role="status">The workflow could not complete this review.</p>}
    {state === 'unknown' && <p role="status">The outcome is unknown. Inspect the workflow history before requesting another review.</p>}
    {state === 'conflict' && <p role="status">The destination changed or the pinned evidence is unavailable. Request a fresh review.</p>}
    {state === 'declined' && <p role="status">Proposal declined.</p>}
    <a href={nodeLink(pr.graph, pr.node)} id={anchor} onClick={() => onView(anchor)}>View workflow</a>
    {review.resultId && <> · <a href={runLink(pr.graph, review.resultId)} id={anchor + '-run'} onClick={() => onView(anchor + '-run')}>View execution</a></>}
  </section>;
}

function formatDiff(diff: string) {
  if (!diff.startsWith('{')) return diff;
  try { return JSON.stringify(JSON.parse(diff), null, 2); }
  catch { return diff; }
}
