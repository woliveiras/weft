import type { Channel, Graph, GraphNode, ValidationIssue } from '../../api/types';

/**
 * Pure edits on a graph draft. The canvas and panels only describe what the
 * user did; these functions decide what that means for the document, so the
 * same rules apply however the edit was made. Every function returns a new
 * graph and keeps fields it does not touch (including `x-` extensions).
 */

export type Point = { x: number; y: number };

/** One rendered edge of a channel: a channel with several targets draws several edges. */
export interface EdgeRef {
  channelId: string;
  target: string;
}

function uniqueId(prefix: string, taken: Set<string>, start: number): string {
  let index = start;
  while (taken.has(`${prefix}-${index}`)) index += 1;
  return `${prefix}-${index}`;
}

function withPositions(nodes: GraphNode[], positions: Record<string, Point>): GraphNode[] {
  return nodes.map((node) => (positions[node.id] ? { ...node, position: positions[node.id] } : node));
}

/** Adds an agent with valid defaults. Current on-screen positions are kept so auto-laid-out agents do not jump. */
export function addAgent(graph: Graph, position: Point, positions: Record<string, Point> = {}): { graph: Graph; id: string } {
  const ids = new Set(graph.spec.nodes.map((node) => node.id));
  const id = uniqueId('agent', ids, graph.spec.nodes.length + 1);
  const index = id.slice('agent-'.length);
  const node: GraphNode = {
    id,
    name: `Agent ${index}`,
    runtime: { kind: 'native' },
    prompt: "Describe this agent's task.",
    loop: { mode: 'event' },
    position,
  };
  return {
    id,
    graph: { ...graph, spec: { ...graph.spec, nodes: [...withPositions(graph.spec.nodes, positions), node] } },
  };
}

export function moveAgents(graph: Graph, positions: Record<string, Point>): Graph {
  return { ...graph, spec: { ...graph.spec, nodes: withPositions(graph.spec.nodes, positions) } };
}

/** Adds a queue channel between two agents. Returns the graph unchanged for non-agent endpoints. */
export function connect(graph: Graph, source: string, target: string): { graph: Graph; id?: string } {
  const ids = new Set(graph.spec.nodes.map((node) => node.id));
  if (!ids.has(source) || !ids.has(target)) return { graph };
  const taken = new Set((graph.spec.channels ?? []).map((channel) => channel.id));
  const id = uniqueId('channel', taken, (graph.spec.channels?.length ?? 0) + 1);
  const channel: Channel = { id, type: 'queue', from: source, to: [target] };
  return { id, graph: { ...graph, spec: { ...graph.spec, channels: [...(graph.spec.channels ?? []), channel] } } };
}

export function updateNode(graph: Graph, nodeId: string, update: Partial<GraphNode>): Graph {
  return {
    ...graph,
    spec: { ...graph.spec, nodes: graph.spec.nodes.map((node) => (node.id === nodeId ? { ...node, ...update } : node)) },
  };
}

export function updateChannel(graph: Graph, channelId: string, update: Partial<Channel>): Graph {
  return {
    ...graph,
    spec: {
      ...graph.spec,
      channels: (graph.spec.channels ?? []).map((channel) => (channel.id === channelId ? { ...channel, ...update } : channel)),
    },
  };
}

export interface Removal {
  nodes: string[];
  /** Channels removed entirely. */
  channels: string[];
  /** Channels that lose some targets but stay. */
  narrowed: { channelId: string; targets: string[] }[];
}

/**
 * Works out what removing agents and edges does to the document, in one
 * pass so a node and its edges are never interpreted twice:
 * - a removed agent takes the channels it produces with it, and leaves the
 *   channels it consumed from;
 * - a listed channel is removed entirely;
 * - removing an edge removes that target from the channel; a channel with
 *   no explicit target left is removed (topic subscribers are implicit, so
 *   an implicit edge removes the channel too).
 */
export function planRemoval(
  graph: Graph,
  nodeIds: Iterable<string>,
  edges: EdgeRef[] = [],
  channelIds: Iterable<string> = [],
): Removal {
  const nodes = new Set(nodeIds);
  const wholeChannels = new Set(channelIds);
  const edgeTargets = new Map<string, Set<string>>();
  for (const edge of edges) {
    const set = edgeTargets.get(edge.channelId) ?? new Set<string>();
    set.add(edge.target);
    edgeTargets.set(edge.channelId, set);
  }
  const out: Removal = { nodes: graph.spec.nodes.filter((n) => nodes.has(n.id)).map((n) => n.id), channels: [], narrowed: [] };
  for (const channel of graph.spec.channels ?? []) {
    const explicit = channel.to ?? [];
    const cut = edgeTargets.get(channel.id);
    if (wholeChannels.has(channel.id) || (channel.from && nodes.has(channel.from))) {
      out.channels.push(channel.id);
      continue;
    }
    if (cut && [...cut].some((target) => !explicit.includes(target))) {
      // An implicit (subscription) edge: there is no target to drop.
      out.channels.push(channel.id);
      continue;
    }
    const dropped = explicit.filter((target) => nodes.has(target) || cut?.has(target));
    if (dropped.length === 0) continue;
    if (dropped.length === explicit.length) out.channels.push(channel.id);
    else out.narrowed.push({ channelId: channel.id, targets: dropped });
  }
  return out;
}

export function applyRemoval(graph: Graph, removal: Removal): Graph {
  const nodes = new Set(removal.nodes);
  const channels = new Set(removal.channels);
  const narrowed = new Map(removal.narrowed.map((n) => [n.channelId, new Set(n.targets)]));
  return {
    ...graph,
    spec: {
      ...graph.spec,
      nodes: graph.spec.nodes.filter((node) => !nodes.has(node.id)),
      channels: (graph.spec.channels ?? []).flatMap((channel) => {
        if (channels.has(channel.id)) return [];
        const drop = narrowed.get(channel.id);
        return drop ? [{ ...channel, to: (channel.to ?? []).filter((target) => !drop.has(target)) }] : [channel];
      }),
    },
  };
}

/** A confirmation sentence, or undefined when nothing would be removed. */
export function describeRemoval(graph: Graph, removal: Removal): string | undefined {
  const name = (id: string) => {
    const node = graph.spec.nodes.find((n) => n.id === id);
    return `"${node?.name || id}"`;
  };
  const parts: string[] = [];
  if (removal.nodes.length) parts.push(`${removal.nodes.length === 1 ? 'agent' : 'agents'} ${removal.nodes.map(name).join(', ')}`);
  if (removal.channels.length)
    parts.push(`${removal.channels.length === 1 ? 'channel' : 'channels'} ${removal.channels.map((c) => `"${c}"`).join(', ')}`);
  const narrowed = removal.narrowed.map((n) => `"${n.channelId}" will stop delivering to ${n.targets.map(name).join(', ')}`);
  if (parts.length === 0 && narrowed.length === 0) return undefined;
  return [parts.length ? `Remove ${parts.join(' and ')}?` : 'Change channel targets?', ...narrowed].join('\n');
}

export interface ElementIssue extends ValidationIssue {
  severity: 'error' | 'warning';
  /** Path below the element, e.g. "prompt" or "loop.schedule"; "" for the element itself. */
  field: string;
}

export interface IssueIndex {
  nodes: Map<string, ElementIssue[]>;
  channels: Map<string, ElementIssue[]>;
  /** Issues that belong to the graph rather than one element. */
  graph: ElementIssue[];
  errorCount: number;
}

const ELEMENT_PATH = /^spec\.(nodes|channels)\[(\d+)\](?:\.(.*))?$/;

/**
 * Maps validation paths ("spec.nodes[2].prompt") to the agents and channels
 * of the draft they were computed for, so problems show next to the element
 * and field that caused them.
 */
export function indexIssues(graph: Graph, errors: ValidationIssue[] = [], warnings: ValidationIssue[] = []): IssueIndex {
  const index: IssueIndex = { nodes: new Map(), channels: new Map(), graph: [], errorCount: errors.length };
  const add = (issue: ValidationIssue, severity: ElementIssue['severity']) => {
    const m = ELEMENT_PATH.exec(issue.path);
    const element =
      m?.[1] === 'nodes' ? graph.spec.nodes[Number(m[2])]?.id : m ? graph.spec.channels?.[Number(m[2])]?.id : undefined;
    if (!m || !element) {
      index.graph.push({ ...issue, severity, field: issue.path });
      return;
    }
    const map = m[1] === 'nodes' ? index.nodes : index.channels;
    const list = map.get(element) ?? [];
    list.push({ ...issue, severity, field: m[3] ?? '' });
    map.set(element, list);
  };
  errors.forEach((issue) => add(issue, 'error'));
  warnings.forEach((issue) => add(issue, 'warning'));
  return index;
}

/** Issues for a field and anything below it ("loop" matches "loop.schedule"). */
export function fieldIssues(issues: ElementIssue[] | undefined, field: string): ElementIssue[] {
  return (issues ?? []).filter((issue) => issue.field === field || issue.field.startsWith(`${field}.`) || issue.field.startsWith(`${field}[`));
}
