import { useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { SCMConnection } from '../api/types';
import { useAsync } from '../hooks/useData';
import { Badge, Btn, ErrorBox } from './common';

type Draft = { name: string; kind: string; repository: string; apiUrl: string; gitUrl: string; branchPrefix: string; approval: boolean; token: string };
const empty: Draft = { name: '', kind: 'github', repository: '', apiUrl: '', gitUrl: '', branchPrefix: '', approval: true, token: '' };

/** Server-held source control connections for push and pull request tools. */
export function SCMConnectionsSection({ role }: { role?: string }) {
  const list = useAsync(api.scmConnections, []);
  const [draft, setDraft] = useState<Draft>();
  const [results, setResults] = useState<Record<string, string>>({});
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
    try {
      const r = await api.testSCMConnection(c.name);
      setResults((cur) => ({ ...cur, [c.name]: r.ok && r.identity ? `Works as ${r.identity.user}; default branch ${r.identity.defaultBranch || 'unknown'}; ${r.identity.canPush ? 'can push' : 'cannot push'}.` : `Test failed: ${r.error}` }));
    } catch (e) {
      setResults((cur) => ({ ...cur, [c.name]: `Test could not run: ${errorMessage(e)}` }));
    }
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
    <section className="integration-section" aria-labelledby="scm-heading">
      <div className="section-head">
        <div>
          <h2 id="scm-heading">Source control</h2>
          <p className="muted small">Tasks push their branch and open pull requests through these connections. Tokens stay on the server; agents never see them or run an SCM CLI.</p>
        </div>
        {admin && !draft && <Btn tone="primary" small onClick={() => setDraft(empty)}>Add source control connection</Btn>}
      </div>
      <ErrorBox error={list.error ? `Could not load source control connections: ${list.error}` : error} />
      {(list.data ?? []).length === 0 && !list.loading && <p className="empty">No source control connection yet.</p>}
      <ul className="integration-grid">
        {(list.data ?? []).map((c) => (
          <li key={c.name} className="integration-card" aria-label={`Source control ${c.name}`}>
            <div className="integration-card-head">
              <strong>{c.name}</strong>
              <span className="integration-card-status"><Badge>{c.kind}</Badge>{!c.tokenConfigured && <Badge tone="warn">No token</Badge>}</span>
            </div>
            <p className="small">{c.repository} · branches {c.branchPrefix || 'weft/'}* · {c.requirePullRequestApproval === false ? 'pull requests open without approval' : 'pull requests need approval'}</p>
            {results[c.name] && <p role="status" className="small">{results[c.name]}</p>}
            <div className="row">
              {canTest && <Btn small onClick={() => void test(c)}>Test connection</Btn>}
              {admin && <Btn small tone="danger" onClick={() => void remove(c)}>Remove</Btn>}
            </div>
          </li>
        ))}
      </ul>
      {draft && (
        <form className="card form" aria-label="Add source control connection" onSubmit={(e) => { e.preventDefault(); void save(); }}>
          <label>Name<input value={draft.name} onChange={(e) => set({ name: e.target.value })} placeholder="team-github" /></label>
          <label>Kind<select value={draft.kind} onChange={(e) => set({ kind: e.target.value })}><option value="github">GitHub</option><option value="bitbucket-dc">Bitbucket Data Center</option></select></label>
          <label>Repository<input value={draft.repository} onChange={(e) => set({ repository: e.target.value })} placeholder="owner/name" /></label>
          <label>API URL<input value={draft.apiUrl} onChange={(e) => set({ apiUrl: e.target.value })} placeholder="Default for the kind" inputMode="url" /></label>
          <label>Git URL<input value={draft.gitUrl} onChange={(e) => set({ gitUrl: e.target.value })} placeholder="Default for the kind" inputMode="url" /></label>
          <label>Branch prefix<input value={draft.branchPrefix} onChange={(e) => set({ branchPrefix: e.target.value })} placeholder="weft/" /></label>
          <label className="checkbox"><input type="checkbox" checked={draft.approval} onChange={(e) => set({ approval: e.target.checked })} /> Opening a pull request needs a person's approval</label>
          <label>Token<input type="password" autoComplete="off" value={draft.token} onChange={(e) => set({ token: e.target.value })} /></label>
          <div className="row">
            <Btn type="submit" tone="primary">Save connection</Btn>
            <Btn type="button" onClick={() => setDraft(undefined)}>Cancel</Btn>
          </div>
        </form>
      )}
    </section>
  );
}
