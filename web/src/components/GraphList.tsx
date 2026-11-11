import { useState } from 'react';
import { parse } from 'yaml';
import { api, errorMessage } from '../api/client';
import type { Graph, GraphAction, GraphSummary } from '../api/types';
import { Badge, Btn, Empty, ErrorBox, Icon, Modal, StateDot, fmtDateTime } from './common';

const TEMPLATE = `apiVersion: weft.io/v1
kind: Graph
metadata:
  name: hello-factory
  description: A producer feeding a consumer through a durable queue.
spec:
  nodes:
    - id: writer
      runtime: { kind: echo }
      prompt: Write a short idea and hand it to @output.
      loop: { mode: continuous, stop: { policy: iterations, maxIterations: 3 } }
    - id: reader
      runtime: { kind: echo }
      prompt: Read @input and summarise it.
      loop: { mode: event }
  channels:
    - { id: ideas, type: queue, from: writer, to: [reader] }
`;

export function GraphList({
  graphs,
  error,
  onOpen,
  onChanged,
  onAgent,
}: {
  graphs: GraphSummary[];
  error?: string;
  onOpen: (id: string) => void;
  onChanged: () => void;
  onAgent?: (graph: string, agent: string) => void;
}) {
  const [creating, setCreating] = useState(false);
  const [actionError, setActionError] = useState<string>();

  const act = async (id: string, a: GraphAction) => {
    try {
      await api.graphAction(id, a);
      setActionError(undefined);
      onChanged();
    } catch (e) {
      setActionError(errorMessage(e));
    }
  };

  const del = async (s: GraphSummary) => {
    if (!confirm(`Delete graph “${s.graph.metadata.name || s.graph.metadata.id}”? Its history stays in the event log.`)) return;
    try {
      await api.deleteGraph(s.graph.metadata.id);
      onChanged();
    } catch (e) {
      setActionError(errorMessage(e));
    }
  };

  return (
    <div className="page">
      <div className="page-head">
        <h1>Graphs</h1>
        <Btn tone="primary" onClick={() => setCreating(true)}>
          New graph
        </Btn>
      </div>
      <ErrorBox error={error ?? actionError} />
      {graphs.length === 0 && !error && <Empty>No graphs yet. Create one from YAML or JSON.</Empty>}
      <div className="cards">
        {graphs.map((s) => {
          const g = s.graph;
          const st = s.record.state;
          return (
            <article key={g.metadata.id} className={`card graph-card st-${st}`}>
              <header>
                <StateDot state={st === 'running' ? 'running' : st === 'paused' ? 'paused' : st === 'failed' ? 'failed' : 'stopped'} />
                <button type="button" className="link title graph-card-open" onClick={() => onOpen(g.metadata.id)}>
                  {g.metadata.name || g.metadata.id}
                </button>
                <Badge>v{s.record.version}</Badge>
                <Badge tone={st === 'running' ? 'ok' : st === 'failed' ? 'bad' : undefined}>{st}</Badge>
              </header>
              {g.metadata.description && <p className="muted small">{g.metadata.description}</p>}
              <p className="small muted">
                {g.spec.nodes.length} agents · {g.spec.channels?.length ?? 0} channels
                {s.record.startedAt ? ` · started ${fmtDateTime(s.record.startedAt)}` : ''}
              </p>
              <div className="graph-card-agents" aria-label={`Agents in ${g.metadata.name || g.metadata.id}`}>
                <strong>Agents</strong>
                {g.spec.nodes.length ? <ul>{g.spec.nodes.map(node => <li key={node.id}>
                  <button type="button" className="link" onClick={() => onAgent ? onAgent(g.metadata.id, node.id) : onOpen(g.metadata.id)}>Configure {node.name || node.id}</button>
                </li>)}</ul> : <p>No agents configured.</p>}
              </div>
              <div className="btn-row graph-card-actions">
                {st === 'stopped' || st === 'failed' ? (
                  <Btn small tone="primary" onClick={() => act(g.metadata.id, 'start')}>
                    <Icon name="play" /> Start
                  </Btn>
                ) : (
                  <>
                    <Btn small onClick={() => act(g.metadata.id, 'stop')}>
                      <Icon name="stop" /> Stop
                    </Btn>
                    {st === 'paused' ? (
                      <Btn small onClick={() => act(g.metadata.id, 'resume')}>
                        Resume
                      </Btn>
                    ) : (
                      <Btn small onClick={() => act(g.metadata.id, 'pause')}>
                        <Icon name="pause" /> Pause
                      </Btn>
                    )}
                  </>
                )}
                <Btn small tone="ghost" onClick={() => onOpen(g.metadata.id)}>
                  Open
                </Btn>
                <Btn small tone="ghost" onClick={() => del(s)} disabled={st === 'running'} title="Delete">
                  Delete
                </Btn>
              </div>
            </article>
          );
        })}
      </div>
      {creating && (
        <CreateDialog
          initial={TEMPLATE}
          title="New graph"
          onClose={() => setCreating(false)}
          onCreated={(id) => {
            setCreating(false);
            onChanged();
            onOpen(id);
          }}
        />
      )}
    </div>
  );
}

function CreateDialog({
  initial,
  title,
  onClose,
  onCreated,
}: {
  initial: string;
  title: string;
  onClose: () => void;
  onCreated: (id: string) => void;
}) {
  const [text, setText] = useState(initial);
  const [error, setError] = useState<string>();
  const [issues, setIssues] = useState<string[]>([]);

  const doc = (): Graph | { yaml: string } => {
    const t = text.trim();
    if (t.startsWith('{')) return JSON.parse(t) as Graph;
    return { yaml: t };
  };

  const validate = async () => {
    try {
      const r = await api.validateGraph(doc());
      setIssues([
        ...(r.errors ?? []).map((e) => `✗ ${e.path}: ${e.message}`),
        ...(r.warnings ?? []).map((e) => `⚠ ${e.path}: ${e.message}`),
        ...(r.valid ? ['✓ valid'] : []),
      ]);
      setError(undefined);
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  const create = async () => {
    try {
      const s = await api.createGraph(doc());
      onCreated(s.graph.metadata.id);
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  const onFile = async (f?: File) => {
    if (f) setText(await f.text());
  };

  return (
    <Modal title={title} onClose={onClose}>
      <p className="muted small">Paste or upload a graph document (YAML or JSON).</p>
      <input type="file" accept=".yaml,.yml,.json" onChange={(e) => void onFile(e.target.files?.[0])} />
      <textarea className="code-editor mono" rows={18} value={text} onChange={(e) => setText(e.target.value)} spellCheck={false} />
      {issues.length > 0 && (
        <ul className="small">
          {issues.map((i, k) => (
            <li key={k}>{i}</li>
          ))}
        </ul>
      )}
      <ErrorBox error={error} />
      <div className="btn-row">
        <Btn onClick={validate}>Validate</Btn>
        <Btn tone="primary" onClick={create}>
          Create
        </Btn>
      </div>
    </Modal>
  );
}

// Re-exported for tests / other screens that want to parse documents the same way.
export function parseGraphText(text: string): Graph {
  const t = text.trim();
  return (t.startsWith('{') ? JSON.parse(t) : parse(t)) as Graph;
}
