import {useEffect, useState} from 'react';
import {LoaderCircle} from 'lucide-react';
import {api, ApiError, errorMessage} from '../api/client';
import type {IssueConnection, IssueConnectionInput} from '../api/types';
import {useAsync} from '../hooks/useData';
import {Badge, Btn, Empty, ErrorBox, fmtDateTime} from './common';
import {HelpField} from './FieldHelp';
import {ConnectionFilters, emptyConnectionFilters} from './ConnectionFilters';

export const issueCreateLink = '#/connections/issues/new';
export const issueConnectionLink = (name: string) => `#/connections/issues/c/${encodeURIComponent(name)}`;
const statuses = {enabled: {label: 'Enabled'}, disabled: {label: 'Disabled'}, review: {label: 'Account review needed'}};
export function issueConnectionStatus(c: IssueConnection): keyof typeof statuses {
  return !c.enabled ? 'disabled' : c.projectRestricted && c.credentialConfigured ? 'enabled' : 'review';
}
export function matchingIssueConnections(rows: IssueConnection[], search: string, status: string): IssueConnection[] {
  const term = search.trim().toLowerCase();
  return rows.filter(c => (!status || issueConnectionStatus(c) === status) && [c.name, c.siteUrl, c.projectId, 'Jira Data Center'].some(value => value.toLowerCase().includes(term)));
}
type Check = {revision: number; state: 'pending' | 'ok' | 'failed'; message?: string};

export function IssueConnectionsSection({role}: {role?: string}) {
  const list = useAsync(api.issueConnections, []);
  const [filters, setFilters] = useState(emptyConnectionFilters);
  const [checks, setChecks] = useState<Record<string, Check>>({});
  const canTest = role === 'admin' || role === 'operator';
  const matches = matchingIssueConnections(list.data ?? [], filters.search, filters.status);
  const test = async (c: IssueConnection) => {
    setChecks(current => ({...current, [c.name]: {revision: c.revision, state: 'pending'}}));
    try {
      const result = await api.testIssueConnection(c.name, c.revision);
      const verified = result.ok && Boolean(result.identity?.key && result.identity?.name);
      setChecks(current => ({...current, [c.name]: {revision: c.revision, state: verified ? 'ok' : 'failed', message: verified ? `Signed in as ${result.identity?.name || result.identity?.key || 'the configured account'}. Project permissions still need operator qualification.` : result.error || 'The identity check failed.'}}));
    } catch (error) {
      setChecks(current => ({...current, [c.name]: {revision: c.revision, state: 'failed', message: errorMessage(error)}}));
    }
  };
  return <section className="card connections-panel" aria-labelledby="issue-connections-heading">
    <header className="connections-panel-head"><div><h2 id="issue-connections-heading">Issue trackers</h2>
      <p className="muted">Jira Data Center connections for issue triggers. Configure Jira Cloud through an MCP connection.</p></div>
      {role === 'admin' && <a className="btn btn-primary" href={issueCreateLink} aria-describedby="issue-connections-heading">Add connection</a>}
    </header>
    <div className="connections-toolbar"><ConnectionFilters value={filters} onChange={setFilters} placeholder="Name, site or project ID" statusLabel="Connection status" statuses={statuses}/>
      <Btn disabled={list.loading} onClick={() => void list.reload()}>Refresh issue connections</Btn>
    </div>
    <ErrorBox error={list.error}/>
    <p className="small muted connections-count" role="status">{list.loading ? 'Loading issue connections…' : list.error ? '' : `${matches.length} of ${list.data?.length ?? 0} connections`}</p>
    {!list.loading && !list.error && !list.data?.length && <Empty>No native issue tracker connections configured.</Empty>}
    {!list.loading && !!list.data?.length && !matches.length && <Empty>No issue connections match these filters.</Empty>}
    {!!matches.length && <div className="connection-table connections-table" role="region" aria-label="Issue tracker connection list" tabIndex={0}>
      <table><caption className="sr-only">Issue tracker connections</caption>
        <thead><tr><th scope="col">Connection</th><th scope="col">Site and project</th><th scope="col">Status</th><th scope="col">Revision</th><th scope="col">Updated</th><th scope="col">Identity check</th>{canTest && <th scope="col">Actions</th>}</tr></thead>
        <tbody>{matches.map(c => { const check = checks[c.name]?.revision === c.revision ? checks[c.name] : undefined; const status = issueConnectionStatus(c); return <tr key={c.name}>
          <td><strong>{c.name}</strong><span className="small muted connection-meta">Jira Data Center</span></td>
          <td>{c.siteUrl}<span className="small muted connection-meta">Project ID {c.projectId}</span></td>
          <td><Badge tone={status === 'enabled' ? 'ok' : status === 'review' ? 'warn' : undefined}>{statuses[status].label}</Badge></td>
          <td>{c.revision}</td><td><time dateTime={c.updatedAt}>{fmtDateTime(c.updatedAt)}</time></td>
          <td><div role="status"><Badge tone={check?.state === 'ok' ? 'ok' : check?.state === 'failed' ? 'bad' : undefined}>{check?.state === 'pending' ? 'Checking identity…' : check?.state === 'ok' ? 'Identity verified' : check?.state === 'failed' ? 'Check failed' : 'Not checked in this session'}</Badge>{check?.message && <p className="small connection-test-result">{check.message}</p>}</div></td>
          {canTest && <td><div className="btn-row"><Btn small disabled={!c.enabled || !c.credentialConfigured || check?.state === 'pending'} onClick={() => void test(c)}>{check?.state === 'pending' ? <><LoaderCircle size={14} className="connection-test-spinner" aria-hidden="true"/> Testing…</> : 'Test connection'}</Btn>{role === 'admin' && <a href={issueConnectionLink(c.name)}>Edit</a>}</div></td>}
        </tr>;})}</tbody>
      </table>
    </div>}
  </section>;
}

const emptyDraft: IssueConnectionInput = {name: '', kind: 'jira-dc', siteUrl: '', projectId: '', enabled: true, projectRestricted: false};
export function IssueConnectionEditor({role, name}: {role?: string; name?: string}) {
  const connection = useAsync(() => name ? api.issueConnection(name) : Promise.resolve(undefined), [name]);
  const [draft, setDraft] = useState<IssueConnectionInput>(emptyDraft);
  const [token, setToken] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string>();
  const [conflict, setConflict] = useState(false);
  useEffect(() => {
    if (connection.data) {
      const c = connection.data;
      setDraft({name: c.name, kind: c.kind, siteUrl: c.siteUrl, projectId: c.projectId, enabled: c.enabled, projectRestricted: c.projectRestricted});
      setToken('');
      setConflict(false);
    }
  }, [connection.data]);
  const scopeChanged = !!connection.data && (draft.siteUrl.trim().replace(/\/+$/, '') !== connection.data.siteUrl || draft.projectId.trim() !== connection.data.projectId);
  const needsToken = !name || scopeChanged;
  const set = (patch: Partial<IssueConnectionInput>) => setDraft(current => ({...current, ...patch}));
  const save = async () => {
    if (role !== 'admin' || saving || conflict || (name && !connection.data)) return;
    if (needsToken && !token) {setError('Provide a PAT for the selected site and project.'); return;}
    const route = location.hash;
    setSaving(true); setError(undefined);
    try {
      await api.saveIssueConnection({...draft, name: draft.name.trim(), siteUrl: draft.siteUrl.trim(), projectId: draft.projectId.trim(), ...(token ? {token} : {})}, name ? connection.data?.revision : undefined);
      setToken('');
      if (location.hash === route) location.hash = '#/connections';
    } catch (error) { setError(errorMessage(error)); setConflict(error instanceof ApiError && error.status === 409); }
    finally {setSaving(false);}
  };
  return <section>
    <header className="page-head connection-detail-head"><div><a href="#/connections">Connections</a><h1>{name ? `Edit ${name}` : 'New issue tracker connection'}</h1><p className="muted">Connect a Jira Data Center project to issue triggers.</p></div></header>
    {role !== 'admin' ? <p>An administrator must configure issue tracker connections.</p> : <>
      {name && connection.loading && <p role="status">Loading issue connection…</p>}
      <ErrorBox error={connection.error}/>
      {name && connection.error && <Btn onClick={() => void connection.reload()}>Retry loading connection</Btn>}
      {(!name || (!connection.loading && connection.data)) && <form className="card form connections-form" aria-label={name ? 'Edit issue tracker connection' : 'Add issue tracker connection'} onSubmit={e => {e.preventDefault(); void save();}}>
        <ErrorBox error={error}/>
        {conflict && <div className="connection-token"><p>The connection changed. Reload its saved revision before submitting again. Reloading discards these form edits.</p><Btn onClick={() => {setError(undefined); void connection.reload();}}>Reload saved connection</Btn></div>}
        <HelpField label="Connection name" help="A stable identifier selected by issue triggers. Use lowercase letters, digits, hyphens or underscores; begin with a letter. Existing names cannot be changed."><input required pattern="[a-z][a-z0-9_-]{0,63}" disabled={!!name || saving} value={draft.name} onChange={e => set({name: e.target.value})}/></HelpField>
        <HelpField label="Service" help="This connection uses the Jira Data Center REST 2 API and a separate PAT. Jira Cloud uses an Atlassian MCP connection."><input readOnly value="Jira Data Center"/></HelpField>
        <HelpField label="Site URL" help="The HTTPS site address, including its context path when present. Do not include /rest/api, a query, credentials or a fragment. TLS certificate validation remains required."><input type="url" required disabled={saving} placeholder="https://jira.example.com/context" value={draft.siteUrl} onChange={e => {setToken(''); set({siteUrl: e.target.value, projectRestricted: false});}}/></HelpField>
        <HelpField label="Project ID" help="The immutable numeric Jira project ID, such as 10001. This is different from a project key such as APP and prevents issue-key prefixes from changing the connection's scope."><input required pattern="[1-9][0-9]{0,19}" inputMode="numeric" disabled={saving} value={draft.projectId} onChange={e => {setToken(''); set({projectId: e.target.value, projectRestricted: false});}}/></HelpField>
        <HelpField className="connection-token" label="Personal access token" help="Use a dedicated technical account whose remote write permissions are restricted to this project. The PAT is encrypted on the server and never returned. Changing site or project requires a replacement PAT. Replacing the PAT requires reviewing the account restriction again."><input type="password" autoComplete="off" required={needsToken} disabled={saving} value={token} onChange={e => {setToken(e.target.value); set({projectRestricted: false});}}/></HelpField>
        {name && <p className="small muted connection-token">{scopeChanged ? 'Site or project changed. Provide a replacement PAT and review the account restriction again.' : 'Leave the PAT blank to keep the stored credential for this site and project.'}</p>}
        <HelpField className="checkbox" label="I verified that the technical account can write only to this project" help="Records your review of the remote Jira permission scheme. Weft cannot turn a broadly privileged PAT into a project-scoped credential. Leave this unchecked until that restriction has been reviewed; triggers cannot use this connection for automatic writes without it."><input type="checkbox" disabled={saving} checked={draft.projectRestricted} onChange={e => set({projectRestricted: e.target.checked})}/></HelpField>
        <HelpField className="checkbox" label="Connection enabled" help="Disabled connections cannot be tested or used by triggers. Saving any connection change invalidates earlier trigger consent; review and resume affected triggers explicitly."><input type="checkbox" disabled={saving} checked={draft.enabled} onChange={e => set({enabled: e.target.checked})}/></HelpField>
        <p className="small muted connection-token">Saving does not start a trigger or grant agent tools. Identity checks confirm authentication; project permission qualification is a separate step.</p>
        <div className="btn-row"><Btn type="submit" tone="primary" disabled={saving || conflict}>{saving ? 'Saving…' : 'Save connection'}</Btn>{saving ? <Btn disabled>Cancel</Btn> : <a href="#/connections">Cancel</a>}</div>
      </form>}
    </>}
  </section>;
}
