import { useEffect, useRef, useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { EnvironmentCleanupItem, EnvironmentCleanupResult } from '../api/types';
import { Btn, ErrorBox, fmtDateTime } from './common';
import {AdminHeader,AdminPages,AdminReview,adminPage,discardAdminDraft,useAdminDraft} from './AdministrationUI';
import {filterAdminResources} from './administrationData';
import { canClean, CLEANUP_LIMIT, cleanupOutcome } from './sharedEnvironmentCleanup';

function Resources({ item }: { item: EnvironmentCleanupItem }) {
  return <>
    <code>{item.image || item.id}</code>
    <ul>{item.caches?.map(cache => <li key={cache.volume}><code>{cache.volume}</code> → {cache.path}</li>)}</ul>
  </>;
}

export function SharedEnvironmentsSection({ canManage }: { canManage: boolean }) {
  // Unmount privileged state when identity changes, including pending confirmation.
  return <section className="integration-section admin-section" aria-label="Shared environment resources">
    {canManage ? <CleanupControls /> : <><AdminHeader title="Shared images and caches" description="Inspect resources registered by Weft on the configured sandbox daemon. Workspaces and private caches are preserved."/><p>Administrator access is required to inspect and clean shared resources.</p></>}
  </section>;
}

function CleanupControls() {
  const [query,setQuery]=useState('');const [status,setStatus]=useState('all');const [page,setPage]=useState(0);
  const [items, setItems] = useState<EnvironmentCleanupItem[]>();
  const [decision, setDecision] = useState<{ graph: string; graphExists: boolean }>();
  const [reason, setReason] = useState('');
  const [deferred, setDeferred] = useState<EnvironmentCleanupResult["deferredGraphs"]>();
  const [selected, setSelected] = useState<string[]>([]);
  const [confirmation, setConfirmation] = useState<EnvironmentCleanupItem[]>();
  const [report, setReport] = useState<EnvironmentCleanupItem[]>();
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [mutating,setMutating]=useState(false);
  const locked = useRef(false);
  const reviewButton = useRef<HTMLButtonElement>(null);

  useEffect(()=>{void refresh();},[]);
  useAdminDraft('resources',selected.length>0||!!decision,mutating);
  const rows=filterAdminResources(items??[],query,status);

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
    if (locked.current || !discardAdminDraft(selected.length>0)) return;
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
    setMutating(true);
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
      setMutating(false);
      locked.current = false;

    }
  }

  async function deferredAction(action: () => Promise<void>) {
    if (locked.current) return;
    setMutating(true);
    locked.current = true;
    setBusy(true);
    setError(undefined);
    try {
      await action();
      setDecision(undefined);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      await preview();
      setBusy(false);
      setMutating(false);
      locked.current = false;
    }
  }

  return <>
    <AdminHeader title="Shared images and caches" description="Inspect resources registered by Weft on the configured sandbox daemon. Workspaces and private caches are preserved."><Btn onClick={() => void refresh()} disabled={busy || !!confirmation || !!decision}>{busy ? 'Checking resources…' : 'Refresh preview'}</Btn></AdminHeader>
    <p className="muted small">Resources referenced by tasks, builds or containers are retained, including stopped and suspended sandboxes. Reset or replace an environment when it is no longer needed. Historical unregistered resources are not listed.</p>
    {busy&&!items&&<p role="status">Loading resource preview…</p>}
    <ErrorBox error={error} />
    {report && <div role="status" className="integration-note">
      <h3>Last cleanup result</h3>
      <ul>{report.map(item => <li key={item.id}><code>{item.id}</code>: {cleanupOutcome(item)}</li>)}</ul>
    </div>}
    {!!deferred?.length && <section className="card admin-card" aria-label="Deferred graph cleanup">
      <h3>Deferred graph cleanup</h3>
      <ul>{deferred.map(item => <li key={item.graph}>
        <strong>{item.graph}</strong>
        {item.invalid ? <p>The cleanup record is invalid. Review the possible resources before releasing the reservation.</p> : <>
          <p>Requested by {item.actor || 'Unknown'} · {fmtDateTime(item.since)}</p>
          <p>{item.reason || 'Deletion was requested with environment cleanup deferred.'}</p>
          {item.graphExists && <p>Deletion was interrupted. Finish deleting the graph or cancel the reservation to keep it.</p>}
          {!item.daemonMatches && <p>Cleanup requires the original sandbox daemon.</p>}
        </>}
        {item.possibleResources?.length ? <details>
          <summary>Resources that may remain</summary>
          <ul>{item.possibleResources.map(resource => <li key={resource}>{resource}</li>)}</ul>
        </details> : null}
        <Btn disabled={busy || !!confirmation || item.invalid || (!item.graphExists && !item.daemonMatches)} onClick={() => {
          const message = item.graphExists
            ? `Finish deleting “${item.graph}” and keep its cleanup deferred?`
            : `Remove the remaining environment resources for “${item.graph}”?`;
          if (confirm(message)) void deferredAction(() => item.graphExists
            ? api.deleteGraph(item.graph, true)
            : api.retryDeferredEnvironmentCleanup(item.graph));
        }}>
          {item.graphExists ? 'Finish deletion of' : 'Clean up'} {item.graph}
        </Btn>{' '}
        <Btn disabled={busy || !!confirmation} onClick={() => {
          setReason('');
          setDecision({ graph: item.graph, graphExists: item.graphExists });
        }}>
          {item.graphExists ? 'Cancel reservation for' : 'Abandon cleanup for'} {item.graph}
        </Btn>
      </li>)}</ul>
    </section>}
    {decision && <form aria-label="Release cleanup reservation" onSubmit={event => {
      event.preventDefault();
      if (reason.trim()) void deferredAction(() => api.abandonDeferredEnvironmentCleanup(decision.graph, reason));
    }}>
      <p>{decision.graphExists
        ? `Keep “${decision.graph}” and release its deletion reservation.`
        : `Release the ID “${decision.graph}”. Any remaining Docker resources will need manual cleanup.`}</p>
      <label>Reason <input required maxLength={500} value={reason} disabled={busy} onChange={event => setReason(event.target.value)} /></label>{' '}
      <Btn type="button" disabled={busy} onClick={() => setDecision(undefined)}>Back</Btn>{' '}
      <Btn type="submit" tone="danger" disabled={busy || !reason.trim()}>Release reservation</Btn>
    </form>}
    {items && <>
      <p>{items.length} registered generations. Select up to {CLEANUP_LIMIT} unused generations.</p>
      {!items.length && <p>No registered shared resources found.</p>}
      <div className="card admin-card"><div className="admin-filters"><label>Search resources<input type="search" value={query} placeholder="Generation, image, cache or usage" onChange={e=>{setQuery(e.target.value);setPage(0);}}/></label><label>Resource status<select value={status} onChange={e=>{setStatus(e.target.value);setPage(0);}}><option value="all">All statuses</option><option value="unused">Unused</option><option value="in-use">In use</option><option value="retained">Retained or absent</option><option value="error">Error</option></select></label><Btn onClick={()=>{setQuery('');setStatus('all');setPage(0);}}>Clear filters</Btn></div>
      <p className="muted small">{rows.length} of {items.length} generations · {selected.length} selected across all pages and filters</p>
      {selected.length>0&&<Btn disabled={busy||!!confirmation} onClick={()=>setSelected([])}>Clear selection</Btn>}
      {rows.length?<div className="admin-table-scroll" tabIndex={0} role="region" aria-label="Shared resources"><table className="table admin-resources-table"><thead><tr><th>Select</th><th>Generation</th><th>Image and shared caches</th><th>Status and retained usage</th></tr></thead><tbody>{adminPage(rows,page).map(item=><tr key={item.id}><td><input type="checkbox" aria-label={`Select ${item.id}`} checked={selected.includes(item.id)} disabled={busy||!!confirmation||!canClean(item)||(selected.length>=CLEANUP_LIMIT&&!selected.includes(item.id))} onChange={e=>setSelected(previous=>e.target.checked?[...previous,item.id]:previous.filter(id=>id!==item.id))}/></td><td><strong>{item.id}</strong></td><td><Resources item={item}/></td><td>{cleanupOutcome(item)}</td></tr>)}</tbody></table></div>:items.length>0&&<p className="empty">No resources match these filters.</p>}
      <AdminPages page={page} total={rows.length} onPage={setPage}/></div>
      <button type="button" className="btn btn-danger" ref={reviewButton} disabled={busy || !!confirmation || !selected.length}
        onClick={() => setConfirmation(items.filter(item => selected.includes(item.id) && canClean(item)))}>
        Review cleanup ({selected.length})
      </button>
    </>}
    {confirmation && <AdminReview returnFocus={reviewButton} label="Confirm shared resource cleanup">
      <h3>Confirm cleanup of {confirmation.length} generations</h3>
      <p>The following images and shared caches will be deleted if they are still unused. Future tasks may need to rebuild them. The server rechecks usage before deletion.</p>
      <ul>{confirmation.map(item => <li key={item.id}><Resources item={item} /></li>)}</ul>
      <Btn autoFocus disabled={busy} onClick={() => { setConfirmation(undefined);  }}>Cancel cleanup</Btn>{' '}
      <Btn tone="danger" disabled={busy} onClick={() => void apply()}>{busy ? 'Cleaning…' : 'Confirm cleanup'}</Btn>
    </AdminReview>}
  </>;
}
