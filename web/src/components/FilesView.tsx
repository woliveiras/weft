import { useEffect, useRef, useState } from 'react';
import { GitView } from './GitView';
import { api, errorMessage } from '../api/client';
import type { FileEntry, GraphStatus, GraphSummary } from '../api/types';
import { Btn, ErrorBox, fmtDateTime } from './common';

type FilesProps = { summary: GraphSummary; status?: GraphStatus };
export function FilesView(props: FilesProps) {
  return <WorkspaceFiles key={props.summary.graph.metadata.id} {...props} />;
}
function WorkspaceFiles({ summary, status }: FilesProps) {
  const graph = summary.graph;
  const graphId = graph.metadata.id;
  const worktreeNodes = graph.spec.nodes.filter((node) => node.workspace?.mode === 'worktree');
  const [nodeId, setNodeId] = useState('');
  const [replica, setReplica] = useState(0);
  const [path, setPath] = useState('');
  const [entries, setEntries] = useState<FileEntry[]>([]);
  const [selected, setSelected] = useState<FileEntry>();
  const [content, setContent] = useState('');
  const [loading, setLoading] = useState(false);
  const [contentLoading, setContentLoading] = useState(false);
  const contentRequest = useRef(0);
  const [error, setError] = useState<string>();
  const replicaCount = Math.max(status?.nodes?.find((node) => node.nodeId === nodeId)?.desiredReplicas ?? 1, 1);
  const location = nodeId ? { node: nodeId, replica } : {};

  useEffect(() => {
    let cancelled = false;
    contentRequest.current++;
    setContentLoading(false);
    setEntries([]);
    setLoading(true);
    setError(undefined);
    setSelected(undefined);
    setContent('');
    api
      .files(graphId, { path, ...location })
      .then((result) => {
        if (!cancelled) setEntries(result);
      })
      .catch((cause: unknown) => {
        if (!cancelled) setError(errorMessage(cause));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
      contentRequest.current++;
    };
  }, [graphId, path, nodeId, replica]);

  const openFile = async (entry: FileEntry) => {
    const request = ++contentRequest.current;
    setSelected(entry);
    setContent('');
    setContentLoading(true);
    setError(undefined);
    try {
      const value = await api.fileContent(graphId, { path: entry.path, ...location });
      if (request === contentRequest.current) setContent(value);
    } catch (cause) {
      if (request === contentRequest.current) {
        setError(errorMessage(cause));
        setContent('');
      }
    } finally {
      if (request === contentRequest.current) setContentLoading(false);
    }
  };

  const segments = path.split('/').filter(Boolean);

  return (
    <div className="files-view">
      <aside className="file-browser">
        <div className="file-browser-head">
          <strong>Workspace files</strong>
          {worktreeNodes.length > 0 && (
            <>
              <label className="small muted">
                Workspace
                <select
                  value={nodeId}
                  onChange={(event) => {
                    setNodeId(event.target.value);
                    setReplica(0);
                    setPath('');
                  }}
                >
                  <option value="">Shared workspace</option>
                  {worktreeNodes.map((node) => (
                    <option value={node.id} key={node.id}>
                      {node.name || node.id} · worktree
                    </option>
                  ))}
                </select>
              </label>
              {nodeId && replicaCount > 1 && (
                <label className="small muted">
                  Replica
                  <select value={replica} onChange={(event) => setReplica(Number(event.target.value))}>
                    {Array.from({ length: replicaCount }, (_, index) => (
                      <option key={index} value={index}>
                        {index}
                      </option>
                    ))}
                  </select>
                </label>
              )}
            </>
          )}
          <div className="file-breadcrumb">
            <button type="button" className="link small" onClick={() => setPath('')}>
              root
            </button>
            {segments.map((segment, index) => {
              const dest = segments.slice(0, index + 1).join('/');
              return (
                <span key={dest}>
                  {' / '}
                  <button type="button" className="link small" onClick={() => setPath(dest)}>
                    {segment}
                  </button>
                </span>
              );
            })}
          </div>
        </div>
        <ErrorBox error={error} />
        {loading ? (
          <p className="muted small">Loading files…</p>
        ) : entries.length === 0 ? (
          <p className="muted small">No files in this directory.</p>
        ) : (
          <ul className="file-list">
            {entries.map((entry) => (
              <li key={entry.path}>
                <button
                  type="button"
                  className={`file-entry ${selected?.path === entry.path ? 'selected' : ''}`}
                  onClick={() => {
                    if (entry.dir) {
                      setPath(entry.path);
                      setSelected(undefined);
                      setContent('');
                    } else {
                      void openFile(entry);
                    }
                  }}
                >
                  <span aria-hidden="true">{entry.dir ? '▸' : '·'}</span>
                  <span className="file-name">{entry.name}</span>
                  {!entry.dir && <span className="muted tiny">{formatSize(entry.size)}</span>}
                </button>
              </li>
            ))}
          </ul>
        )}
        <p className="muted tiny file-readonly">Read-only view. Workspace files can only be changed by agents.</p>
      </aside>
      <section className="file-content" aria-label="File contents">
        <GitView graphId={graphId} node={nodeId} replica={replica} path={selected?.path ?? path} />
        {selected ? (
          <>
            <div className="file-content-head">
              <div>
                <strong className="mono">{selected.path}</strong>
                <div className="muted tiny">
                  {formatSize(selected.size)} · modified {fmtDateTime(selected.modTime)}
                  {selected.size >= 4 * 1024 * 1024 ? ' · content capped at 4 MiB' : ''}
                </div>
              </div>
              <Btn
                small
                tone="ghost"
                onClick={() => void openFile(selected)}
                disabled={contentLoading}
                aria-label="Refresh file"
              >
                Refresh
              </Btn>
            </div>
            {contentLoading ? <p className="muted small">Loading content…</p> : <pre className="file-content-text">{content}</pre>}
          </>
        ) : (
          <p className="muted small">Select a file to inspect its contents.</p>
        )}
      </section>
    </div>
  );
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
}
