import { useEffect, useRef, useState } from 'react';
import { parse, stringify } from 'yaml';
import { api, errorMessage } from '../api/client';
import type { Graph, GraphSummary } from '../api/types';
import { useAsync } from '../hooks/useData';
import { Btn, ErrorBox, fmtDateTime } from './common';

/**
 * The graph draft as YAML: the advanced view of the same draft the canvas
 * edits. Valid YAML updates the shared draft as you type, so switching
 * views never loses work; saving, discarding and validation live in the
 * workspace toolbar and use the same version check as the canvas.
 */
export function YamlEditor({
  summary,
  draft,
  dirty,
  onChange,
  onLoadVersion,
  onSave,
}: {
  summary: GraphSummary;
  draft: Graph;
  dirty: boolean;
  onChange: (graph: Graph) => void;
  onLoadVersion: (graph: Graph) => void;
  /** Cmd/Ctrl+S: the workspace save (validates, then saves with the version check). */
  onSave: () => void;
}) {
  const gid = summary.graph.metadata.id;
  const [text, setText] = useState(() => stringify(draft));
  const [parseError, setParseError] = useState<string>();
  const [error, setError] = useState<string>();
  const [notice, setNotice] = useState<string>();
  const versions = useAsync(() => api.graphVersions(gid), [gid, summary.record.version]);
  // The draft this editor produced last; any other draft came from elsewhere
  // (canvas, discard, save) and replaces the text.
  const emitted = useRef<Graph>(draft);

  useEffect(() => {
    if (draft === emitted.current) return;
    emitted.current = draft;
    setText(stringify(draft));
    setParseError(undefined);
  }, [draft]);

  const edit = (next: string) => {
    setText(next);
    setNotice(undefined);
    let graph: unknown;
    try {
      graph = parse(next);
    } catch (e) {
      setParseError(`YAML: ${(e as Error).message}`);
      return;
    }
    if (!graph || typeof graph !== 'object' || Array.isArray(graph) || !('spec' in graph)) {
      setParseError('YAML: the document must be a mapping with a spec.');
      return;
    }
    setParseError(undefined);
    emitted.current = graph as Graph;
    onChange(graph as Graph);
  };

  const loadVersion = async (v: number) => {
    try {
      const s = await api.getGraph(gid, v);
      onLoadVersion(s.graph);
      setNotice(`Loaded version ${v} into the draft – save to restore it as a new version.`);
      setError(undefined);
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  return (
    <div className="page fill split">
      <div className="split-main">
        <div className="page-head">
          <h2>Definition</h2>
          <span className="muted small">
            based on version {summary.record.version}
            {dirty ? ' · unsaved changes' : ''}
          </span>
          <Btn small tone="ghost" onClick={() => void download(gid).catch((e) => setError(errorMessage(e)))}>
            Export saved
          </Btn>
        </div>
        <p className="muted small">
          Advanced view of the graph draft. Keys starting with <code>x-</code> on the graph, metadata, agents and channels
          are kept as written; other unknown keys are rejected when you validate or save.
        </p>
        <ErrorBox error={error} />
        {parseError && (
          <div className="error-box" role="alert">
            {parseError}
            <div className="small">The graph view and Save use the last valid YAML until this is fixed.</div>
          </div>
        )}
        {notice && <div className="ok small">{notice}</div>}
        <textarea
          className="code-editor mono"
          spellCheck={false}
          value={text}
          onChange={(e) => edit(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Tab') {
              e.preventDefault();
              const t = e.currentTarget;
              const s = t.selectionStart;
              const v = t.value.slice(0, s) + '  ' + t.value.slice(t.selectionEnd);
              edit(v);
              requestAnimationFrame(() => t.setSelectionRange(s + 2, s + 2));
            }
            if (e.key === 's' && (e.metaKey || e.ctrlKey)) {
              e.preventDefault();
              onSave();
            }
          }}
          aria-label="Graph YAML"
        />
      </div>
      <aside className="split-side narrow">
        <h3>Versions</h3>
        <ErrorBox error={versions.error} />
        <ul className="versions">
          {[...(versions.data ?? [])].reverse().map((v) => (
            <li key={v.version}>
              <button type="button" className="link" onClick={() => loadVersion(v.version)}>
                v{v.version}
              </button>{' '}
              <span className="muted small">
                {fmtDateTime(v.createdAt)}
                {v.author ? ` · ${v.author}` : ''}
              </span>
              {v.version === summary.record.version && <span className="small"> (current)</span>}
            </li>
          ))}
        </ul>
      </aside>
    </div>
  );
}

/** The export endpoint needs the bearer token, so download via fetch + blob rather than a plain link. */
async function download(gid: string) {
  const text = await api.exportGraph(gid, 'yaml');
  const url = URL.createObjectURL(new Blob([text], { type: 'application/yaml' }));
  const a = document.createElement('a');
  a.href = url;
  a.download = `${gid}.yaml`;
  a.click();
  URL.revokeObjectURL(url);
}
