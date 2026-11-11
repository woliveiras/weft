import { useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { Graph, Trigger, TriggerInput, TriggerMatches } from '../api/types';
import { useAsync } from '../hooks/useData';
import { Badge, Btn, ErrorBox, fmtDateTime } from './common';

const modes: Record<TriggerInput['authorizationMode'], string> = {
  ask: 'Ask — every gated action waits for a person',
  workspace: 'Workspace — act inside the task worktree',
  full: 'Full access',
};

function emptyInput(graph: Graph): TriggerInput {
  const first = graph.spec.nodes.find((n) => !n.disabled)?.id ?? '';
  return { name: '', workflow: graph.metadata.id, nodeId: first, resultNodeId: '', source: { kind: 'github', connectionId: '', repository: '' }, label: 'weft', owner: '', intervalMinutes: 15, authorizationMode: 'ask' };
}

/** Issue triggers of a saved workflow: labelled issues start tasks under the consent of the administrator who saves or resumes them. */
export function TriggersSection({ graph, role }: { graph: Graph; role?: string }) {
  const list = useAsync(() => api.triggers(graph.metadata.id), [graph.metadata.id]);
  const scm = useAsync(() => api.scmConnections().catch(() => []), []);
  const [draft, setDraft] = useState<TriggerInput>();
  const [error, setError] = useState<string>();
  const [matches, setMatches] = useState<Record<string, TriggerMatches | string>>({});
  const admin = role === 'admin';
  const canPreview = admin || role === 'operator';
  const nodes = graph.spec.nodes.filter((n) => !n.disabled);
  const set = (patch: Partial<TriggerInput>) => setDraft((d) => ({ ...(d ?? emptyInput(graph)), ...patch }));
  const setSource = (patch: Partial<TriggerInput['source']>) => setDraft((d) => {
    const cur = d ?? emptyInput(graph);
    return { ...cur, source: { ...cur.source, ...patch } };
  });
  const save = async () => {
    if (!draft) return;
    setError(undefined);
    try {
      const src = draft.source.kind === 'github'
        ? { kind: draft.source.kind, connectionId: draft.source.connectionId.trim(), repository: draft.source.repository?.trim(), query: draft.source.query?.trim() || undefined }
        : { kind: draft.source.kind, connectionId: draft.source.connectionId.trim(), cloudId: draft.source.cloudId?.trim(), query: draft.source.query?.trim() || undefined };
      await api.createTrigger({ ...draft, source: src, resultNodeId: draft.resultNodeId || undefined, owner: draft.owner?.trim() || undefined, scmConnection: draft.scmConnection || undefined });
      setDraft(undefined);
      await list.reload();
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  const act = async (t: Trigger, action: 'pause' | 'resume' | 'delete') => {
    setError(undefined);
    if (action === 'delete' && !confirm(`Delete the trigger "${t.name}"? Running tasks continue.`)) return;
    try {
      if (action === 'delete') await api.deleteTrigger(t.id);
      else await api.triggerAction(t.id, action);
      await list.reload();
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  const preview = async (t: Trigger) => {
    try {
      const m = await api.triggerMatches(t.id);
      setMatches((cur) => ({ ...cur, [t.id]: m.error ? `Preview failed: ${m.error}` : m }));
    } catch (e) {
      setMatches((cur) => ({ ...cur, [t.id]: `Preview failed: ${errorMessage(e)}` }));
    }
  };
  return (
    <fieldset aria-label="Issue triggers">
      <legend>Issue triggers</legend>
      <p className="muted tiny">
        A trigger polls an issue tracker through an MCP connection and starts one task at a time for issues with its label, without a person present. It writes progress back to the issue and pauses on failure or when the workflow changes.
      </p>
      <ErrorBox error={list.error || error} />
      {(list.data ?? []).map((t) => {
        const m = matches[t.id];
        return (
          <article key={t.id} className="card" aria-label={`Trigger ${t.name}`}>
            <div className="integration-card-head">
              <strong>{t.name}</strong>
              <span className="integration-card-status">
                <Badge tone={t.state === 'active' ? 'ok' : 'warn'}>{t.state === 'active' ? 'Active' : t.state === 'paused' ? 'Paused' : 'Finished'}</Badge>
              </span>
            </div>
            <p className="small">
              {t.source.kind === 'github' ? `GitHub ${t.source.repository}` : 'Jira'} via {t.source.connectionId} · label <code>{t.label}</code> · every {t.intervalMinutes} min · {modes[t.authorizationMode].split(' — ')[0]}
              {t.scmConnection && ` · hands off to ${t.scmConnection}`}
            </p>
            <p className="small muted">Runs as {t.consent.actor} for workflow version {t.consent.workflowVersion} (consent recorded {fmtDateTime(t.consent.recordedAt)}).</p>
            {t.pausedReason && <p role="status">{t.pausedReason}</p>}
            {t.lastError && <p className="tone-bad small">Last run failed: {t.lastError}</p>}
            <p className="small">
              {t.lastRunAt ? `Last run ${fmtDateTime(t.lastRunAt)}` : 'Not run yet'}
              {t.nextRunAt && t.state === 'active' && ` · next ${fmtDateTime(t.nextRunAt)}`}
            </p>
            {t.currentTask && <p className="small">Working on {t.currentIssue?.key}: <a href={`#/tasks/${encodeURIComponent(t.currentTask)}`}>open task</a></p>}
            {(t.skipped ?? []).length > 0 && (
              <details>
                <summary>{t.skipped!.length} skipped issues</summary>
                <ul>{t.skipped!.map((s) => <li key={s.key}>{s.key} {s.title && `— ${s.title}`}: {s.reason}</li>)}</ul>
              </details>
            )}
            {(t.notes ?? []).length > 0 && <ul aria-label="Trigger notes">{t.notes!.map((n, i) => <li key={i} className="small">{n}</li>)}</ul>}
            <div className="row">
              {canPreview && <Btn small onClick={() => void preview(t)}>Preview matching issues</Btn>}
              {admin && t.state === 'active' && <Btn small onClick={() => void act(t, 'pause')}>Pause</Btn>}
              {admin && t.state === 'paused' && <Btn small tone="primary" onClick={() => void act(t, 'resume')}>Resume and record consent</Btn>}
              {admin && <Btn small tone="danger" onClick={() => void act(t, 'delete')}>Delete</Btn>}
            </div>
            {typeof m === 'string' && <p role="alert">{m}</p>}
            {m && typeof m !== 'string' && (
              <div aria-label="Matching issues" role="group">
                <p className="small">{m.busy ? `Waiting for task ${m.busy} before taking another issue.` : m.next ? `Next: ${m.next.key} — ${m.next.title}` : 'No eligible issue now.'}</p>
                {m.waiting.length > 0 && <p className="small">Waiting: {m.waiting.map((i) => i.key).join(', ')}</p>}
                {m.skipped.length > 0 && <ul>{m.skipped.map((s) => <li key={s.key} className="small">{s.key}: {s.reason}</li>)}</ul>}
              </div>
            )}
          </article>
        );
      })}
      {(list.data ?? []).length === 0 && !list.loading && <p className="empty">No triggers for this workflow.</p>}
      {admin && !draft && <Btn small onClick={() => setDraft(emptyInput(graph))}>Add issue trigger</Btn>}
      {draft && (
        <div className="card form" role="form" aria-label="Add issue trigger">
          <label>Name<input value={draft.name} onChange={(e) => set({ name: e.target.value })} /></label>
          <label>Tracker<select value={draft.source.kind} onChange={(e) => setSource({ kind: e.target.value as 'github' | 'jira' })}><option value="github">GitHub</option><option value="jira">Jira</option></select></label>
          <label>MCP connection<input value={draft.source.connectionId} onChange={(e) => setSource({ connectionId: e.target.value })} placeholder="connection id" /></label>
          {draft.source.kind === 'github'
            ? <label>Repository<input value={draft.source.repository ?? ''} onChange={(e) => setSource({ repository: e.target.value })} placeholder="owner/name" /></label>
            : <label>Jira cloud ID<input value={draft.source.cloudId ?? ''} onChange={(e) => setSource({ cloudId: e.target.value })} /></label>}
          <label>Extra query<input value={draft.source.query ?? ''} onChange={(e) => setSource({ query: e.target.value })} placeholder={draft.source.kind === 'github' ? 'milestone:v1' : 'project = APP'} /></label>
          <label>Label<input value={draft.label} onChange={(e) => set({ label: e.target.value })} /></label>
          <label>Owner (tracker login or account id)<input value={draft.owner ?? ''} onChange={(e) => set({ owner: e.target.value })} /></label>
          <label>Entry agent<select value={draft.nodeId} onChange={(e) => set({ nodeId: e.target.value })}>{nodes.map((n) => <option key={n.id} value={n.id}>{n.name || n.id}</option>)}</select></label>
          <label>Result agent<select value={draft.resultNodeId ?? ''} onChange={(e) => set({ resultNodeId: e.target.value })}><option value="">Same as entry</option>{nodes.map((n) => <option key={n.id} value={n.id}>{n.name || n.id}</option>)}</select></label>
          <label>Check every (minutes)<input type="number" min={5} max={1440} value={draft.intervalMinutes} onChange={(e) => set({ intervalMinutes: Number(e.target.value) })} /></label>
          <label>Authorization<select value={draft.authorizationMode} onChange={(e) => set({ authorizationMode: e.target.value as TriggerInput['authorizationMode'] })}>{Object.entries(modes).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</select></label>
          {(scm.data ?? []).length > 0 && <label>Source control hand-off<select value={draft.scmConnection ?? ''} onChange={(e) => set({ scmConnection: e.target.value })}><option value="">None</option>{scm.data!.map((c) => <option key={c.name} value={c.name}>{c.name} ({c.repository})</option>)}</select></label>}
          <p className="small">Saving records your consent: tasks start as you, with {modes[draft.authorizationMode].split(' — ')[0]} authorization, for workflow version {graph.metadata.version}. Changing the workflow later pauses the trigger.</p>
          <div className="row">
            <Btn tone="primary" onClick={() => void save()}>Save trigger</Btn>
            <Btn onClick={() => setDraft(undefined)}>Cancel</Btn>
          </div>
        </div>
      )}
    </fieldset>
  );
}
