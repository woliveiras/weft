import {profileLabels} from '../api/tasks';
import {cloudWorkflowConnection} from './workflowEdition';
import { LifecycleMappingPicker } from './LifecycleMappingPicker';
import { HelpField } from './FieldHelp';
import { useEffect, useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { Graph, Trigger, TriggerInput, TriggerMatches, TriggerSource } from '../api/types';
import { useAsync } from '../hooks/useData';
import { Badge, Btn, ErrorBox, fmtDateTime } from './common';

const modes: Record<TriggerInput['authorizationMode'], string> = {
  ...profileLabels,
  ask: 'Ask — every gated action waits for a person',
  workspace: 'Workspace — act inside the task worktree',
  full: 'Full access',
};

function emptyInput(graph: Graph): TriggerInput {
  const first = graph.spec.nodes.find((n) => !n.disabled)?.id ?? '';
  return { name: '', workflow: graph.metadata.id, nodeId: first, resultNodeId: '', source: { kind: 'github', connectionId: '', repository: '' }, label: 'weft:todo', owner: '', intervalMinutes: 15, authorizationMode: graph.spec.task?.authorizationProfile??'ask' };
}

function holdResolutionWarning(hold: NonNullable<Trigger['holds']>[number]): string | undefined {
  if (hold.publication === 'label_applied') {
    return 'Abandon the readiness comment and resolve this hold? The tracker keeps its current labels and assignment, including any weft:needs-attention label. The readiness comment has not been published.';
  }
  if (hold.undoOwner || hold.publication === 'label_pending' || hold.publication === 'label_pending_only' || hold.publication === 'label_failed') {
    return 'Abandon the tracker update and resolve this hold? The tracker keeps its current labels (including any selection, weft:wip, or weft:needs-attention labels) and assignment. Review them on the issue.';
  }
}

function holdRetryDeadline(hold: NonNullable<Trigger['holds']>[number], now: number): string | undefined {
  return [hold.retryAt, hold.retryLabelAt, hold.retryCommentAt, hold.retryAccessAt]
    .filter((at): at is string => Boolean(at && Date.parse(at) > now))
    .sort((a, b) => Date.parse(b) - Date.parse(a))[0];
}

/** Issue triggers of a saved workflow: labelled issues start tasks under the consent of the administrator who saves or resumes them. */
export function TriggersSection({ graph, role, onTriggers }: {
  graph: Graph;
  role?: string;
  onTriggers?: (triggers: Trigger[] | undefined) => void;
}) {
  const list = useAsync(() => api.triggers(graph.metadata.id), [graph.metadata.id]);
  useEffect(() => { onTriggers?.(list.data); }, [list.data, onTriggers]);
  const policy = useAsync(() => api.triggerPolicy(), []);
  const scm = useAsync(() => api.scmConnections().catch(() => []), []);
  const [editing, setEditing] = useState<string>();
  const [draft, setDraft] = useState<TriggerInput>();
  const nativeSelected = draft?.source.kind === 'jira-dc';
  const native = useAsync(() => nativeSelected ? api.issueConnections() : Promise.resolve([]), [nativeSelected]);
  const cloudSelected=draft?.source.kind==='jira';
  const cloudProfiles=useAsync(()=>cloudSelected?api.jiraCloudProfiles():Promise.resolve([]),[cloudSelected]);
  const selectedCloud=cloudProfiles.data?.find(p=>p.name===draft?.source.profileId);
  const cloudScopeMatches=!!selectedCloud&&selectedCloud.enabled&&selectedCloud.projectRestricted&&selectedCloud.revision===draft?.source.profileRevision&&selectedCloud.mcpConnection===draft?.source.connectionId&&selectedCloud.cloudId===draft?.source.cloudId&&selectedCloud.siteUrl===draft?.source.siteUrl&&selectedCloud.projectId===draft?.source.projectId;
  const selectedNative = native.data?.find(c => c.name === draft?.source.connectionId);
  const nativeScopeMatches = !!selectedNative && selectedNative.enabled && selectedNative.credentialConfigured && selectedNative.projectRestricted && selectedNative.siteUrl === draft?.source.siteUrl && selectedNative.projectId === draft?.source.projectId;
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string>();
  const [matches, setMatches] = useState<Record<string, TriggerMatches | string>>({});
  const [resolving, setResolving] = useState<string>();
  const admin = role === 'admin';
  const canPreview = admin || role === 'operator';
  const nodes = graph.spec.nodes.filter((n) => !n.disabled);
  const [deadlineTick, refreshDeadlines] = useState(0);
  const now = Date.now();
  let nextDeadline = Infinity;
  for (const trigger of list.data ?? []) {
    for (const hold of trigger.holds ?? []) {
      const deadline = holdRetryDeadline(hold, now);
      if (deadline) nextDeadline = Math.min(nextDeadline, Date.parse(deadline));
    }
  }
  useEffect(() => {
    if (!Number.isFinite(nextDeadline)) return;
    const delay = Math.min(2_147_483_647, Math.max(0, nextDeadline - Date.now()));
    const timer = window.setTimeout(() => refreshDeadlines((tick) => tick + 1), delay);
    return () => window.clearTimeout(timer);
  }, [nextDeadline, deadlineTick]);
  const set = (patch: Partial<TriggerInput>) => setDraft((d) => ({ ...(d ?? emptyInput(graph)), ...patch }));
  const setSource = (patch: Partial<TriggerInput['source']>) => setDraft((d) => {
    const cur=d??emptyInput(graph);
    const changedScope=(patch.connectionId!==undefined&&patch.connectionId!==cur.source.connectionId)||(patch.cloudId!==undefined&&patch.cloudId!==cur.source.cloudId)||(patch.siteUrl!==undefined&&patch.siteUrl!==cur.source.siteUrl)||(patch.projectId!==undefined&&patch.projectId!==cur.source.projectId);
    const clearProfile=cur.source.kind==='jira'&&!!cur.source.profileId&&changedScope;
    return {...cur,lifecycleMappings:changedScope?undefined:cur.lifecycleMappings,source:{...cur.source,...patch,...(clearProfile?{profileId:undefined,profileRevision:undefined,siteUrl:undefined,projectId:undefined}:{})}};
  });
  const save = async () => {
    if (!draft || saving) return;
    if (nativeSelected && (!nativeScopeMatches || native.loading || native.error)) {setError('Select and review an enabled Jira Data Center connection with a project-restricted account.'); return;}
    if(cloudSelected&&draft.source.profileId&&(!cloudScopeMatches||cloudProfiles.loading||cloudProfiles.error)){setError('Review the selected Cloud profile revision and scope before saving.');return;}
    setSaving(true);
    setError(undefined);
    try {
      const src: TriggerSource = draft.source.kind === 'github'
        ? { kind: draft.source.kind, connectionId: draft.source.connectionId.trim(), repository: draft.source.repository?.trim(), query: draft.source.query?.trim() || undefined }
        : draft.source.kind === 'jira-dc' ? {kind: 'jira-dc', connectionId: draft.source.connectionId, siteUrl: draft.source.siteUrl, projectId: draft.source.projectId, query: draft.source.query?.trim() || undefined} : { kind: draft.source.kind, connectionId: draft.source.connectionId.trim(), cloudId: draft.source.cloudId?.trim(), ...(draft.source.profileId?{profileId:draft.source.profileId,profileRevision:draft.source.profileRevision,siteUrl:draft.source.siteUrl,projectId:draft.source.projectId}:{}), query: draft.source.query?.trim() || undefined };
      const input = { ...draft, source: src, resultNodeId: draft.resultNodeId || undefined, owner: draft.owner?.trim() || undefined, scmConnection: draft.scmConnection || undefined };
      if (editing) await api.updateTrigger(editing, input);
      else await api.createTrigger(input);
      setEditing(undefined);
      setDraft(undefined);
      await list.reload();
    } catch (e) {
      setError(errorMessage(e));
    } finally {setSaving(false);}
  };
  const edit = (t: Trigger) => {
    setEditing(t.id);
    setDraft({
      name: t.name, workflow: t.workflow, nodeId: t.nodeId, resultNodeId: t.resultNodeId, source: t.source,
      label: t.label, owner: t.owner, intervalMinutes: t.intervalMinutes, scmConnection: t.scmConnection,
      authorizationMode: t.authorizationMode, executionMode: t.executionMode, projectId: t.projectId,
      readinessDisabled: t.readinessDisabled, lifecycleMappings: t.lifecycleMappings,
    });
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
  const resolve = async (id: string, key: string, generation: string, warning?: string) => {
    if (warning && !confirm(warning)) return;
    setError(undefined);
    setResolving(`${id}:${key}`);
    try {
      await api.resolveTriggerHold(id, key, generation, Boolean(warning));
      await list.reload();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setResolving(undefined);
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
    <fieldset aria-label="Issue triggers" disabled={saving}>
      <legend>Issue triggers</legend>
      <p className="muted tiny">
        A trigger checks issues with its selection label, assesses readiness and starts one task at a time under reviewed consent.
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
              {t.source.kind === 'github' ? `GitHub ${t.source.repository}` : t.source.kind === 'jira-dc' ? `Jira Data Center · ${t.source.siteUrl} · project ${t.source.projectId}` : 'Jira Cloud'} via {t.source.connectionId} · label <code>{t.label}</code> · every {t.intervalMinutes} min · {modes[t.authorizationMode].split(' — ')[0]}
              {t.scmConnection && ` · hands off to ${t.scmConnection}`}
            </p>
            <p className="small muted">Runs as {t.consent.actor} for workflow version {t.consent.workflowVersion} (consent recorded {fmtDateTime(t.consent.recordedAt)}).</p>
            {t.pausedReason && <p role="status">{t.pausedReason}</p>}
            {t.lastFailedTask && <p><a href={`#/tasks/${encodeURIComponent(t.lastFailedTask)}`}>Open failed task</a></p>}
            {t.lastError && <p className="tone-bad small">Last run failed: {t.lastError}</p>}
            <p className="small">
              {t.lastRunAt ? `Last run ${fmtDateTime(t.lastRunAt)}` : 'Not run yet'}
              {t.nextRunAt && t.state === 'active' && ` · next ${fmtDateTime(t.nextRunAt)}`}
            </p>
            {t.currentTask && <p className="small">Working on {t.currentIssue?.key}: <a href={`#/tasks/${encodeURIComponent(t.currentTask)}`}>open task</a></p>}
            <p className="small">Readiness {t.readinessDisabled ? 'disabled' : 'enabled'}</p>
            {t.searchDeferredReason && <p role="status">{t.searchDeferredReason}</p>}
            {t.readinessBlock && <p role="status">{t.readinessBlock}</p>}
            {Boolean(t.holds?.length) && <ul aria-label="Held issues">
              {t.holds?.map(h => {
                const warning = holdResolutionWarning(h);
                const retryAt = holdRetryDeadline(h, now);
                return <li key={h.key}>
                  {h.url ? <a href={h.url}>{h.key}</a> : h.key} · round {h.round} · {h.message || h.reason} · edits {h.snapshotEdits ?? 0} of 3
                  {h.eligibility && ` The issue ${h.eligibility}.`}
                  {h.accessError && ` ${h.accessError}`}
                  {h.publicationMessage && ` ${h.publicationMessage}`}
                  {retryAt && <> · Retry after <time dateTime={retryAt}>{fmtDateTime(retryAt)}</time></>}
                  {h.publication === 'comment_pending' && ' Tracker comment delivery is unconfirmed.'}
                  {(h.publication === 'label_pending' || h.publication === 'label_pending_only') && ' Tracker label update is pending.'}
                  {canPreview && <Btn small disabled={resolving !== undefined} onClick={() => void resolve(t.id, h.key, h.generation, warning)}>{warning ? 'Force resolve hold' : 'Resolve hold'}</Btn>}
                </li>;
              })}
            </ul>}
            {Boolean(t.holdResolutions?.length) && <details>
              <summary>Resolved readiness holds</summary>
              <ul>{t.holdResolutions?.map((h, index) => <li key={`${h.key}:${h.resolvedAt}:${index}`}>{h.key} · {h.reason}</li>)}</ul>
            </details>}
            {(t.skipped ?? []).length > 0 && (
              <details>
                <summary>{t.skipped!.length} skipped issues</summary>
                <ul>{t.skipped!.map((s) => <li key={s.key}>{s.key} {s.title && `— ${s.title}`}: {s.reason}</li>)}</ul>
              </details>
            )}
            {(t.notes ?? []).length > 0 && <ul aria-label="Trigger notes">{t.notes!.map((n, i) => <li key={i} className="small">{n}</li>)}</ul>}
            <div className="row">
              {canPreview && <Btn small onClick={() => void preview(t)}>Preview matching issues</Btn>}
              {admin && <Btn small onClick={() => edit(t)}>Edit</Btn>}
              {admin && t.state === 'active' && <Btn small onClick={() => void act(t, 'pause')}>Pause</Btn>}
              {admin && (t.state === 'paused' || (t.state === 'active' && t.holds?.some((h) => h.accessError))) && <Btn small tone="primary" onClick={() => void act(t, 'resume')}>Resume and record consent</Btn>}
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
        <div className="card form" role="form" aria-label={editing ? 'Edit issue trigger' : 'Add issue trigger'}>
          <label>Name<input value={draft.name} onChange={(e) => set({ name: e.target.value })} /></label>
          <HelpField label="Tracker" help="GitHub and Jira Cloud use MCP connections. Jira Data Center uses its separate project-scoped REST connection. Source control delivery is selected independently."><select value={draft.source.kind} onChange={e => set({lifecycleMappings: undefined, source: {kind: e.target.value as TriggerSource['kind'], connectionId: '', ...(e.target.value === 'github' ? {repository: ''} : {})}})}><option value="github">GitHub</option><option value="jira">Jira Cloud</option><option value="jira-dc">Jira Data Center</option></select></HelpField>
          {nativeSelected ? <>
            <HelpField label="Issue tracker connection" help="Select an enabled native connection whose account has been reviewed as remotely restricted to its project. The exact site, numeric project and connection revision are included in trigger consent."><select disabled={native.loading} value={draft.source.connectionId} onChange={e => {
              const c = native.data?.find(row => row.name === e.target.value);
              setSource({connectionId: c?.name ?? '', siteUrl: c?.siteUrl, projectId: c?.projectId});
            }}><option value="">Select connection</option>{draft.source.connectionId && !native.data?.some(c => c.name === draft.source.connectionId) && <option value={draft.source.connectionId} disabled>{draft.source.connectionId} (unavailable)</option>}{native.data?.map(c => <option key={c.name} value={c.name} disabled={!c.enabled || !c.projectRestricted || !c.credentialConfigured}>{c.name} · project {c.projectId}{!c.enabled ? ' · disabled' : !c.projectRestricted || !c.credentialConfigured ? ' · account review needed' : ''}</option>)}</select></HelpField>
            {native.loading && <p role="status">Loading issue connections…</p>}
            <ErrorBox error={native.error}/>
            <div className="btn-row"><Btn small disabled={native.loading} onClick={() => void native.reload()}>Refresh issue connections</Btn><a href="#/connections/issues/new">Configure issue connection</a></div>
            <HelpField label="Reviewed site" help="The saved HTTPS site, including its context path. Weft rejects a trigger when this snapshot does not match the native connection."><input readOnly value={draft.source.siteUrl ?? ''}/></HelpField>
            <HelpField label="Reviewed project ID" help="The numeric Jira project identity bound to this trigger, independent of its task project or source control repository."><input readOnly value={draft.source.projectId ?? ''}/></HelpField>
            {selectedNative && !nativeScopeMatches && <p role="alert">The connection scope or write prerequisite changed. Review it before saving. {selectedNative.enabled && selectedNative.projectRestricted && selectedNative.credentialConfigured && <Btn small onClick={() => setSource({siteUrl: selectedNative.siteUrl, projectId: selectedNative.projectId})}>Use current connection scope</Btn>}</p>}
            <p className="small muted">Jira Data Center claims preserve assignees and update labels. Eligibility uses the issue's new status-category key, regardless of its display name. Resolve readiness holds in the console before manual reselection; a relabel alone cannot prove its actor's permissions.</p>
          </> : <>
            <HelpField label="MCP connection" help="The enabled, discovered MCP connection used to read tracker issues. It needs access to the repository or Jira Cloud site you configure."><input value={draft.source.connectionId} onChange={e => setSource({connectionId: e.target.value})} placeholder="connection id"/></HelpField>
            {draft.source.kind === 'github' ? <HelpField label="Repository" help="The GitHub repository to poll, written as owner/repository. Use a repository the selected connection can read."><input value={draft.source.repository ?? ''} onChange={e => setSource({repository: e.target.value})} placeholder="owner/name"/></HelpField> : <HelpField label="Jira cloud ID" help="Identifies the Jira Cloud site to search. Use the cloud ID associated with the selected Atlassian MCP connection."><input value={draft.source.cloudId ?? ''} onChange={e => setSource({cloudId: e.target.value})}/></HelpField>}
          </>}
          {cloudSelected&&<>
          <HelpField label="Optional Cloud workflow profile" help="Opt in to an explicitly reviewed profile revision for status synchronization. Leaving this empty preserves the existing Jira Cloud issue-trigger configuration. Selecting a profile binds its MCP connection, Cloud site and project; it does not select a transition automatically."><select disabled={cloudProfiles.loading||!!list.data?.find(t=>t.id===editing)?.currentTask} value={draft.source.profileId?`${draft.source.profileId}@${draft.source.profileRevision}`:''} onChange={e=>{
            const p=cloudProfiles.data?.find(v=>`${v.name}@${v.revision}`===e.target.value);
            if(p)set({lifecycleMappings:undefined,source:{kind:'jira',connectionId:p.mcpConnection,cloudId:p.cloudId,siteUrl:p.siteUrl,projectId:p.projectId,profileId:p.name,profileRevision:p.revision,query:draft.source.query}});
            else set({lifecycleMappings:undefined,source:{kind:'jira',connectionId:draft.source.connectionId,cloudId:draft.source.cloudId,query:draft.source.query}});
          }}><option value="">Keep current behavior — no status synchronization</option>{draft.source.profileId&&!cloudProfiles.data?.some(p=>p.name===draft.source.profileId&&p.revision===draft.source.profileRevision)&&<option value={`${draft.source.profileId}@${draft.source.profileRevision}`} disabled>{draft.source.profileId} · Revision {draft.source.profileRevision} (review required)</option>}{cloudProfiles.data?.map(p=><option key={p.name} value={`${p.name}@${p.revision}`} disabled={!p.enabled||!p.projectRestricted}>{p.name} · Revision {p.revision} · Project {p.projectId}{p.enabled&&p.projectRestricted?'':' · review needed'}</option>)}</select></HelpField>
          <ErrorBox error={cloudProfiles.error}/>{cloudProfiles.loading&&<p role="status">Loading Cloud workflow profiles…</p>}
          <div className="btn-row"><Btn small disabled={cloudProfiles.loading} onClick={()=>void cloudProfiles.reload()}>Refresh Cloud profiles</Btn><a href="#/connections/jira-cloud/new">Configure Cloud profile</a></div>
          {draft.source.profileId&&!cloudScopeMatches&&!cloudProfiles.loading&&<p role="alert">The selected Cloud profile changed or its scope no longer matches. Select a reviewed revision again before saving.</p>}
          {selectedCloud&&cloudScopeMatches&&<LifecycleMappingPicker edition="jira" connection={cloudWorkflowConnection(selectedCloud)} value={draft.lifecycleMappings} disabled={!!list.data?.find(t=>t.id===editing)?.currentTask} onChange={lifecycleMappings=>set({lifecycleMappings})}/>}
          </>}
          <HelpField label={"Extra query"} help={"Narrows the tracker search using GitHub search syntax or JQL for Jira. Test the filter in your tracker before enabling the trigger."}><input value={draft.source.query ?? ''} onChange={(e) => setSource({ query: e.target.value })} placeholder={draft.source.kind === 'github' ? 'milestone:v1' : 'project = APP'} /></HelpField>
          {nativeSelected&&selectedNative&&nativeScopeMatches&&<LifecycleMappingPicker connection={selectedNative} value={draft.lifecycleMappings} disabled={!!list.data?.find(t=>t.id===editing)?.currentTask} onChange={lifecycleMappings=>set({lifecycleMappings})}/>}
          <HelpField label={'Assess issue readiness before work'} help="Checks whether an eligible issue describes enough work to begin. Add the missing requirements to the tracker issue before retrying."><input type="checkbox" checked={!draft.readinessDisabled} onChange={(e) => set({ readinessDisabled: !e.target.checked })} /></HelpField>
          <HelpField label={"Label"} help={"Only issues with this tracker label are considered. Use a dedicated label to control which issues can start workflow tasks."}><input value={draft.label} onChange={(e) => set({ label: e.target.value })} /></HelpField>
          <HelpField label={"Owner (tracker login or account id)"} help={"Limits eligible issues by assignee and identifies whose clarification replies can contribute. Jira Data Center uses a login and preserves the assignee; Jira Cloud expects an account ID."}><input value={draft.owner ?? ''} onChange={(e) => set({ owner: e.target.value })} /></HelpField>
          <HelpField label={"Entry agent"} help={"The node that receives the task when an eligible issue starts a run. Pick the agent that knows how to turn issue details into work."}><select value={draft.nodeId} onChange={(e) => set({ nodeId: e.target.value })}>{nodes.map((n) => <option key={n.id} value={n.id}>{n.name || n.id}</option>)}</select></HelpField>
          <HelpField label={"Result agent"} help={"The node that supplies the task result. Leave it empty when the entry agent finishes the task itself."}><select value={draft.resultNodeId ?? ''} onChange={(e) => set({ resultNodeId: e.target.value })}><option value="">Same as entry</option>{nodes.map((n) => <option key={n.id} value={n.id}>{n.name || n.id}</option>)}</select></HelpField>
          <HelpField label={"Check every (minutes)"} help={"Controls how often Weft polls the tracker. Shorter intervals pick up eligible issues sooner and make more tracker calls."}><input type="number" min={5} max={1440} value={draft.intervalMinutes} onChange={(e) => set({ intervalMinutes: Number(e.target.value) })} /></HelpField>
          <HelpField label={"Authorization"} help={"Controls decisions during generated tasks. Ask prompts for gated actions. Workspace access covers reviewed worktree operations. Full access preauthorizes reviewed grants within scope; mandatory policies still apply."}><select value={draft.authorizationMode} onChange={(e) => set({ authorizationMode: e.target.value as TriggerInput['authorizationMode'] })}>{Object.entries(modes).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</select></HelpField>
          {(scm.data ?? []).length > 0 && <HelpField label={"Source control hand-off"} help={"Select the connection that delivers the result to source control. It determines the destination and whether opening a pull request requires approval."}><select value={draft.scmConnection ?? ''} onChange={(e) => set({ scmConnection: e.target.value })}><option value="">None</option>{scm.data!.map((c) => <option key={c.name} value={c.name}>{c.name} ({c.repository})</option>)}</select></HelpField>}
          <p className="small">Saving records your consent: tasks start as you, with {modes[draft.authorizationMode].split(' — ')[0]} authorization, for workflow version {graph.metadata.version}. Tracker access includes issue lookup, label updates and comments. {!draft.readinessDisabled && (nativeSelected ? 'Readiness also reads replies and sends the issue and eligible replies to the entry agent’s model within the installation limits. DC manual requeue requires console review.' : 'Readiness also reads replies and label history and sends the issue and replies to the entry agent’s model within the installation limits.')} Saving a changed scope records your consent again. A paused trigger stays paused until you resume it.</p>
          {!draft.readinessDisabled && policy.data && (
            <p className="small">Each assessment uses one call, up to {policy.data.inputBytes / 1024} KiB of input, {policy.data.outputTokens} output tokens and {policy.data.seconds} seconds. The monetary limit is {policy.data.moneyMicros / 1_000_000} {policy.data.currency} per assessment.</p>
          )}
          <div className="row">
            <Btn tone="primary" disabled={saving || (nativeSelected && (!nativeScopeMatches || native.loading || !!native.error))} onClick={() => void save()}>{saving ? 'Saving…' : 'Save trigger'}</Btn>
            <Btn onClick={() => { setDraft(undefined); setEditing(undefined); }}>Cancel</Btn>
          </div>
        </div>
      )}
    </fieldset>
  );
}
