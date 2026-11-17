import { useRef, useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { HistoryRetentionPolicy, HistoryRetentionView } from '../api/types';
import { useAsync } from '../hooks/useData';
import { Btn, ErrorBox } from './common';

export function HistoryRetentionSection({ canManage }: { canManage: boolean }) {
  return <section className="integration-section" aria-label="Blocked history retention">
    <h2>Blocked access history retention</h2>
    <RetentionControls key={String(canManage)} canManage={canManage} />
  </section>;
}
function RetentionControls({ canManage }: { canManage: boolean }) {
  const current = useAsync(api.historyRetention, []);
  const [draft, setDraft] = useState<HistoryRetentionPolicy>();
  const [review, setReview] = useState<HistoryRetentionView>();
  const [busy, setBusy] = useState(false);
  const locked = useRef(false);
  const [error, setError] = useState<string>();
  const [notice, setNotice] = useState<string>();
  const policy = draft ?? current.data?.policy;
  const disabled = !canManage || busy || current.loading || !!current.error || !!review;
  const valid = policy && Number.isInteger(policy.days) && policy.days >= 1 && policy.days <= 90 && Number.isInteger(policy.maxEvents) && policy.maxEvents >= 1 && policy.maxEvents <= 10000;
  async function save() {
    if (!canManage || !review || locked.current) return;
    locked.current = true; setBusy(true); setError(undefined); setNotice(undefined);
    try {
      await api.saveHistoryRetention(review.policy, review.revision);
      setDraft(undefined);
      setNotice('Retention saved. New limits apply on subsequent history reads and collection cycles.');
    } catch (e) {
      setError(`Save could not be confirmed: ${errorMessage(e)}. Reload and review the current limits before retrying.`);
      setDraft(undefined);
    } finally {
      setReview(undefined); await current.reload(); locked.current = false; setBusy(false);
    }
  }
  return <>
    <p>Installation-wide limits apply separately to every environment. Defaults: 7 days and 1,000 blocks.</p>
    <Btn disabled={busy || current.loading || !!review} onClick={() => { setDraft(undefined); setError(undefined); setNotice(undefined); void current.reload(); }}>Reload retention limits</Btn>
    <ErrorBox error={current.error ?? error} />
    {notice && <p role="status">{notice}</p>}
    {current.loading && <p>Loading retention limits…</p>}
    {!canManage && <p>Administrator access is required to change retention.</p>}
    {!current.error && current.data && policy && <>
      <p>Revision {current.data.revision}{current.data.updatedBy ? ` · Updated by ${current.data.updatedBy}` : ''}</p>
      <label>Retention days (1–90)<input type="number" min={1} max={90} step={1} disabled={disabled} value={policy.days || ''} onChange={event => { setNotice(undefined); setDraft({ ...policy, days: Number(event.target.value) }); }} /></label>
      <label>Maximum blocks per environment (1–10,000)<input type="number" min={1} max={10000} step={1} disabled={disabled} value={policy.maxEvents || ''} onChange={event => { setNotice(undefined); setDraft({ ...policy, maxEvents: Number(event.target.value) }); }} /></label>
      {canManage && <Btn disabled={disabled || !valid} onClick={() => { if (!current.data) return; setError(undefined); setNotice(undefined); setReview({ ...current.data, policy: { ...policy } }); }}>Review retention changes</Btn>}
    </>}
    {review && <section className="integration-card" aria-label="Confirm history retention">
      <h3>Review retention changes</h3>
      <p>Keep up to {review.policy.maxEvents} blocks per environment for {review.policy.days} days.</p>
      <p>Reducing limits permanently discards older or excess history on subsequent reads and collection cycles, including histories of deleted environments. Increasing limits does not restore discarded history. In-flight collection may finish using previous limits; the next cycle applies the new limits.</p>
      <Btn disabled={busy} onClick={() => void save()}>Confirm retention changes</Btn>
      <Btn disabled={busy} onClick={() => setReview(undefined)}>Cancel retention changes</Btn>
    </section>}
  </>;
}
