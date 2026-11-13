import { useRef, useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { EnvironmentCleanupItem, EnvironmentCleanupResult } from '../api/types';
import { Btn, ErrorBox } from './common';
import { canClean, CLEANUP_LIMIT, cleanupOutcome } from './sharedEnvironmentCleanup';

function Resources({ item }: { item: EnvironmentCleanupItem }) {
  return <>
    <code>{item.image || item.id}</code>
    <ul>{item.caches?.map(cache => <li key={cache.volume}><code>{cache.volume}</code> → {cache.path}</li>)}</ul>
  </>;
}

export function SharedEnvironmentsSection({ canManage }: { canManage: boolean }) {
  // Unmount privileged state when identity changes, including pending confirmation.
  return <section className="integration-section" aria-label="Shared environment resources">
    <h2>Shared images and caches</h2>
    <p className="muted">Inspect resources registered by Weft on the configured sandbox daemon. Workspaces and private caches are preserved.</p>
    {canManage ? <CleanupControls /> : <p>Administrator access is required to inspect and clean shared resources.</p>}
  </section>;
}

function CleanupControls() {
  const [items, setItems] = useState<EnvironmentCleanupItem[]>();
  const [deferred, setDeferred] = useState<EnvironmentCleanupResult["deferredGraphs"]>();
  const [selected, setSelected] = useState<string[]>([]);
  const [confirmation, setConfirmation] = useState<EnvironmentCleanupItem[]>();
  const [report, setReport] = useState<EnvironmentCleanupItem[]>();
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const locked = useRef(false);
  const reviewButton = useRef<HTMLButtonElement>(null);

  async function preview() {
    setItems(undefined);
    setDeferred(undefined);
    setSelected([]);
    try {
      const result = await api.previewEnvironmentCleanup();
      setItems(result.generations ?? []);
      setDeferred(result.deferredGraphs);
    } catch (e) {
      setError(previous => [previous, `Preview failed: ${errorMessage(e)}`].filter(Boolean).join(' '));
    }
  }

  async function refresh() {
    if (locked.current) return;
    locked.current = true;
    setBusy(true);
    setError(undefined);
    await preview();
    setBusy(false);
    locked.current = false;
  }

  async function apply() {
    if (locked.current || !confirmation?.length) return;
    locked.current = true;
    setBusy(true);
    setError(undefined);
    setReport(undefined);
    const ids = confirmation.map(item => item.id);
    try {
      const result = await api.cleanupEnvironments(ids);
      setReport(result.generations ?? []);
    } catch (e) {
      setError(`Cleanup could not be confirmed: ${errorMessage(e)}. Some resources may have been removed. Review the refreshed preview before retrying.`);
    } finally {
      setConfirmation(undefined);
      await preview();
      setBusy(false);
      locked.current = false;
      reviewButton.current?.focus();
    }
  }

  async function retryDeferred(graph: string) {
    if (locked.current || !confirm(`Remove the remaining environment resources for “${graph}”?`)) return;
    locked.current = true;
    setBusy(true);
    setError(undefined);
    try {
      await api.retryDeferredEnvironmentCleanup(graph);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      await preview();
      setBusy(false);
      locked.current = false;
    }
  }

  return <>
    <p className="muted small">Resources referenced by tasks, builds or containers are retained, including stopped and suspended sandboxes. Reset or replace an environment when it is no longer needed. Historical unregistered resources are not listed.</p>
    <Btn onClick={() => void refresh()} disabled={busy || !!confirmation}>{busy ? 'Checking resources…' : 'Refresh preview'}</Btn>
    <ErrorBox error={error} />
    {report && <div role="status" className="integration-note">
      <h3>Last cleanup result</h3>
      <ul>{report.map(item => <li key={item.id}><code>{item.id}</code>: {cleanupOutcome(item)}</li>)}</ul>
    </div>}
    {!!deferred?.length && <section aria-label="Deferred graph cleanup">
      <h3>Deferred graph cleanup</h3>
      <ul>{deferred.map(item => <li key={item.graph}>
        <code>{item.graph}</code>{' '}
        <Btn disabled={busy || !!confirmation} onClick={() => void retryDeferred(item.graph)}>
          Clean up {item.graph}
        </Btn>
      </li>)}</ul>
    </section>}
    {items && <>
      <p>{items.length} registered generations. Select up to {CLEANUP_LIMIT} unused generations.</p>
      {!items.length && <p>No registered shared resources found.</p>}
      <div className="integration-grid">{items.map(item => <article className="integration-card shared-resource" key={item.id}>
        <label><input type="checkbox" aria-label={`Select ${item.id}`} checked={selected.includes(item.id)}
          disabled={busy || !!confirmation || !canClean(item) || (selected.length >= CLEANUP_LIMIT && !selected.includes(item.id))}
          onChange={e => setSelected(previous => e.target.checked ? [...previous, item.id] : previous.filter(id => id !== item.id))} /> <strong>{item.id}</strong></label>
        <p>{cleanupOutcome(item)}</p>
        <Resources item={item} />
      </article>)}</div>
      <button type="button" className="btn btn-danger" ref={reviewButton} disabled={busy || !!confirmation || !selected.length}
        onClick={() => setConfirmation(items.filter(item => selected.includes(item.id) && canClean(item)))}>
        Review cleanup ({selected.length})
      </button>
    </>}
    {confirmation && <section className="integration-card shared-resource" aria-label="Confirm shared resource cleanup">
      <h3>Confirm cleanup of {confirmation.length} generations</h3>
      <p>The following images and shared caches will be deleted if they are still unused. Future tasks may need to rebuild them. The server rechecks usage before deletion.</p>
      <ul>{confirmation.map(item => <li key={item.id}><Resources item={item} /></li>)}</ul>
      <Btn autoFocus disabled={busy} onClick={() => { setConfirmation(undefined); reviewButton.current?.focus(); }}>Cancel cleanup</Btn>{' '}
      <Btn tone="danger" disabled={busy} onClick={() => void apply()}>{busy ? 'Cleaning…' : 'Confirm cleanup'}</Btn>
    </section>}
  </>;
}
