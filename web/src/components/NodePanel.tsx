import { FieldHelp, HelpField } from './FieldHelp';
import { MCPGrants } from './MCPGrants';
import { useEffect, useRef, useState } from 'react';
import { api, errorMessage } from '../api/client';
import {
  grantName,
  type GraphAction,
  type GraphNode,
  type GraphStatus,
  type GraphSummary,
  type WeftEvent,
  type ToolGrant,
  type ToolInfo,
} from '../api/types';
import { useAsync, useThrottled } from '../hooks/useData';
import { matches, useEventStream } from '../hooks/useEventStream';
import { Badge, Btn, Empty, ErrorBox, Icon, StateDot, fmtTime, since, stateLabel } from './common';
import { describeEvent, eventTone, foldLiveOutput } from './describe';
import type { ElementIssue } from './canvas/edit';
import { effectiveLoopMode } from './canvas/model';
import { ElementIssues, FieldIssues } from './GraphIssues';
import { AgentEnvironment } from './AgentEnvironment';
import { ModelFields } from './ModelFields';

const LOG_TYPES = [
  'agent.output',
  'agent.broadcast',
  'operator.message',
  'execution.started',
  'execution.completed',
  'execution.failed',
  'tool.invoked',
  'tool.completed',
  'tool.denied',
  'approval.requested',
  'approval.decided',
  'replica.state_changed',
];

interface Props {
  summary: GraphSummary;
  status?: GraphStatus;
  nodeId: string;
  onSaved: (s: GraphSummary) => void;
  onStatus: (s: GraphStatus) => void;
  onUpdateNode?: (nodeId: string, update: Partial<GraphNode>) => void;
  /** Validation findings for this agent. */
  issues?: ElementIssue[];
  onConfigure?: () => void;
  onRemove?: () => void;
}

export function NodePanel({ summary, status, nodeId, onSaved, onStatus, onUpdateNode, issues, onConfigure, onRemove }: Props) {
  const graph = summary.graph;
  const gid = graph.metadata.id;
  const node = graph.spec.nodes.find((n) => n.id === nodeId);
  const ns = status?.nodes?.find((n) => n.nodeId === nodeId);
  const [replica, setReplica] = useState(-1);
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [scale, setScale] = useState<number>(ns?.desiredReplicas ?? 1);

  useEffect(() => {
    setReplica(-1);
    setError(undefined);
  }, [nodeId]);
  useEffect(() => {
    if (ns) setScale(ns.desiredReplicas);
  }, [ns?.desiredReplicas]); // eslint-disable-line react-hooks/exhaustive-deps

  if (!node) return <Empty>Node “{nodeId}” is not in this graph version.</Empty>;

  const act = async (a: GraphAction) => {
    if (a === 'kill' && !confirm(`Kill ${node.name || node.id}? Turns in flight are cancelled and their inputs redelivered.`))
      return;
    setBusy(true);
    try {
      onStatus(await api.nodeAction(gid, nodeId, a));
      setError(undefined);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  const doScale = async () => {
    setBusy(true);
    try {
      onStatus(await api.scaleNode(gid, nodeId, scale));
      setError(undefined);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  const desired = Math.max(ns?.desiredReplicas ?? 1, ns?.replicas?.length ?? 0, 1);
  const replicas = Array.from({ length: desired }, (_, i) => ns?.replicas?.find((r) => r.replica === i));
  const sel = replica >= 0 ? replicas[replica] : undefined;

  return (
    <div className="panel-body">
      <div className="panel-title">
        <StateDot state={ns?.state ?? 'stopped'} />
        <h2>{node.name || node.id}</h2>
        <Badge>{stateLabel(ns?.state ?? 'stopped')}</Badge>
        {node.disabled && <Badge tone="muted">disabled</Badge>}
      </div>
      {node.description && <p className="muted">{node.description}</p>}
      <div className="btn-row">
        {onConfigure && <Btn tone="primary" onClick={onConfigure}>Configure agent</Btn>}
        {onRemove && <Btn tone="danger" onClick={onRemove}>Remove agent</Btn>}
      </div>
      <AgentEnvironment graph={graph} node={node} />
      <ElementIssues issues={issues} label="Agent validation" />

      <div className="btn-row">
        <Btn tone="primary" small disabled={busy || node.disabled} onClick={() => act('start')} title="Start">
          <Icon name="play" /> Start
        </Btn>
        <Btn small disabled={busy} onClick={() => act('stop')} title="Stop after the current turn">
          <Icon name="stop" /> Stop
        </Btn>
        <Btn small disabled={busy} onClick={() => act('pause')}>
          <Icon name="pause" /> Pause
        </Btn>
        <Btn small disabled={busy} onClick={() => act('resume')}>
          Resume
        </Btn>
        <Btn small tone="danger" disabled={busy} onClick={() => act('kill')} title="Kill turns in flight now">
          <Icon name="bolt" />
        </Btn>
      </div>
      <ErrorBox error={error} />

      <section>
        <h3>Replicas</h3>
        <div className="seg" role="tablist" aria-label="Replica">
          <button
            type="button"
            role="tab"
            aria-selected={replica === -1}
            className={replica === -1 ? 'on' : ''}
            onClick={() => setReplica(-1)}
          >
            all
          </button>
          {replicas.map((r, i) => (
            <button
              type="button"
              role="tab"
              key={i}
              aria-selected={replica === i}
              className={replica === i ? 'on' : ''}
              onClick={() => setReplica(i)}
              title={r ? `${stateLabel(r.state)} · turn ${r.iteration}${r.branch ? ` · ${r.branch}` : ''}` : 'not started'}
            >
              <StateDot state={r?.state ?? 'stopped'} /> n{i}
            </button>
          ))}
        </div>
        {sel && (
          <dl className="kv small">
            <dt>state</dt>
            <dd>
              {stateLabel(sel.state)}
              {sel.detail ? ` – ${sel.detail}` : ''}
            </dd>
            <dt>turns</dt>
            <dd>
              {sel.iteration} total, {sel.runIteration} this run
            </dd>
            {sel.instance && (
              <>
                <dt>instance</dt>
                <dd>{sel.instance}</dd>
              </>
            )}
            {sel.branch && (
              <>
                <dt>branch</dt>
                <dd className="mono">{sel.branch}</dd>
              </>
            )}
            {sel.workdir && (
              <>
                <dt>workdir</dt>
                <dd className="mono">{sel.workdir}</dd>
              </>
            )}
            {sel.deadline && (
              <>
                <dt>deadline</dt>
                <dd>{fmtTime(sel.deadline)}</dd>
              </>
            )}
          </dl>
        )}
        <div className="inline-form">
          <label>
            Scale to{' '}
            <input
              type="number"
              min={0}
              max={64}
              value={scale}
              onChange={(e) => setScale(Number(e.target.value))}
              aria-label="Replica count"
            />
          </label>
          <Btn small disabled={busy || scale === ns?.desiredReplicas} onClick={doScale}>
            Apply
          </Btn>
        </div>
      </section>

      <OutputLog graphId={gid} nodeId={nodeId} replica={replica} />
      <ChatBox graphId={gid} nodeId={nodeId} replica={replica} />
      <PromptEditor summary={summary} nodeId={nodeId} onSaved={onSaved} onUpdateNode={onUpdateNode} issues={issues} />
      {onUpdateNode && <AgentEditor summary={summary} nodeId={nodeId} onUpdateNode={onUpdateNode} issues={issues} />}
      <NodeSettings summary={summary} nodeId={nodeId} onUpdateNode={onUpdateNode} issues={issues} />
      <Executions graphId={gid} nodeId={nodeId} replica={replica} />
    </div>
  );
}

function OutputLog({ graphId, nodeId, replica }: { graphId: string; nodeId: string; replica: number }) {
  const [lines, setLines] = useState<WeftEvent[]>([]);
  const [follow, setFollow] = useState(true);
  const box = useRef<HTMLDivElement>(null);
  const filter = { graph: graphId, node: nodeId, replica, types: LOG_TYPES };

  useEffect(() => {
    let cancelled = false;
    setLines([]);
    api
      .events(graphId, { node: nodeId, types: LOG_TYPES.join(','), limit: 300 })
      .then((evs) => {
        if (!cancelled) setLines((cur) => mergeEvents(evs.filter((e) => matches(e, filter)), cur));
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [graphId, nodeId, replica]);

  useEventStream(filter, (ev) => setLines((cur) => mergeEvents(cur, [ev]).slice(-1000)));

  useEffect(() => {
    if (follow && box.current) box.current.scrollTop = box.current.scrollHeight;
  }, [lines, follow]);

  return (
    <section>
      <div className="section-head">
        <h3>Live output</h3>
        <label className="small">
          <input type="checkbox" checked={follow} onChange={(e) => setFollow(e.target.checked)} /> follow
        </label>
      </div>
      <div className="log" ref={box} aria-live="polite">
        {lines.length === 0 && <div className="muted">No output yet.</div>}
        {foldLiveOutput(lines).map((ev) => (
          <div key={ev.id} className={`log-line ${eventTone(ev)}`}>
            <span className="log-time">{fmtTime(ev.time)}</span>
            {replica < 0 && <span className="log-rep">n{ev.replica ?? 0}</span>}
            <span className="log-text">{describeEvent(ev)}</span>
          </div>
        ))}
      </div>
    </section>
  );
}

function mergeEvents(a: WeftEvent[], b: WeftEvent[]): WeftEvent[] {
  const seen = new Set(a.map((e) => e.id));
  const out = [...a, ...b.filter((e) => !seen.has(e.id))];
  out.sort((x, y) => x.seq - y.seq);
  return out;
}

function ChatBox({ graphId, nodeId, replica }: { graphId: string; nodeId: string; replica: number }) {
  const [text, setText] = useState('');
  const [error, setError] = useState<string>();
  const history = useAsync(() => api.chatHistory(graphId, nodeId), [graphId, nodeId]);
  const reload = useThrottled(() => void history.reload(), 300);
  useEventStream({ graph: graphId, node: nodeId, types: ['operator.message'] }, reload);

  const send = async () => {
    if (!text.trim()) return;
    try {
      await api.sendChat(graphId, nodeId, text.trim(), replica);
      setText('');
      setError(undefined);
      void history.reload();
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  const msgs = (history.data ?? []).filter((m) => replica < 0 || m.replica < 0 || m.replica === replica).slice(-8);
  return (
    <section>
      <h3>Talk to {replica >= 0 ? `n${replica}` : 'the agent'}</h3>
      {msgs.length > 0 && (
        <ul className="chat">
          {msgs.map((m) => (
            <li key={m.id}>
              <span className="muted small">
                {fmtTime(m.createdAt)} {m.author}
                {m.replica >= 0 ? ` → n${m.replica}` : ''} {m.consumedAt ? '✓ read' : '· queued'}
              </span>
              <div>{m.text}</div>
            </li>
          ))}
        </ul>
      )}
      <div className="chat-input">
        <textarea
          rows={2}
          value={text}
          placeholder="Message wakes a waiting loop; mid-turn it is delivered on the next turn."
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) void send();
          }}
          aria-label="Operator message"
        />
        <Btn tone="primary" small onClick={send} disabled={!text.trim()}>
          Send
        </Btn>
      </div>
      <ErrorBox error={error} />
    </section>
  );
}

function PromptEditor({
  summary,
  nodeId,
  onSaved,
  onUpdateNode,
  issues,
}: {
  summary: GraphSummary;
  nodeId: string;
  onSaved: (s: GraphSummary) => void;
  onUpdateNode?: (nodeId: string, update: Partial<GraphNode>) => void;
  issues?: ElementIssue[];
}) {
  const node = summary.graph.spec.nodes.find((n) => n.id === nodeId)!;
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(node.prompt ?? '');
  const [error, setError] = useState<string>();
  useEffect(() => {
    if (!editing) setDraft(node.prompt ?? '');
  }, [node.prompt, editing]);

  const save = async () => {
    if (onUpdateNode) {
      onUpdateNode(nodeId, { prompt: draft });
      setEditing(false);
      setError(undefined);
      return;
    }
    const g = structuredClone(summary.graph);
    const n = g.spec.nodes.find((x) => x.id === nodeId)!;
    n.prompt = draft;
    try {
      const s = await api.updateGraph(g.metadata.id, g, summary.record.version);
      onSaved(s);
      setEditing(false);
      setError(undefined);
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  return (
    <section>
      <div className="section-head">
        <h3>Prompt</h3>
        {!editing ? (
          <Btn small tone="ghost" onClick={() => setEditing(true)}>
            Edit
          </Btn>
        ) : (
          <span className="btn-row">
            <Btn small tone="primary" onClick={save}>
              {onUpdateNode ? 'Apply to draft' : `Save v${summary.record.version + 1}`}
            </Btn>
            <Btn small tone="ghost" onClick={() => setEditing(false)}>
              Cancel
            </Btn>
          </span>
        )}
      </div>
      {editing ? (
        <textarea className="prompt-edit mono" rows={10} value={draft} onChange={(e) => setDraft(e.target.value)} />
      ) : (
        <pre className="prompt">{highlightTokens(node.prompt ?? '')}</pre>
      )}
      <FieldIssues issues={issues} field="prompt" />
      <p className="muted small">
        {onUpdateNode
          ? 'Prompt edits join the graph draft; once saved, replicas pick them up on their next turn.'
          : "Prompt changes are picked up on the replica's next turn."}
      </p>
      <ErrorBox error={error} />
    </section>
  );
}

/** Colour @tokens (parameters, @input/@output/@project/@loop). */
function highlightTokens(s: string) {
  const parts = s.split(/(@[A-Za-z_][\w-]*)/g);
  return parts.map((p, i) =>
    p.startsWith('@') ? (
      <span key={i} className={['@input', '@output', '@project', '@loop'].includes(p) ? 'tok-res' : 'tok-param'}>
        {p}
      </span>
    ) : (
      p
    ),
  );
}

function grantDetail(g: ToolGrant | string): string {
  if (typeof g === 'string') return '';
  const bits = [
    g.paths?.length ? `paths: ${g.paths.join(', ')}` : '',
    g.commands?.length ? `commands: ${g.commands.join(', ')}` : '',
    g.hosts?.length ? `hosts: ${g.hosts.join(', ')}` : '',
    g.rateLimit ? `${g.rateLimit}/min` : '',
  ].filter(Boolean);
  return bits.join(' · ');
}

export function NodeSettings({
  summary,
  nodeId,
  onUpdateNode,
  issues,
  toolsOnly = false,
}: {
  summary: GraphSummary;
  nodeId: string;
  onUpdateNode?: (nodeId: string, update: Partial<GraphNode>) => void;
  issues?: ElementIssue[];
  toolsOnly?: boolean;
}) {
  const node = summary.graph.spec.nodes.find((n) => n.id === nodeId)!;
  const defaults: NonNullable<GraphSummary['graph']['spec']['defaults']> = node.agent ? { loop: summary.graph.spec.defaults?.loop } : summary.graph.spec.defaults ?? {};
  const usesGraphDefaults = !node.agent && node.tools === undefined;
  const tools = usesGraphDefaults ? (defaults.tools ?? []) : (node.tools ?? []);
  const toolCatalog = useAsync(() => api.tools(), []);
  const [toolSearch, setToolSearch] = useState('');
  const rt = { ...defaults.runtime, ...node.runtime };
  const model = { ...defaults.model, ...node.model };
  const loop = node.loop ?? defaults.loop ?? {};
  const iso = { ...defaults.isolation, ...node.isolation };
  const inputs = (summary.graph.spec.channels ?? []).filter((c) => c.to?.includes(nodeId));
  const outputs = (summary.graph.spec.channels ?? []).filter((c) => c.from === nodeId);
  const knownTools = new Set((toolCatalog.data ?? []).map((tool) => tool.name));
  const extraGrants = tools.filter((grant) => !knownTools.has(grantName(grant)) && !grantName(grant).startsWith('mcp.'));
  const visibleTools = (toolCatalog.data ?? []).filter((tool) => {
    const query = toolSearch.trim().toLowerCase();
    return !query || `${tool.name} ${tool.capability} ${tool.description}`.toLowerCase().includes(query);
  });
  const updateToolGrant = (tool: ToolInfo, checked: boolean) => {
    if (!onUpdateNode) return;
    const current = tools.filter((grant) => grantName(grant) !== tool.name);
    onUpdateNode(nodeId, {
      tools: checked ? [...current, { name: tool.name }] : current,
    });
  };
  const setGrantApproval = (name: string, requiresApproval: boolean) => {
    if (!onUpdateNode) return;
    onUpdateNode(nodeId, {
      tools: tools.map((grant) => {
        if (grantName(grant) !== name) return grant;
        const object: ToolGrant = typeof grant === 'string' ? { name: grant } : { ...grant };
        if (requiresApproval) object.requiresApproval = true;
        else delete object.requiresApproval;
        return object;
      }),
    });
  };
  const updateExtraGrant = (grant: ToolGrant | string, checked: boolean) => {
    if (!onUpdateNode) return;
    const current = tools.filter((item) => grantName(item) !== grantName(grant));
    onUpdateNode(nodeId, { tools: checked ? [...current, grant] : current });
  };
  return (
    <section>
      {onUpdateNode && <MCPGrants grants={tools} onChange={grants=>onUpdateNode(nodeId,{tools:grants})} />}
      <div className="section-head">
        <div className="field-help-caption"><h3>Tools {usesGraphDefaults ? <span className="muted small">(graph defaults)</span> : null}</h3><FieldHelp label="Tools">Select the capabilities this agent needs. Grants allow access within workspace and installation limits. Require approval on a selected tool when someone should review each use.</FieldHelp></div>
        <FieldIssues issues={issues} field="tools" />
        {onUpdateNode && toolCatalog.data && (
          <span className="muted small">{tools.length} selected</span>
        )}
      </div>
      {onUpdateNode ? (
        <div className="tool-picker">
          <label className="tool-search">
            <span className="sr-only">Search available tools</span>
            <input
              type="search"
              value={toolSearch}
              onChange={(event) => setToolSearch(event.target.value)}
              placeholder="Search available tools"
            />
          </label>
          <ErrorBox error={toolCatalog.error} />
          {toolCatalog.loading && <p className="muted small">Loading available tools…</p>}
          {!toolCatalog.loading && toolCatalog.data?.length === 0 && (
            <p className="muted small">No tools are available in this installation.</p>
          )}
          <div className="tool-options" role="group" aria-label="Available tools" tabIndex={0}>
            {visibleTools.map((tool) => {
              const grant = tools.find((item) => grantName(item) === tool.name);
              return (
                <div key={tool.name}>
                  <label className="tool-option">
                    <input
                      type="checkbox"
                      checked={!!grant}
                      onChange={(event) => updateToolGrant(tool, event.target.checked)}
                    />
                    <span className="tool-option-copy">
                      <span className="tool-option-title">
                        <code>{tool.name}</code>
                        <span className="badge">{tool.capability}</span>
                      </span>
                      <span className="muted small">{tool.description}</span>
                    </span>
                  </label>
                  {grant && (
                    <label className="check tiny tool-approval">
                      <input
                        type="checkbox"
                        checked={typeof grant !== 'string' && !!grant.requiresApproval}
                        onChange={(event) => setGrantApproval(tool.name, event.target.checked)}
                      />
                      Require approval for {tool.name}
                    </label>
                  )}
                </div>
              );
            })}
          </div>
          {extraGrants.length > 0 && (
            <div className="tool-extra">
              <div className="field-help-caption"><h4>Patterns and configured grants</h4><FieldHelp label="Patterns and configured grants">These entries are saved grants without an exact match in the current tool catalog. A pattern can grant access to more than one tool. Keep it only if the agent needs that scope.</FieldHelp></div>
              <p className="muted small">These grants are not in the current tool catalog. Existing restrictions are preserved.</p>
              {extraGrants.map((grant) => (
                <label className="tool-option" key={grantName(grant)}>
                  <input
                    type="checkbox"
                    checked
                    onChange={(event) => updateExtraGrant(grant, event.target.checked)}
                  />
                  <span className="tool-option-copy">
                    <code>{grantName(grant)}</code>
                    {grantDetail(grant) && <span className="muted small">{grantDetail(grant)}</span>}
                  </span>
                </label>
              ))}
            </div>
          )}
        </div>
      ) : tools.length === 0 ? (
        <p className="muted small">No tools granted (default deny).</p>
      ) : (
        <ul className="grants">
          {tools.map((g, i) => (
            <li key={i}>
              <code>{grantName(g)}</code>
              {typeof g !== 'string' && g.requiresApproval && <Badge tone="warn">approval</Badge>}
              <span className="muted small">{grantDetail(g)}</span>
            </li>
          ))}
        </ul>
      )}
      {!toolsOnly && <><h3>Settings</h3>
      <dl className="kv">
        <dt>runtime</dt>
        <dd>
          {rt.kind ?? 'native'}
          {rt.framework ? ` · ${rt.framework}` : ''}
          {rt.agent ? ` · agent ${rt.agent}` : ''}
          {rt.endpoint ? <div className="mono small">{rt.endpoint}</div> : null}
          {rt.image ? <div className="mono small">{rt.image}</div> : null}
        </dd>
        <dt>model</dt>
        <dd>
          {model.provider || 'default provider'} / {model.name || 'default model'}
          {model.temperature !== undefined ? ` · t=${model.temperature}` : ''}
        </dd>
        <dt>loop</dt>
        <dd>
          {effectiveLoopMode(summary.graph, node)}
          {loop.schedule ? ` · ${loop.schedule}` : ''}
          {loop.interval ? ` · every ${loop.interval}` : ''}
          {loop.stop?.policy ? ` · stop: ${loop.stop.policy}` : ''}
          {loop.stop?.maxIterations ? ` (${loop.stop.maxIterations} turns)` : ''}
          {loop.stop?.maxDuration ? ` (${loop.stop.maxDuration})` : ''}
          {loop.batchSize ? ` · batch ${loop.batchSize}` : ''}
        </dd>
        <dt>cluster</dt>
        <dd>
          {node.cluster
            ? `${node.cluster.mode ?? 'fixed'} · ${
                node.cluster.mode === 'autoscale'
                  ? `${node.cluster.minReplicas ?? 1}–${node.cluster.maxReplicas ?? '?'}`
                  : (node.cluster.replicas ?? 1)
              } · ${node.cluster.comms ?? 'isolated'}`
            : 'single replica'}
        </dd>
        <dt>isolation</dt>
        <dd>
          {iso.mode ?? 'process'}
          {iso.image ? ` · ${iso.image}` : ''}
          {iso.cpu ? ` · ${iso.cpu} cpu` : ''}
          {iso.memory ? ` · ${iso.memory}` : ''}
          {iso.network ? ` · net ${iso.network}` : ''}
        </dd>
        <dt>workspace</dt>
        <dd>
          {node.workspace?.mode ?? 'shared'}
          {node.workspace?.subdir ? ` · ${node.workspace.subdir}` : ''}
        </dd>
        {(node.approval?.beforeTurn || node.approval?.beforeEmit) && (
          <>
            <dt>approval</dt>
            <dd>
              {[node.approval.beforeTurn && 'before turn', node.approval.beforeEmit && 'before emit']
                .filter(Boolean)
                .join(', ')}
              {node.approval.timeout ? ` · timeout ${node.approval.timeout} → ${node.approval.onTimeout ?? 'reject'}` : ''}
            </dd>
          </>
        )}
        <dt>inputs</dt>
        <dd>{inputs.map((c) => c.id).join(', ') || <span className="muted">none</span>}</dd>
        <dt>outputs</dt>
        <dd>{outputs.map((c) => c.id).join(', ') || <span className="muted">none – results land in the workspace</span>}</dd>
        {node.subscribe?.length ? (
          <>
            <dt>subscribes</dt>
            <dd>{node.subscribe.join(', ')}</dd>
          </>
        ) : null}
      </dl></>}
    </section>
  );
}

function Executions({ graphId, nodeId, replica }: { graphId: string; nodeId: string; replica: number }) {
  const ex = useAsync(() => api.executions(graphId, { node: nodeId, limit: 30 }), [graphId, nodeId]);
  const reload = useThrottled(() => void ex.reload(), 800);
  useEventStream({ graph: graphId, node: nodeId, prefixes: ['execution.'] }, reload);
  const rows = (ex.data ?? []).filter((e) => replica < 0 || e.replica === replica);
  return (
    <section>
      <div className="section-head">
        <h3>Recent turns</h3>
        <Btn small tone="ghost" onClick={() => void ex.reload()} aria-label="Refresh">
          <Icon name="refresh" />
        </Btn>
      </div>
      <ErrorBox error={ex.error} />
      {rows.length === 0 ? (
        <p className="muted small">No turns yet.</p>
      ) : (
        <table className="table small">
          <thead>
            <tr>
              <th>#</th>
              <th>rep</th>
              <th>status</th>
              <th>time</th>
              <th>tokens</th>
              <th>summary / error</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((e) => (
              <tr key={e.id} title={e.id}>
                <td>{e.iteration}</td>
                <td>n{e.replica}</td>
                <td>
                  <StateDot state={e.status} /> {stateLabel(e.status)}
                </td>
                <td>{since(e.startedAt, e.completedAt)}</td>
                <td>
                  {e.usage?.inputTokens ?? 0}/{e.usage?.outputTokens ?? 0}
                </td>
                <td className={e.error ? 'bad' : ''}>
                  {e.error ?? e.summary ?? ''}
                  {e.signal ? <Badge>{e.signal}</Badge> : null}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

/** Empty input clears the field so graph defaults apply again. */
function numberOrUndefined(value: string): number | undefined {
  if (value.trim() === '') return undefined;
  const n = Number(value);
  return Number.isFinite(n) ? n : undefined;
}

const STOP_FIELD = {
  iterations: { key: 'maxIterations', label: 'Max turns', kind: 'number' },
  duration: { key: 'maxDuration', label: 'Max duration (e.g. 2h)', kind: 'duration' },
  idle: { key: 'idleTimeout', label: 'Idle timeout (e.g. 10m)', kind: 'duration' },
} as const;

/** Edits an agent's document fields. Every change goes to the graph draft; nothing is saved until the toolbar Save. */
export function AgentEditor({
  summary,
  nodeId,
  onUpdateNode,
  issues,
  section,
  definitionOnly = false,
  sharedReadOnly = false,
}: {
  summary: GraphSummary;
  nodeId: string;
  onUpdateNode: (nodeId: string, update: Partial<GraphNode>) => void;
  issues?: ElementIssue[];
  section?: 'general' | 'tools' | 'advanced';
  definitionOnly?: boolean;
  sharedReadOnly?: boolean;
}) {
  const node = summary.graph.spec.nodes.find((n) => n.id === nodeId)!;
  const defaults: NonNullable<GraphSummary['graph']['spec']['defaults']> = node.agent ? { loop: summary.graph.spec.defaults?.loop } : summary.graph.spec.defaults ?? {};
  const update = (patch: Partial<GraphNode>) => onUpdateNode(nodeId, patch);
  const runtime = { ...defaults.runtime, ...node.runtime };
  const loop = node.loop ?? defaults.loop ?? {};
  const stop = loop.stop ?? {};
  const cluster = node.cluster ?? {};
  const approval = node.approval ?? {};
  const setLoop = (patch: NonNullable<GraphNode['loop']>) => update({ loop: { ...node.loop, ...patch } });
  const setStop = (patch: NonNullable<NonNullable<GraphNode['loop']>['stop']>) => setLoop({ stop: { ...node.loop?.stop, ...patch } });
  const setCluster = (patch: NonNullable<GraphNode['cluster']>) => update({ cluster: { ...node.cluster, ...patch } });
  const setLimits = (patch: NonNullable<GraphNode['limits']>) => update({ limits: { ...node.limits, ...patch } });
  const isolation = { ...defaults.isolation, ...node.isolation };
  const setIsolation = (patch: NonNullable<GraphNode['isolation']>) => update({ isolation: { ...node.isolation, ...patch } });
  const grants = node.tools === undefined ? (defaults.tools ?? []) : node.tools;
  const hasShell = grants.some((grant) => {
    const name = grantName(grant);
    return name === '*' || name.startsWith('shell');
  });
  const stopField = stop.policy && stop.policy in STOP_FIELD ? STOP_FIELD[stop.policy as keyof typeof STOP_FIELD] : undefined;

  return (
    <section className="form node-editor" aria-label="Agent configuration">
      {!definitionOnly && <h3>Agent configuration</h3>}
      <div hidden={!!section && section !== 'general'}>
      {!definitionOnly && <>
      <HelpField label={"ID"} help={"The workflow uses this ID to route messages and identify the agent in channels. It stays fixed after the node is created."}>
        <input value={node.id} readOnly aria-describedby={`${nodeId}-id-hint`} />
      </HelpField>
      <span id={`${nodeId}-id-hint`} className="muted tiny">
        Channels refer to agents by ID, so it is fixed once created.
      </span>
      <FieldIssues issues={issues} field="id" />
      </>}
      <HelpField label={"Name"} help={"The name is shown in the workflow and run history. Choose a label that describes the role, such as Release reviewer."}>
        <input value={node.name ?? ''} onChange={(event) => update({ name: event.target.value || undefined })} />
        <FieldIssues issues={issues} field="name" />
      </HelpField>
      <HelpField label={"Description"} help={"Describe what this agent is responsible for. This summary helps people choose the right agent from the catalog."}>
        <textarea
          rows={definitionOnly ? 4 : 2}
          value={node.description ?? ''}
          onChange={(event) => update({ description: event.target.value || undefined })}
        />
      </HelpField>

      </div>
      <fieldset hidden={!!section && section !== 'general'}>
        <legend className="sr-only">Runtime and model</legend>
        <HelpField label={"Runtime"} help={"Native runs the model and tool loop in Weft. External hands each turn to an adapter you run. Echo is useful for testing workflow wiring without calling a model."}>
          <select
            value={runtime.kind ?? 'native'}
            onChange={(event) =>
              update({ runtime: { ...node.runtime, kind: event.target.value as NonNullable<GraphNode['runtime']>['kind'] } })
            }
          >
            <option value="native">native</option>
            <option value="external">external</option>
            <option value="echo">echo</option>
          </select>
        </HelpField>
        {runtime.kind === 'external' && (
          <>
            <HelpField label={"Framework"} help={"An optional label for the external adapter, such as langgraph or custom. It identifies the adapter implementation; choose an adapter that speaks the Weft Runtime Protocol."}>
              <input
                value={node.runtime?.framework ?? ''}
                placeholder={defaults.runtime?.framework ?? 'langgraph'}
                onChange={(event) => update({ runtime: { ...node.runtime, framework: event.target.value || undefined } })}
              />
            </HelpField>
            <HelpField label={"Endpoint"} help={"The address of the external runtime adapter, reachable from the Weft server. For example, http://agents:8000 can refer to a service on the same Docker network. This is separate from the model provider URL."}>
              <input
                value={node.runtime?.endpoint ?? ''}
                placeholder={defaults.runtime?.endpoint ?? 'http://agents:8080'}
                onChange={(event) => update({ runtime: { ...node.runtime, endpoint: event.target.value || undefined } })}
              />
            </HelpField>
            <HelpField label={"Image"} help={"A container image that runs the external runtime adapter. Use this when Weft needs to launch the adapter instead of reaching an existing endpoint. The command sandbox image is configured under Tools & permissions."}>
              <input
                value={node.runtime?.image ?? ''}
                placeholder={defaults.runtime?.image}
                onChange={(event) => update({ runtime: { ...node.runtime, image: event.target.value || undefined } })}
              />
            </HelpField>
            <p className="muted tiny">An external agent needs an endpoint, command or image.</p>
          </>
        )}
        <FieldIssues issues={issues} field="runtime" />
        <ModelFields model={node.model} inherited={defaults.model} onChange={(next) => update({ model: next })} />
        <FieldIssues issues={issues} field="model" />
      </fieldset>

      <fieldset hidden={!!section && section !== 'advanced'}>
        <legend>Loop</legend>
        <HelpField label={"Loop mode"} help={"Event waits for incoming work. Once runs a single turn per start. Continuous repeats even without new input. Scheduled runs at the interval you set below."}>
          <select
            value={effectiveLoopMode(summary.graph, node)}
            onChange={(event) => setLoop({ mode: event.target.value as NonNullable<GraphNode['loop']>['mode'] })}
          >
            {(['once', 'continuous', 'scheduled', 'event'] as const).map((mode) => (
              <option key={mode} value={mode}>
                {mode}
              </option>
            ))}
          </select>
          <FieldIssues issues={issues} field="loop.mode" />
        </HelpField>
        {loop.mode === 'scheduled' && (
          <HelpField label={"Schedule"} help={"Choose when a scheduled agent runs. Use an interval such as @every 1h or a five-field cron expression. This field only applies in scheduled mode."}>
            <input
              value={loop.schedule ?? ''}
              placeholder="@every 1h"
              onChange={(event) => setLoop({ schedule: event.target.value || undefined })}
            />
            <FieldIssues issues={issues} field="loop.schedule" />
          </HelpField>
        )}
        <HelpField label={"Stop policy"} help={"Choose when an agent stops repeating turns, such as after a limit or a period without work. Auto asks the agent to confirm it is done on a subsequent turn and works best with a repeating loop."}>
          <select
            value={stop.policy ?? 'never'}
            onChange={(event) => setStop({ policy: event.target.value as NonNullable<typeof stop.policy> })}
          >
            {(['never', 'auto', 'iterations', 'duration', 'idle'] as const).map((policy) => (
              <option key={policy} value={policy}>
                {policy}
              </option>
            ))}
          </select>
          <FieldIssues issues={issues} field="loop.stop.policy" />
        </HelpField>
        {stopField && (
          <HelpField label={stopField.label} help={stopField.key === 'maxIterations'
            ? 'The maximum number of turns this agent runs before it stops.'
            : 'Enter a duration such as 10m or 1h. The selected stop policy determines whether it counts total run time or time without work.'}>
            <input
              type={stopField.kind === 'number' ? 'number' : 'text'}
              min={stopField.kind === 'number' ? 1 : undefined}
              value={String(stop[stopField.key] ?? '')}
              onChange={(event) =>
                setStop({
                  [stopField.key]:
                    stopField.kind === 'number' ? numberOrUndefined(event.target.value) : event.target.value || undefined,
                })
              }
            />
            <FieldIssues issues={issues} field={`loop.stop.${stopField.key}`} />
          </HelpField>
        )}
        {fieldIssuesExact(issues, 'loop')}
      </fieldset>

      <fieldset hidden={!!section && section !== 'advanced'}>
        <legend>Replicas</legend>
        <HelpField label={"Scaling"} help={"Fixed keeps the configured replica count. Autoscale tracks pending work and adjusts the count within the minimum and maximum you set."}>
          <select
            value={cluster.mode ?? 'fixed'}
            onChange={(event) => setCluster({ mode: event.target.value as NonNullable<GraphNode['cluster']>['mode'] })}
          >
            <option value="fixed">fixed</option>
            <option value="autoscale">autoscale</option>
          </select>
        </HelpField>
        {cluster.mode === 'autoscale' ? (
          <>
            <HelpField label={"Min replicas"} help={"The number of replicas autoscaling keeps available when work is low. A lower minimum uses fewer resources when the workflow is idle."}>
              <input
                type="number"
                min={1}
                value={cluster.minReplicas ?? ''}
                onChange={(event) => setCluster({ minReplicas: numberOrUndefined(event.target.value) })}
              />
            </HelpField>
            <HelpField label={"Max replicas"} help={"Caps autoscaling as pending work grows. Raise this only when the model provider and workspace can handle more simultaneous work."}>
              <input
                type="number"
                min={1}
                max={256}
                value={cluster.maxReplicas ?? ''}
                onChange={(event) => setCluster({ maxReplicas: numberOrUndefined(event.target.value) })}
              />
            </HelpField>
          </>
        ) : (
          <HelpField label={"Replicas"} help={"How many copies can handle work for this node at the same time. Queue consumers share messages; a broadcast can send a separate copy to each replica."}>
            <input
              type="number"
              min={1}
              max={256}
              value={cluster.replicas ?? ''}
              placeholder="1"
              onChange={(event) => setCluster({ replicas: numberOrUndefined(event.target.value) })}
            />
          </HelpField>
        )}
        <FieldIssues issues={issues} field="cluster" />
        <HelpField label={"Workspace"} help={"Shared replicas see the same files. Worktree gives each replica a separate Git working tree so concurrent edits can be kept apart."}>
          <select
            value={node.workspace?.mode ?? 'shared'}
            onChange={(event) => update({ workspace: { ...node.workspace, mode: event.target.value as 'shared' | 'worktree' } })}
          >
            <option value="shared">shared</option>
            <option value="worktree">worktree</option>
          </select>
          <FieldIssues issues={issues} field="workspace" />
        </HelpField>
      </fieldset>

      <fieldset hidden={!!section && section !== 'tools'} disabled={sharedReadOnly}>
        <legend><span className="field-help-caption">Command sandbox <FieldHelp label="Command sandbox">Controls where agent commands run and which container image, network and resources are available. Choose these settings based on the commands the workflow needs.</FieldHelp></span></legend>
        <HelpField label={"Isolation"} help={"Process runs commands inside Weft. Docker uses the dedicated command sandbox. Kubernetes currently runs commands in the Weft pod. Tool grants and installation policy still control which operations are allowed."}>
          <select
            value={isolation.mode ?? 'process'}
            onChange={(event) => setIsolation({ mode: event.target.value as NonNullable<GraphNode['isolation']>['mode'] })}
          >
            <option value="process">process — inside the Weft server</option>
            <option value="docker">docker — dedicated sandbox container</option>
            <option value="kubernetes">kubernetes</option>
          </select>
          <FieldIssues issues={issues} field="isolation.mode" />
        </HelpField>
        {isolation.mode === 'docker' ? (
          <>
            <HelpField label={"Network"} help={"None blocks sandbox network access. Bridge allows public network access subject to installation rules and can reach other containers on that bridge. Use only the network access needed by the task."}>
              <select
                value={isolation.network ?? 'none'}
                onChange={(event) => setIsolation({ network: event.target.value === 'none' ? undefined : event.target.value })}
              >
                <option value="none">none — no network access</option>
                <option value="bridge">bridge — internet only, no private networks</option>
              </select>
            </HelpField>
            <HelpField label={"Image"} help={"The Docker image containing the command-line tools the agent needs. Leave it blank to use the workflow environment or installation default. A custom image needs to be available on the sandbox daemon."}>
              <input
                value={node.isolation?.image ?? ''}
                placeholder={defaults.isolation?.image ?? 'installation default (weft/sandbox:latest)'}
                onChange={(event) => setIsolation({ image: event.target.value || undefined })}
              />
            </HelpField>
            <HelpField label={"CPU limit"} help={"Limits how much CPU a Docker sandbox can use, in CPU cores. For example, 2 allows up to two cores for that sandbox."}>
              <input
                value={node.isolation?.cpu ?? ''}
                placeholder={defaults.isolation?.cpu ?? 'e.g. 2'}
                onChange={(event) => setIsolation({ cpu: event.target.value || undefined })}
              />
            </HelpField>
            <HelpField label={"Memory limit"} help={"Caps Docker sandbox memory. Use a size such as 2g and allow enough for builds. Commands can be killed if they exceed the limit."}>
              <input
                value={node.isolation?.memory ?? ''}
                placeholder={defaults.isolation?.memory ?? 'e.g. 2g'}
                onChange={(event) => setIsolation({ memory: event.target.value || undefined })}
              />
            </HelpField>
            <p className="muted tiny">
              Commands run in a container on the Weft sandbox daemon, never on the host. The image must already exist
              there; see Administration → System health.
            </p>
          </>
        ) : (
          hasShell && (
            <p className="field-warning" role="note">
              This agent can run shell commands. With process isolation they run inside the Weft server container and
              can read its files; choose docker for anything but trusted agents.
            </p>
          )
        )}
        <FieldIssues issues={issues} field="isolation" />
      </fieldset>

      <fieldset hidden={!!section && section !== 'advanced'}>
        <legend>Limits per turn</legend>
        <p className="muted tiny">
          A turn that exceeds a limit fails and its input is redelivered. Long tasks (dependency upgrades, large
          refactors) need more time and tool calls than the defaults.
        </p>
        <HelpField label={"Turn timeout"} help={"The maximum time for one execution turn. Increase it for long builds or checks. An expired turn fails and its input may be delivered again."}>
          <input
            value={node.limits?.turnTimeout ?? ''}
            placeholder="30m"
            onChange={(event) => setLimits({ turnTimeout: event.target.value || undefined })}
          />
          <FieldIssues issues={issues} field="limits.turnTimeout" />
        </HelpField>
        <HelpField label={"Max tool calls"} help={"Caps the number of tool calls during one turn. Each call uses part of the allowance, so long multi-step tasks may need a larger budget."}>
          <input
            type="number"
            min={1}
            value={node.limits?.maxToolCalls ?? ''}
            placeholder="50"
            onChange={(event) => setLimits({ maxToolCalls: numberOrUndefined(event.target.value) })}
          />
          <FieldIssues issues={issues} field="limits.maxToolCalls" />
        </HelpField>
        <HelpField label={"Token budget"} help={"Caps input plus output tokens for one turn. Use a larger allowance when the agent must read substantial context or generate long results."}>
          <input
            type="number"
            min={1}
            value={node.limits?.maxTokens ?? ''}
            placeholder="unlimited"
            aria-describedby={`${nodeId}-tokens-hint`}
            onChange={(event) => setLimits({ maxTokens: numberOrUndefined(event.target.value) })}
          />
        </HelpField>
        <span id={`${nodeId}-tokens-hint`} className="muted tiny">
          Input plus output tokens the agent may use in one turn.
        </span>
        <FieldIssues issues={issues} field="limits.maxTokens" />
      </fieldset>

      <fieldset hidden={definitionOnly || (!!section && section !== 'tools')}>
        <legend>Permissions</legend>
        <p className="muted tiny">Tool grants are chosen below; these gates hold the agent for a human decision.</p>
        <HelpField className="check" label="Approve each turn before it starts" help="A person reviews and approves each turn before the agent starts processing. Use it when every execution needs review.">
          <input
            type="checkbox"
            checked={!!approval.beforeTurn}
            onChange={(event) => update({ approval: { ...node.approval, beforeTurn: event.target.checked || undefined } })}
          />
        </HelpField>
        <HelpField className="check" label="Approve output before it is sent" help="Holds each outgoing message for review before downstream channels receive it. Use it when agent results need a human check.">
          <input
            type="checkbox"
            checked={!!approval.beforeEmit}
            onChange={(event) => update({ approval: { ...node.approval, beforeEmit: event.target.checked || undefined } })}
          />
        </HelpField>
        <FieldIssues issues={issues} field="approval" />
        <HelpField className="check" label="Disable agent" help="Keeps this node and its configuration in the workflow while preventing its replicas from running. Check downstream nodes if they depend on its output.">
          <input type="checkbox" checked={!!node.disabled} onChange={(event) => update({ disabled: event.target.checked || undefined })} />
        </HelpField>
      </fieldset>
    </section>
  );
}

/** Issues on exactly `field` (not its children, which are shown under their own inputs). */
function fieldIssuesExact(issues: ElementIssue[] | undefined, field: string) {
  const own = (issues ?? []).filter((issue) => issue.field === field);
  return own.length ? <FieldIssues issues={own} field={field} /> : null;
}
