import { useEffect, useState } from 'react';
import { api, errorMessage } from '../api/client';
import { fileOperations } from './fileOperations';
import { Btn, ErrorBox } from './common';

import type { GitInspection, WeftEvent } from '../api/types';

export function GitView({ graphId, node, replica, path }: { graphId: string; node: string; replica: number; path: string }) {
  const [view, setView] = useState<'status' | 'diff' | 'history'>('status');
  const [revision, setRevision] = useState(0);
  const [result, setResult] = useState<GitInspection>();
  const [error, setError] = useState<string>();
  const [loading, setLoading] = useState(false);
  const [executions, setExecutions] = useState<Awaited<ReturnType<typeof api.executions>>>([]);
  const [events, setEvents] = useState<WeftEvent[]>([]);
  useEffect(() => {
    let cancelled = false;
    setResult(undefined); setError(undefined); setLoading(true); setExecutions([]); setEvents([]);
    Promise.all([
      api.gitInspection(graphId, view, { path, ...(node ? { node, replica } : {}) }),
      api.executions(graphId, { ...(node ? { node } : {}), limit: 20 }),
      api.events(graphId, { types: 'tool.completed,workspace.observed', limit: 100 }),
    ]).then(([inspection, runs, operations]) => {
      if (!cancelled) { setEvents(node ? operations.filter(event => !event.nodeId || (event.nodeId === node && event.replica === replica)) : operations); setResult(inspection); setExecutions(node ? runs.filter(run => run.replica === replica) : runs); }
    }).catch(cause => { if (!cancelled) setError(errorMessage(cause)); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [graphId, node, replica, path, view, revision]);
  return <section aria-label="Git inspection">
    <h3>Git inspection</h3>
    <label>Git view <select value={view} onChange={event => setView(event.target.value as typeof view)}>
      <option value="status">Status</option><option value="diff">Diff</option><option value="history">History</option>
    </select></label>{' '}
    <Btn small onClick={() => setRevision(value => value + 1)} disabled={loading}>Refresh Git</Btn>
    <p className="muted small">{path ? `Path: ${path}` : 'All workspace files'} · {node ? `Agent ${node} · replica ${replica}` : 'Shared workspace'}</p>
    <ErrorBox error={error} />
    {loading && <p>Loading Git…</p>}
    {result && <>
      <p className="small mono">{result.directory} · {result.repository ? `Branch: ${result.branch}` : 'Workspace has no Git repository.'}</p>
      {result.repository && <>
        <pre className="file-content-text">{result.content || (view === 'status' ? 'Working tree clean.' : 'No changes.')}</pre>
        <p className="muted small">Diff compares tracked files with HEAD, including staged changes. Read untracked files in Files. History shows up to 50 commits.</p>
      </>}
      <p className="muted small">Change attribution is inconclusive outside the tool observation window. Observations include shell changes, but concurrent external writers and background processes can affect attribution. Partial scans do not prove absence of changes.</p>
      <p className="small">Recorded operations and workspace observations (latest 100 events; current contents may have changed since):</p>
      <ul>{fileOperations(events, path).map(operation => <li key={operation.id}>{operation.paths.join(' → ')} · {operation.tool} · {operation.outcome} · {operation.node ? `${operation.node} / ${operation.replica}` : 'shared workspace'} · {operation.executionId ? <>execution <span className="mono">{operation.executionId}</span></> : 'execution unknown'}</li>)}</ul>
      <ul>{executions.map(run => <li key={run.id}><span className="mono">{run.id}</span> · {run.nodeId} · replica {run.replica} · {run.status}{run.summary ? ` · ${run.summary}` : ''}{run.error ? ` · ${run.error}` : ''}</li>)}</ul>
      <a href={`#/g/${encodeURIComponent(graphId)}/messages`}>Inspect task messages</a>{' · '}
      <a href={`#/g/${encodeURIComponent(graphId)}/events`}>Inspect execution events</a>
    </>}
  </section>;
}
