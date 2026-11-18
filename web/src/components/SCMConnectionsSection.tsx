import { HelpField } from './FieldHelp';
import { useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { SCMConnection } from '../api/types';
import { useAsync } from '../hooks/useData';
import { Badge, Btn, Empty, ErrorBox } from './common';

type Draft = { name: string; kind: string; repository: string; apiUrl: string; gitUrl: string; branchPrefix: string; approval: boolean; token: string };
const empty: Draft = { name: '', kind: 'github', repository: '', apiUrl: '', gitUrl: '', branchPrefix: '', approval: true, token: '' };
const serviceNames: Record<string, string> = { github: 'GitHub', 'bitbucket-dc': 'Bitbucket Data Center' };

/** Server-held source control connections for push and pull request tools. */
export function SCMConnectionsSection({ role }: { role?: string }) {
  const list = useAsync(api.scmConnections, []);
  const [draft, setDraft] = useState<Draft>();
  const [results, setResults] = useState<Record<string, { message: string; pending?: boolean }>>({});
  const [error, setError] = useState<string>();
  const admin = role === 'admin';
  const canTest = admin || role === 'operator';
  const save = async () => {
    if (!draft) return;
    setError(undefined);
    try {
      await api.createSCMConnection({
        name: draft.name.trim(), kind: draft.kind, repository: draft.repository.trim(), apiUrl: draft.apiUrl.trim() || undefined,
        gitUrl: draft.gitUrl.trim() || undefined, branchPrefix: draft.branchPrefix.trim() || undefined,
        requirePullRequestApproval: draft.approval ? undefined : false, token: draft.token,
      });
      setDraft(undefined);
      await list.reload();
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  const test = async (c: SCMConnection) => {
    setResults(cur => ({ ...cur, [c.name]: { message: 'Testing connection…', pending: true } }));
    let message: string;
    try {
      const r = await api.testSCMConnection(c.name);
      message = r.ok && r.identity
        ? `Works as ${r.identity.user}; default branch ${r.identity.defaultBranch || 'unknown'}; ${r.identity.canPush ? 'can push' : 'cannot push'}.`
        : `Test failed: ${r.error || 'No connection identity returned.'}`;
    } catch (e) {
      message = `Test could not run: ${errorMessage(e)}`;
    }
    setResults(cur => ({ ...cur, [c.name]: { message } }));
  };
  const remove = async (c: SCMConnection) => {
    if (!confirm(`Remove the source control connection "${c.name}"? Tasks that use it can no longer push.`)) return;
    try {
      await api.deleteSCMConnection(c.name);
      await list.reload();
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  const set = (patch: Partial<Draft>) => setDraft((d) => ({ ...(d ?? empty), ...patch }));
  return (
    <section className="card connections-panel" aria-labelledby="scm-heading">
      <header className="connections-panel-head">
        <div>
          <h2 id="scm-heading">Source control</h2>
          <p className="muted">Let tasks push branches and open pull requests. Weft keeps the credentials on the server.</p>
        </div>
        {admin && !draft && <Btn tone="primary" onClick={() => { setError(undefined); setDraft(empty); }}>Add source control connection</Btn>}
      </header>
      {list.error && <div className="connections-load-error">
        <ErrorBox error={`Could not load source control connections: ${list.error}`} />
        <Btn disabled={list.loading} onClick={() => void list.reload()}>Reload source control connections</Btn>
      </div>}
      {!draft && <ErrorBox error={error} />}
      {draft && (
        <form className="form connections-form" aria-label="Add source control connection" onSubmit={(e) => { e.preventDefault(); void save(); }}>
          <h3>New source control connection</h3>
          <ErrorBox error={error} />
          <HelpField label={"Name"} help={"A stable connection name for workflow and task hand-offs. Use a name that makes the intended repository easy to identify."}><input value={draft.name} onChange={(e) => set({ name: e.target.value })} placeholder="team-github" /></HelpField>
          <HelpField label={"Kind"} help={"The source-control service Weft calls to open pull requests. It determines the default API and Git addresses."}><select value={draft.kind} onChange={(e) => set({ kind: e.target.value })}><option value="github">GitHub</option><option value="bitbucket-dc">Bitbucket Data Center</option></select></HelpField>
          <HelpField label={"Repository"} help={"The single repository this connection is allowed to use. Enter it as owner/name."}><input value={draft.repository} onChange={(e) => set({ repository: e.target.value })} placeholder="owner/name" /></HelpField>
          <HelpField label={"Branch prefix"} help={"Only branches under this prefix can be pushed through this connection. For example, weft/ separates task branches from release branches."}><input value={draft.branchPrefix} onChange={(e) => set({ branchPrefix: e.target.value })} placeholder="weft/" /></HelpField>
          <HelpField label={"API URL"} help={"The source-control API address used to open pull requests. Set it for a self-hosted service or leave it empty to use the default for the selected kind."}><input value={draft.apiUrl} onChange={(e) => set({ apiUrl: e.target.value })} placeholder="Default for the kind" inputMode="url" /></HelpField>
          <HelpField label={"Git URL"} help={"The Git address used to push a task branch. Set it when the repository uses a different Git host or address."}><input value={draft.gitUrl} onChange={(e) => set({ gitUrl: e.target.value })} placeholder="Default for the kind" inputMode="url" /></HelpField>
          <HelpField className="connection-token" label={"Token"} help={"A credential for the configured repository. Give it only the permissions needed for task branches and pull requests."}><input type="password" autoComplete="off" value={draft.token} onChange={(e) => set({ token: e.target.value })} /></HelpField>
          <HelpField className="checkbox" label={"Opening a pull request needs a person's approval"} help="Holds a pull request action until a person approves it. The repo, branch, and connection scope are still checked when the action runs."><input type="checkbox" checked={draft.approval} onChange={(e) => set({ approval: e.target.checked })} /></HelpField>
          <div className="btn-row">
            <Btn type="submit" tone="primary">Save connection</Btn>
            <Btn onClick={() => { setError(undefined); setDraft(undefined); }}>Cancel</Btn>
          </div>
        </form>
      )}
      {list.loading && <p className="muted" role="status">Loading source control connections…</p>}
      {!list.loading && !list.error && !list.data?.length && <Empty>No source control connection yet.</Empty>}
      {!!list.data?.length && <div className="connection-table connections-table" role="region" aria-label="Source control connection list" tabIndex={0}>
        <table><caption className="sr-only">Source control connections</caption>
          <thead><tr><th scope="col">Connection</th><th scope="col">Repository and branches</th><th scope="col">Pull request approval</th><th scope="col">Credential</th>{canTest && <th scope="col">Actions</th>}</tr></thead>
          <tbody>{list.data.map(c => <tr key={c.name}>
            <td><strong>{c.name}</strong><span className="small muted connection-meta">{serviceNames[c.kind] ?? c.kind}</span>
              {results[c.name] && <p role="status" className="small connection-test-result">{results[c.name].message}</p>}
            </td>
            <td>{c.repository}<code className="small muted connection-meta">{c.branchPrefix || 'weft/'}*</code></td>
            <td>{c.requirePullRequestApproval === false ? 'Not required' : 'Required'}</td>
            <td>{c.tokenConfigured ? 'Configured' : <Badge tone="warn">No token</Badge>}</td>
            {canTest && <td><div className="btn-row">
              <Btn small disabled={results[c.name]?.pending} onClick={() => void test(c)}>Test connection</Btn>
              {admin && <Btn small tone="danger" disabled={results[c.name]?.pending} onClick={() => void remove(c)}>Remove</Btn>}
            </div></td>}
          </tr>)}</tbody>
        </table>
      </div>}
    </section>
  );
}
