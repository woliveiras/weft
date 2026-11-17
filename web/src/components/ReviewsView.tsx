import { useEffect, useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { HumanReview } from '../api/types';
import { useAsync } from '../hooks/useData';
import { Badge, Btn, ErrorBox, fmtDateTime } from './common';

export function ReviewsView() {
  const list = useAsync(() => api.reviews(), []);
  useEffect(() => {
    const timer = window.setInterval(() => void list.reload(), 3000);
    return () => window.clearInterval(timer);
  }, [list.reload]);
  return <section aria-label="Application reviews">
    <h2>Application reviews</h2>
    <ErrorBox error={list.error}/>
    <div className="cards">
      {(list.data ?? []).map(r => <ReviewCard key={r.id} review={r} reload={() => void list.reload()}/>)}
    </div>
  </section>;
}

export function ReviewCard({ review: r, reload }: { review: HumanReview; reload: () => void }) {
  const [method, setMethod] = useState('managed');
  const [comment, setComment] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [credential, setCredential] = useState<string>();
  const [logs, setLogs] = useState<string>();
  useEffect(() => { setCredential(undefined); }, [r.generation, r.state]);

  async function run(work: () => Promise<unknown>) {
    setBusy(true);
    setError(undefined);
    try {
      await work();
      reload();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function openPreview() {
    const popup = window.open(`${r.url}/_weft/connect`, '_blank');
    if (!popup) throw new Error('Allow a popup to open this preview.');
    try {
      const ready = new Promise<void>((resolve, reject) => {
        const timer = window.setTimeout(() => {
          window.removeEventListener('message', listener);
          reject(new Error('Preview connection timed out'));
        }, 15000);
        function listener(event: MessageEvent) {
          if (event.source === popup && event.origin === r.url && event.data?.type === 'weft-preview-ready') {
            window.clearTimeout(timer);
            window.removeEventListener('message', listener);
            resolve();
          }
        }
        window.addEventListener('message', listener);
      });
      const [access] = await Promise.all([api.previewCredential(r.id, true), ready]);
      popup.postMessage({ type: 'weft-preview-access', token: access.token }, access.url);
    } catch (e) {
      popup.close();
      throw e;
    }
  }

  const pending = r.state === 'pending';
  const ready = pending && r.session === 'ready';
  return <article className="card">
    <header><strong>{r.request.title}</strong> <Badge>{r.state}</Badge> <Badge>{r.session}</Badge></header>
    <p className="muted small">{r.graphId} / {r.nodeId} · Candidate {r.candidate.slice(0, 12)} · Preview generation {r.generation}</p>
    <p style={{ whiteSpace: 'pre-wrap' }}>{r.request.instructions}</p>
    {!!r.request.checks?.length && <details>
      <summary>Agent-reported checks</summary>
      <ul>{r.request.checks.map((check, i) => <li key={i}>{check}</li>)}</ul>
    </details>}
    {r.request.externalUrl && <p>
      <a href={r.request.externalUrl} target="_blank" rel="noopener noreferrer">External review environment (unverified)</a>
    </p>}
    {r.session !== 'none' && <>
      <p className="small">Preview expires {fmtDateTime(r.sessionExpires)}</p>
      <code>{r.url}</code>
      <div className="btn-row">
        <Btn disabled={busy || !ready} onClick={() => void run(openPreview)}>Open preview</Btn>
        <Btn disabled={busy || !ready} onClick={() => void run(async () => {
          const result = await api.previewCredential(r.id, false);
          setCredential(result.token);
        })}>Generate HTTP client credential</Btn>
        <Btn disabled={busy || !ready} onClick={() => void run(() => api.reviewAction(r.id, 'renew'))}>Extend preview</Btn>
        <Btn disabled={busy || !pending || !['failed', 'expired', 'stopped'].includes(r.session)}
          onClick={() => void run(() => api.reviewAction(r.id, 'restart'))}>Restart preview</Btn>
        <Btn disabled={busy || !pending} onClick={() => void run(async () => {
          await api.reviewAction(r.id, 'revoke-access');
          setCredential(undefined);
        })}>Revoke preview access</Btn>
        <Btn disabled={busy} onClick={() => void run(async () => {
          setLogs((await api.previewLogs(r.id)).logs);
        })}>Application logs</Btn>
      </div>
      <p className="small">For cURL or Postman, supply X-Weft-Preview-Token separately from the application's Authorization header. Credentials expire after 15 minutes or when review access ends.</p>
      <pre>{`curl '${r.url}/' -H "X-Weft-Preview-Token: $WEFT_PREVIEW_TOKEN"`}</pre>
      {credential && <label>Preview credential (keep private)
        <input readOnly type="password" aria-label="Preview credential" value={credential} onFocus={e => e.target.select()}/>
        <Btn onClick={() => setCredential(undefined)}>Hide credential</Btn>
      </label>}
      {logs !== undefined && <details open><summary>Application logs</summary><pre>{logs}</pre></details>}
    </>}
    {r.error && <p role="alert">{r.error}</p>}
    {pending ? <div className="form">
      <label>Review method<select value={method} onChange={e => setMethod(e.target.value)}>
        <option value="managed">Tested the Weft preview</option>
        <option value="external">Tested an external environment</option>
        <option value="local">Tested the branch locally</option>
        <option value="without_execution">Accept without manual execution</option>
      </select></label>
      <label>Review feedback<textarea value={comment} onChange={e => setComment(e.target.value)}/></label>
      <div className="btn-row">
        {(['approved', 'changes_requested', 'rejected'] as const).map(state => <Btn key={state} disabled={busy}
          onClick={() => void run(() => api.reviewAction(r.id, 'decide', { state, method, comment, revision: r.revision }))}>
          {state === 'approved' ? 'Approve candidate' : state === 'changes_requested' ? 'Request changes' : 'Reject'}
        </Btn>)}
      </div>
    </div> : <p>Decision by {r.decidedBy || 'system'} · {r.method} · {r.comment}</p>}
    <ErrorBox error={error}/>
  </article>;
}
