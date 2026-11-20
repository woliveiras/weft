import { LoaderCircle } from 'lucide-react';
import { HelpField } from './FieldHelp';
import { useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { SCMConnection } from '../api/types';
import { useAsync } from '../hooks/useData';
import { Badge, Btn, Empty, ErrorBox } from './common';
import { ConnectionFilters, emptyConnectionFilters } from './ConnectionFilters';

type Draft = { name: string; kind: string; repository: string; apiUrl: string; gitUrl: string; branchPrefix: string; approval: boolean; token: string };
const empty: Draft = { name: '', kind: 'github', repository: '', apiUrl: '', gitUrl: '', branchPrefix: '', approval: true, token: '' };
export const scmCreateLink = '#/connections/source-control/new';
type ConnectionTest = { state: 'testing' | 'connected' | 'limited' | 'failed'; message?: string };
const testStatuses: Record<ConnectionTest['state'] | 'untested', { label: string; tone?: string }> = {
  untested: { label: 'Not tested' },
  testing: { label: 'Checking connection' },
  connected: { label: 'Connected', tone: 'ok' },
  limited: { label: 'Limited access', tone: 'warn' },
  failed: { label: 'Connection failed', tone: 'bad' },
};
const serviceNames: Record<string, string> = { github: 'GitHub', 'bitbucket-cloud': 'Bitbucket Cloud', 'bitbucket-dc': 'Bitbucket Data Center' };

/** Server-held source control connections for push and pull request tools. */
export function SCMConnectionsSection({ role }: { role?: string }) {
  const list = useAsync(api.scmConnections, []);
  const [filters, setFilters] = useState(emptyConnectionFilters);
  const [results, setResults] = useState<Record<string, ConnectionTest>>({});
  const [error, setError] = useState<string>();
  const admin = role === 'admin';
  const canTest = admin || role === 'operator';
  const search = filters.search.trim().toLowerCase();
  const matches = (list.data ?? []).filter(connection => {
    const status = results[connection.name]?.state ?? 'untested';
    const service = serviceNames[connection.kind] ?? connection.kind;
    return (!filters.status || status === filters.status) &&
      [connection.name, connection.kind, service, connection.repository].some(value => value.toLowerCase().includes(search));
  });
  const test = async (connection: SCMConnection) => {
    setResults(current => ({ ...current, [connection.name]: { state: 'testing' } }));
    let result: ConnectionTest;
    try {
      const check = await api.testSCMConnection(connection.name);
      if (check.ok && check.identity) {
        const identity = check.identity;
        const pushAccess = identity.canPush ? 'Can push.' : 'Cannot push.';
        result = {
          state: identity.canPush ? 'connected' : 'limited',
          message: `Signed in as ${identity.user}. Default branch is ${identity.defaultBranch || 'unknown'}. ${pushAccess}`,
        };
      } else {
        result = { state: 'failed', message: check.error || 'The service returned no connection identity.' };
      }
    } catch (e) {
      result = { state: 'failed', message: errorMessage(e) };
    }
    setResults(current => ({ ...current, [connection.name]: result }));
  };
  const remove = async (c: SCMConnection) => {
    if (!confirm(`Remove the source control connection "${c.name}"? Tasks that use it can no longer push.`)) return;
    try {
      await api.deleteSCMConnection(c.name);
      setResults(current => {
        const next = { ...current };
        delete next[c.name];
        return next;
      });
      await list.reload();
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  return (
    <section className="card connections-panel" aria-labelledby="scm-heading">
      <header className="connections-panel-head">
        <div>
          <h2 id="scm-heading">Source control</h2>
          <p className="muted">Let tasks push branches and open pull requests. Weft keeps the credentials on the server.</p>
        </div>
        {admin && <a className="btn btn-primary" href={scmCreateLink} aria-describedby="scm-heading">Add connection</a>}
      </header>
      <div className="connections-toolbar">
        <ConnectionFilters value={filters} onChange={setFilters} placeholder="Name, service or repository"
          statusLabel="Test status" statuses={testStatuses} />
      </div>
      {list.error && <div className="connections-load-error">
        <ErrorBox error={`Could not load source control connections: ${list.error}`} />
        <Btn disabled={list.loading} onClick={() => void list.reload()}>Reload source control connections</Btn>
      </div>}
      <ErrorBox error={error} />
      <p className="small muted connections-count" role="status">
        {list.loading ? 'Loading source control connections…' : list.error ? '' : list.data?.length ? `${matches.length} of ${list.data.length} connections` : ''}
      </p>
      {!list.loading && !list.error && !list.data?.length && <Empty>No source control connection yet.</Empty>}
      {!list.loading && !!list.data?.length && !matches.length && <Empty>No connections match these filters.</Empty>}
      {matches.length > 0 && <div className="connection-table connections-table" role="region" aria-label="Source control connection list" tabIndex={0}>
        <table><caption className="sr-only">Source control connections</caption>
          <thead><tr><th scope="col">Connection</th><th scope="col">Repository and branches</th><th scope="col">Pull request approval</th><th scope="col">Credential</th><th scope="col">Test status</th>{canTest && <th scope="col">Actions</th>}</tr></thead>
          <tbody>{matches.map(c => {
            const result = results[c.name];
            const pending = result?.state === 'testing';
            const status = testStatuses[result?.state ?? 'untested'];
            return <tr key={c.name}>
              <td><strong>{c.name}</strong><span className="small muted connection-meta">{serviceNames[c.kind] ?? c.kind}</span></td>
              <td>{c.repository}<code className="small muted connection-meta">{c.branchPrefix || 'weft/'}*</code></td>
              <td>{c.requirePullRequestApproval === false ? 'Not required' : 'Required'}</td>
              <td>{c.tokenConfigured ? 'Configured' : <Badge tone="warn">No token</Badge>}</td>
              <td><div role="status">
                <Badge tone={status.tone}>{status.label}</Badge>
                {result?.message && <p className="small connection-test-result">{result.message}</p>}
              </div></td>
              {canTest && <td><div className="btn-row">
                <Btn small disabled={pending} onClick={() => void test(c)}>
                  {pending ? <><LoaderCircle size={14} className="connection-test-spinner" aria-hidden="true" /> Testing…</> : 'Test connection'}
                </Btn>
                {admin && <Btn small tone="danger" disabled={pending} onClick={() => void remove(c)}>Remove</Btn>}
              </div></td>}
            </tr>;
          })}</tbody>
        </table>
      </div>}
    </section>
  );
}

export function SCMConnectionCreate({ role }: { role?: string }) {
  const kinds = useAsync(api.scmKinds, []);
  const [draft, setDraft] = useState<Draft>(empty);
  const kind = kinds.data?.find(item => item.id === draft.kind);
  const [error, setError] = useState<string>();
  const [saving, setSaving] = useState(false);
  const set = (patch: Partial<Draft>) => setDraft(current => ({ ...current, ...patch }));

  const save = async () => {
    if (saving || role !== 'admin' || !kind) return;
    const saveRoute = location.hash;
    setSaving(true);
    setError(undefined);
    try {
      await api.createSCMConnection({
        name: draft.name.trim(), kind: draft.kind, repository: draft.repository.trim(), apiUrl: draft.apiUrl.trim() || undefined,
        gitUrl: draft.gitUrl.trim() || undefined, branchPrefix: draft.branchPrefix.trim() || undefined,
        requirePullRequestApproval: draft.approval ? undefined : false, token: draft.token,
      });
      if (location.hash === saveRoute) location.hash = '#/connections';
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setSaving(false);
    }
  };
  return <section>
    <header className="page-head connection-detail-head"><div>
      <a href="#/connections">Connections</a><h1>New source control connection</h1>
      <p className="muted">Configure the repository and credential that tasks use to push branches and open pull requests.</p>
    </div></header>
    {role === 'admin' ? <form className="card form connections-form" aria-label="Add source control connection" onSubmit={e => { e.preventDefault(); void save(); }}>
      <ErrorBox error={error} />
      {kinds.loading && <p role="status">Loading source control services…</p>}
      {kinds.error && <div><ErrorBox error={`Could not load source control services: ${kinds.error}`} /><Btn onClick={() => void kinds.reload()}>Retry loading services</Btn></div>}
      {!kinds.loading && !kinds.error && !kinds.data?.length && <Empty>No source control adapter is available.</Empty>}
      <HelpField label={"Name"} help={"A stable connection name for workflow and task hand-offs. Use a name that makes the intended repository easy to identify."}><input value={draft.name} onChange={(e) => set({ name: e.target.value })} placeholder="team-repository" /></HelpField>
      <HelpField label={"Kind"} help={"The source-control service Weft calls to open pull requests. It determines the default API and Git addresses."}><select disabled={kinds.loading || !kinds.data?.length} value={draft.kind} onChange={(e) => set({ kind: e.target.value, apiUrl: '', gitUrl: '', token: '' })}>{kinds.data?.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></HelpField>
      <HelpField label={"Repository"} help={"The single repository this connection is allowed to use. Use the repository format shown for the selected service."}><input value={draft.repository} onChange={(e) => set({ repository: e.target.value })} placeholder={kind?.repositoryHint ?? 'Repository identifier'} /></HelpField>
      <HelpField label={"Branch prefix"} help={"Only branches under this prefix can be pushed through this connection. For example, weft/ separates task branches from release branches."}><input value={draft.branchPrefix} onChange={(e) => set({ branchPrefix: e.target.value })} placeholder="weft/" /></HelpField>
      <HelpField label={"API URL"} help={"The source-control API address used to open pull requests. Set it for a self-hosted service or leave it empty to use the default for the selected kind."}><input value={draft.apiUrl} onChange={(e) => set({ apiUrl: e.target.value })} placeholder={kind?.apiUrlHint ?? 'Service API URL'} required={kind?.apiUrlRequired} inputMode="url" /></HelpField>
      <HelpField label={"Git URL"} help={"The Git address used to push a task branch. Set it when the repository uses a different Git host or address."}><input value={draft.gitUrl} onChange={(e) => set({ gitUrl: e.target.value })} placeholder="Default for the kind" inputMode="url" /></HelpField>
      <p className="muted connection-token" role="note">{kind?.credentialHelp}</p>
      <HelpField className="connection-token" label={"Token"} help={"A credential for the configured repository. Give it only the permissions needed for task branches and pull requests."}><input type="password" autoComplete="off" value={draft.token} onChange={(e) => set({ token: e.target.value })} /></HelpField>
      <HelpField className="checkbox" label={"Opening a pull request needs a person's approval"} help="Holds a pull request action until a person approves it. The repo, branch, and connection scope are still checked when the action runs."><input type="checkbox" checked={draft.approval} onChange={(e) => set({ approval: e.target.checked })} /></HelpField>
      <div className="btn-row">
        <Btn type="submit" tone="primary" disabled={saving || !kind}>{saving ? 'Saving…' : 'Save connection'}</Btn>
        {saving ? <Btn disabled>Cancel</Btn> : <a href="#/connections">Cancel</a>}
      </div>
    </form> : <p>An administrator must create this connection.</p>}
  </section>;
}
