import dagre from '@dagrejs/dagre';
import type { Edge, Node } from '@xyflow/react';
import type { Channel, ChannelStatus, ChannelType, Graph, GraphNode, GraphStatus, LoopMode, NodeStatus } from '../../api/types';
import type { ElementIssue, IssueIndex } from './edit';

export const NODE_W = 240;
export const NODE_H = 118;

export interface AgentNodeData extends Record<string, unknown> {
  node: GraphNode;
  /** The loop mode the node runs with, defaults applied. */
  loopMode?: LoopMode;
  status?: NodeStatus;
  issues?: ElementIssue[];
}

export interface PortNodeData extends Record<string, unknown> {
  label: string;
  kind: 'ingress' | 'nosubs';
}

export interface ChannelEdgeData extends Record<string, unknown> {
  channel: Channel;
  type: ChannelType;
  target: string;
  pending: number;
  inFlight: number;
  dead: number;
  /** Queue channels share one backlog; only the first edge carries the count. */
  shared: boolean;
  showCount: boolean;
  /** Index among edges joining the same ordered pair, to fan out overlapping curves. */
  pairIndex: number;
  selected?: boolean;
  onSelect?: (channelId: string) => void;
  issues?: ElementIssue[];
}

/** Same glob rules as domain.GlobMatch: '*' any run, '?' one char. */
export function globMatch(pattern: string, name: string): boolean {
  if (pattern === '*' || pattern === name) return true;
  const re = new RegExp(
    '^' +
      pattern
        .split('')
        .map((c) => (c === '*' ? '.*' : c === '?' ? '.' : c.replace(/[.+^${}()|[\]\\]/g, '\\$&')))
        .join('') +
      '$',
  );
  return re.test(name);
}

/** Consumers of a channel: explicit `to` plus topic subscribers (graph.go Consumers). */
export function consumers(g: Graph, c: Channel): string[] {
  const out = [...(c.to ?? [])];
  if (c.type === 'topic') {
    const topic = c.topic || c.id;
    for (const n of g.spec.nodes) {
      if (out.includes(n.id)) continue;
      if ((n.subscribe ?? []).some((p) => globMatch(p, topic))) out.push(n.id);
    }
  }
  return out;
}

/**
 * The loop mode a node runs with: its own, the graph default, or (like
 * graph.Normalize on the server) event when something feeds it, else once.
 * Graph documents are stored as written, so the node often leaves it unset.
 */
export function effectiveLoopMode(g: Graph, n: GraphNode): LoopMode {
  if (n.loop?.mode) return n.loop.mode;
  if (g.spec.defaults?.loop?.mode) return g.spec.defaults.loop.mode;
  return (g.spec.channels ?? []).some((c) => consumers(g, c).includes(n.id)) ? 'event' : 'once';
}

export function nodeCount(n: GraphNode): { desired: number; label: string } {
  const c = n.cluster;
  if (!c || (!c.replicas && !c.maxReplicas && c.mode !== 'autoscale')) return { desired: 1, label: '' };
  if (c.mode === 'autoscale') {
    return { desired: c.maxReplicas ?? c.replicas ?? 1, label: `${c.minReplicas ?? 1}–${c.maxReplicas ?? c.replicas ?? '?'}` };
  }
  return { desired: c.replicas ?? 1, label: String(c.replicas ?? 1) };
}

function groupCounts(cs: ChannelStatus | undefined, c: Channel, target: string) {
  const groups = cs?.groups ?? [];
  const type = c.type ?? 'queue';
  const pick =
    type === 'queue'
      ? groups.filter((g) => g.group === c.id || g.node === undefined)
      : groups.filter((g) => g.node === target);
  return pick.reduce(
    (a, g) => ({ pending: a.pending + g.pending, inFlight: a.inFlight + g.inFlight, dead: a.dead + g.dead }),
    { pending: 0, inFlight: 0, dead: 0 },
  );
}

export function buildElements(g: Graph, status?: GraphStatus, issues?: IssueIndex): { nodes: Node[]; edges: Edge[] } {
  const nodes: Node[] = g.spec.nodes.map((n) => ({
    id: n.id,
    type: 'agent',
    position: n.position ?? { x: 0, y: 0 },
    data: {
      node: n,
      loopMode: effectiveLoopMode(g, n),
      status: status?.nodes?.find((s) => s.nodeId === n.id),
      issues: issues?.nodes.get(n.id),
    } satisfies AgentNodeData,
  }));
  const edges: Edge[] = [];
  const pairCount = new Map<string, number>();
  for (const c of g.spec.channels ?? []) {
    const type = c.type ?? 'queue';
    const cs = status?.channels?.find((s) => s.channelId === c.id);
    let source = c.from;
    if (!source) {
      source = `__in:${c.id}`;
      nodes.push({
        id: source,
        type: 'port',
        position: { x: 0, y: 0 },
        data: { label: c.label || c.id, kind: 'ingress' } satisfies PortNodeData,
        selectable: false,
        connectable: false,
      });
    }
    let targets = consumers(g, c);
    if (targets.length === 0) {
      const id = `__nosubs:${c.id}`;
      nodes.push({
        id,
        type: 'port',
        position: { x: 0, y: 0 },
        data: { label: `${c.topic || c.id} (no subscribers)`, kind: 'nosubs' } satisfies PortNodeData,
        selectable: false,
        connectable: false,
      });
      targets = [id];
    }
    targets.forEach((t, i) => {
      const key = `${source}->${t}`;
      const pairIndex = pairCount.get(key) ?? 0;
      pairCount.set(key, pairIndex + 1);
      const counts = groupCounts(cs, c, t);
      edges.push({
        id: `${c.id}::${t}`,
        source: source!,
        target: t,
        type: 'channel',
        data: {
          channel: c,
          type,
          target: t,
          ...counts,
          shared: type === 'queue' && targets.length > 1,
          showCount: type !== 'queue' || i === 0,
          pairIndex,
          issues: issues?.channels.get(c.id),
        } satisfies ChannelEdgeData,
      });
    });
  }
  return { nodes, edges };
}

/** Auto-layout with dagre for nodes that have no stored position. */
export function layout(nodes: Node[], edges: Edge[]): Node[] {
  const positioned = nodes.filter((n) => n.type === 'agent' && (n.data as AgentNodeData).node.position);
  const allPositioned = positioned.length === nodes.filter((n) => n.type === 'agent').length && positioned.length > 0;
  const g = new dagre.graphlib.Graph();
  g.setGraph({ rankdir: 'LR', nodesep: 60, ranksep: 120, marginx: 20, marginy: 20 });
  g.setDefaultEdgeLabel(() => ({}));
  for (const n of nodes) {
    const w = n.type === 'agent' ? NODE_W : 150;
    const h = n.type === 'agent' ? NODE_H : 36;
    g.setNode(n.id, { width: w, height: h });
  }
  for (const e of edges) if (e.source !== e.target) g.setEdge(e.source, e.target);
  dagre.layout(g);
  return nodes.map((n) => {
    const own = n.type === 'agent' ? (n.data as AgentNodeData).node.position : undefined;
    if (own && allPositioned) return { ...n, position: own };
    if (n.type === 'port' && allPositioned) {
      // Ports sit to the left (ingress) or right of their neighbour.
      const e = edges.find((x) => x.source === n.id || x.target === n.id);
      const other = e ? nodes.find((x) => x.id === (e.source === n.id ? e.target : e.source)) : undefined;
      const op = other && other.type === 'agent' ? (other.data as AgentNodeData).node.position : undefined;
      if (op) {
        const ingress = (n.data as PortNodeData).kind === 'ingress';
        return { ...n, position: { x: op.x + (ingress ? -220 : NODE_W + 80), y: op.y + NODE_H / 2 - 18 } };
      }
    }
    const p = g.node(n.id);
    const w = n.type === 'agent' ? NODE_W : 150;
    const h = n.type === 'agent' ? NODE_H : 36;
    return { ...n, position: { x: p.x - w / 2, y: p.y - h / 2 } };
  });
}
