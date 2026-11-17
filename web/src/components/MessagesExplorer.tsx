import { useEffect, useMemo, useState } from 'react';
import { api } from '../api/client';
import { traceIdFromParent, type Graph, type Info, type MessageRecord } from '../api/types';
import { useAsync } from '../hooks/useData';
import { Badge, Btn, ErrorBox, JsonView, StateDot, fmtTime, stateLabel } from './common';
import { MessageList } from './ChannelPanel';
import { tempoLink } from './traces';

interface TreeNode {
  id: string;
  records: MessageRecord[];
  children: TreeNode[];
}

/** Build the causal tree (causation_id → id) of one workflow. */
function buildTree(records: MessageRecord[]): TreeNode[] {
  const byId = new Map<string, TreeNode>();
  for (const r of records) {
    const n = byId.get(r.message.id) ?? { id: r.message.id, records: [], children: [] };
    n.records.push(r);
    byId.set(r.message.id, n);
  }
  const roots: TreeNode[] = [];
  for (const n of byId.values()) {
    const cause = n.records[0].message.metadata?.causation_id as string | undefined;
    const parent = cause ? byId.get(cause) : undefined;
    if (parent && parent !== n) parent.children.push(n);
    else roots.push(n);
  }
  const sortRec = (ns: TreeNode[]) => {
    ns.sort((a, b) => a.records[0].createdAt.localeCompare(b.records[0].createdAt));
    ns.forEach((n) => sortRec(n.children));
  };
  sortRec(roots);
  return roots;
}

export function MessagesExplorer({
  graph,
  info,
  correlationId,
  onCorrelation,
}: {
  graph: Graph;
  info?: Info;
  correlationId?: string;
  onCorrelation: (c: string | undefined) => void;
}) {
  const gid = graph.metadata.id;
  const [input, setInput] = useState(correlationId ?? '');
  const [status, setStatus] = useState('');
  const [node, setNode] = useState('');
  useEffect(() => setInput(correlationId ?? ''), [correlationId]);

  const recent = useAsync(
    () => api.messages(gid, { status: status || undefined, node: node || undefined, limit: 100 }),
    [gid, status, node],
  );
  const chain = useAsync(
    () => (correlationId ? api.messages(gid, { correlationId, limit: 1000 }) : Promise.resolve([])),
    [gid, correlationId],
  );
  const tree = useMemo(() => buildTree(chain.data ?? []), [chain.data]);
  const traceId = useMemo(() => {
    const r = (chain.data ?? []).find((x) => x.message.metadata?.traceparent);
    return r ? traceIdFromParent(r.message.metadata?.traceparent) : undefined;
  }, [chain.data]);

  return (
    <div className="page fill split">
      <div className="split-main">
        <div className="page-head">
          <h2>Workflow trace</h2>
          <form
            className="inline-form"
            onSubmit={(e) => {
              e.preventDefault();
              onCorrelation(input.trim() || undefined);
            }}
          >
            <input
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder="correlation id"
              aria-label="Correlation id"
              className="mono"
              size={34}
            />
            <Btn small tone="primary" type="submit">
              Trace
            </Btn>
          </form>
          {traceId && info?.grafanaUrl && (
            <a href={tempoLink(info.grafanaUrl, traceId)} target="_blank" rel="noreferrer">
              Open trace {traceId.slice(0, 8)}… in Grafana
            </a>
          )}
        </div>
        <ErrorBox error={chain.error} />
        {!correlationId && <p className="muted">Pick a message on the right (or paste a correlation id) to see its causal chain.</p>}
        {correlationId && tree.length === 0 && !chain.loading && <p className="muted">No messages with that correlation id.</p>}
        <ul className="tree">
          {tree.map((n) => (
            <TreeItem key={n.id} n={n} />
          ))}
        </ul>
      </div>
      <aside className="split-side">
        <div className="page-head">
          <h3>Recent messages</h3>
          <select value={status} onChange={(e) => setStatus(e.target.value)} aria-label="Status filter">
            <option value="">any status</option>
            {['published', 'held', 'delivered', 'acked', 'nacked', 'dead_lettered', 'dropped', 'rejected'].map((s) => (
              <option key={s} value={s}>
                {stateLabel(s)}
              </option>
            ))}
          </select>
          <select value={node} onChange={(e) => setNode(e.target.value)} aria-label="Node filter">
            <option value="">any node</option>
            {graph.spec.nodes.map((n) => (
              <option key={n.id} value={n.id}>
                {n.name || n.id}
              </option>
            ))}
          </select>
        </div>
        <ErrorBox error={recent.error} />
        <MessageList records={recent.data ?? []} onShowCorrelation={(c) => onCorrelation(c)} />
      </aside>
    </div>
  );
}

function TreeItem({ n }: { n: TreeNode }) {
  const m = n.records[0].message;
  return (
    <li>
      <div className="tree-row">
        <code>{m.type}</code>
        <span className="muted small">
          {m.source || 'external'} → {m.target || n.records.map((r) => r.channel).join(', ')}
        </span>
        <span className="muted small">{fmtTime(n.records[0].createdAt)}</span>
        {n.records.map((r) => (
          <Badge key={`${r.channel}:${r.group ?? ''}`} title={`${r.channel}${r.group ? ` / ${r.group}` : ''}`}>
            <StateDot state={r.status} /> {r.group ?? r.channel}: {stateLabel(r.status)}
          </Badge>
        ))}
        {typeof m.metadata?.injected_by === 'string' && <Badge tone="info">injected by {m.metadata.injected_by}</Badge>}
        {typeof m.metadata?.execution_id === 'string' && (
          <span className="muted small mono" title="emitted by execution">
            {m.metadata.execution_id.slice(0, 14)}…
          </span>
        )}
      </div>
      <JsonView value={m.payload} label="payload" />
      {n.children.length > 0 && (
        <ul>
          {n.children.map((c) => (
            <TreeItem key={c.id} n={c} />
          ))}
        </ul>
      )}
    </li>
  );
}
