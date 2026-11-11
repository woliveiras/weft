// Fixture-backed fake of the Weft API, used only with VITE_MOCK=1 so the
// UI can be eyeballed without a server. It is deliberately loose: enough
// behaviour to exercise every screen, not a reimplementation of the engine.
import type {
  Approval,
  Execution,
  Graph,
  GraphNode,
  GraphStatus,
  GraphSummary,
  GroupStats,
  WeftEvent,
  MessageRecord,
  OperatorMessage,
  ProviderInfo,
  ProviderInput,
  ProviderPlugin,
  ProviderTestResult,
  ReplicaState,
  ReplicaStatus,
} from '../api/types';
import { mockBus } from './events';
import { parse as parseYaml } from 'yaml';

const now = () => new Date().toISOString();
let seq = 0;
let idn = 0;
const nid = (p: string) => `${p}_${Date.now().toString(36)}${(idn++).toString(36)}`;

const softwareFactory: Graph = {
  apiVersion: 'weft.io/v1',
  kind: 'Graph',
  metadata: {
    id: 'software-factory',
    name: 'Software Factory',
    description: 'Planner → research → developer cluster → reviewer ⇄ developer → QA → gated release.',
    version: 3,
  },
  spec: {
    parameters: [{ name: 'repo', value: 'workspace/app', description: 'Project directory' }],
    nodes: [
      {
        id: 'planner',
        name: 'Planner',
        description: 'Turns feature requests into tasks; asks research questions first.',
        runtime: { kind: 'external', framework: 'custom', agent: 'planner', endpoint: 'http://agents:8000' },
        model: { provider: 'local-ollama', name: 'qwen2.5-coder:7b' },
        prompt: 'Break @input feature requests into small, testable tasks.',
        tools: ['filesystem.read', { name: 'filesystem.write', paths: ['plans/**'] }],
        loop: { mode: 'event', stop: { policy: 'never' } },
      },
      {
        id: 'researcher',
        name: 'Researcher',
        runtime: { kind: 'external', framework: 'langgraph', agent: 'researcher' },
        model: { provider: 'local-ollama', name: 'llama3.1:8b' },
        prompt: 'Answer research questions and write notes into research/.',
        tools: ['filesystem.read', 'filesystem.write', { name: 'http.fetch', hosts: ['*.wikipedia.org'] }],
        loop: { mode: 'event' },
        subscribe: ['research.*'],
      },
      {
        id: 'developer',
        name: 'Developer',
        runtime: { kind: 'native' },
        model: { provider: 'openai-compatible', name: 'gpt-4.1' },
        prompt: 'Implement the task from @input in @project with tests. Commit on your branch.',
        tools: [
          'filesystem.*',
          { name: 'shell.exec', commands: ['go *', 'npm *'] },
          'git.read',
          'git.write',
        ],
        loop: { mode: 'event' },
        cluster: { mode: 'autoscale', minReplicas: 1, maxReplicas: 4, comms: 'isolated' },
        isolation: { mode: 'docker', image: 'weft/sandbox:latest', cpu: '1', memory: '1Gi', network: 'none' },
        workspace: { mode: 'worktree' },
      },
      {
        id: 'reviewer',
        name: 'Reviewer',
        runtime: { kind: 'external', framework: 'autogen', agent: 'reviewer' },
        prompt: 'Review the diff. Approve or request changes.',
        tools: ['git.read', 'filesystem.read'],
        loop: { mode: 'event' },
      },
      {
        id: 'qa',
        name: 'QA',
        runtime: { kind: 'external', framework: 'crewai', agent: 'qa' },
        prompt: 'Run the test suite and report.',
        tools: ['filesystem.read', { name: 'shell.exec', commands: ['go test *'], requiresApproval: true }],
        loop: { mode: 'event' },
        approval: { beforeEmit: false },
      },
      {
        id: 'release',
        name: 'Version control',
        runtime: { kind: 'echo' },
        prompt: 'Merge approved branches.',
        tools: ['git.*'],
        loop: { mode: 'event' },
        disabled: false,
      },
    ],
    channels: [
      { id: 'requests', type: 'queue', to: ['planner'], label: 'requests' },
      { id: 'research', type: 'topic', from: 'planner', topic: 'research.questions', types: ['research.*'] },
      { id: 'findings', type: 'queue', from: 'researcher', to: ['planner'] },
      { id: 'tasks', type: 'queue', from: 'planner', to: ['developer'], types: ['task.*'] },
      { id: 'review', type: 'queue', from: 'developer', to: ['reviewer'] },
      {
        id: 'changes',
        type: 'queue',
        from: 'reviewer',
        to: ['developer'],
        when: 'payload.verdict == "changes_requested"',
      },
      { id: 'approved', type: 'fanout', from: 'reviewer', to: ['qa', 'release'], when: 'payload.verdict == "approved"' },
      { id: 'ship', type: 'queue', from: 'qa', to: ['release'], approval: true },
      { id: 'announce', type: 'broadcast', from: 'release', to: ['developer'] },
    ],
  },
};

interface Db {
  graphs: Map<string, GraphSummary>;
  replicas: Map<string, ReplicaStatus[]>;
  messages: MessageRecord[];
  executions: Execution[];
  approvals: Approval[];
  chat: OperatorMessage[];
  events: WeftEvent[];
  versions: Map<string, Graph[]>;
}

const db: Db = {
  graphs: new Map(),
  replicas: new Map(),
  messages: [],
  executions: [],
  approvals: [],
  chat: [],
  events: [],
  versions: new Map(),
};

function addGraph(g: Graph, state: 'running' | 'stopped' = 'stopped') {
  db.graphs.set(g.metadata.id, {
    graph: g,
    record: {
      id: g.metadata.id,
      version: g.metadata.version ?? 1,
      desiredState: state,
      state,
      runId: state === 'running' ? nid('run') : undefined,
      updatedAt: now(),
    },
  });
  db.versions.set(g.metadata.id, [structuredClone(g)]);
  for (const n of g.spec.nodes) {
    db.replicas.set(`${g.metadata.id}/${n.id}`, makeReplicas(g.metadata.id, n, state === 'running' ? 'idle' : 'stopped'));
  }
}

function desired(n: GraphNode): number {
  const c = n.cluster;
  if (!c) return 1;
  if (c.mode === 'autoscale') return Math.max(c.minReplicas ?? 1, 2);
  return c.replicas ?? 1;
}

function makeReplicas(graphId: string, n: GraphNode, state: ReplicaState): ReplicaStatus[] {
  return Array.from({ length: desired(n) }, (_, i) => ({
    graphId,
    nodeId: n.id,
    replica: i,
    state,
    iteration: 0,
    runIteration: 0,
    updatedAt: now(),
    branch: n.workspace?.mode === 'worktree' ? `weft/${graphId}-${n.id}-n${i}` : undefined,
  }));
}

addGraph(softwareFactory, 'running');

function emit(ev: Omit<WeftEvent, 'seq' | 'id' | 'time'>) {
  const full: WeftEvent = { ...ev, seq: ++seq, id: nid('evt'), time: now() };
  db.events.push(full);
  if (db.events.length > 5000) db.events.shift();
  mockBus().dispatchEvent(
    new CustomEvent('sse', { detail: { type: full.type, data: JSON.stringify(full), seq: full.seq } }),
  );
  return full;
}

function seedMessages() {
  const corr = nid('msg');
  const traceparent = '00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01';
  const root = mkRecord('requests', 'feature.request', 'api', 'planner', { title: 'Add dark mode' }, corr, undefined, traceparent);
  root.message.metadata![`injected_by`] = 'admin';
  const q = mkRecord('research', 'research.question', 'planner', '', { question: 'Which CSS approach?' }, corr, root.message.id, traceparent);
  const f = mkRecord('findings', 'research.done', 'researcher', 'planner', { notes: 'research/dark-mode.md' }, corr, q.message.id, traceparent);
  const t1 = mkRecord('tasks', 'task.ready', 'planner', 'developer', { task: 'Theme tokens' }, corr, f.message.id, traceparent);
  const t2 = mkRecord('tasks', 'task.ready', 'planner', 'developer', { task: 'Toggle UI' }, corr, f.message.id, traceparent);
  mkRecord('review', 'code.ready', 'developer', 'reviewer', { branch: 'weft/n0', files: 3 }, corr, t1.message.id, traceparent);
  t2.status = 'delivered';
}

function mkRecord(
  channel: string,
  type: string,
  source: string,
  target: string,
  payload: unknown,
  corr: string,
  cause: string | undefined,
  traceparent: string,
): MessageRecord {
  const id = nid('msg');
  const r: MessageRecord = {
    message: {
      id,
      type,
      source,
      target,
      payload,
      metadata: {
        graph: 'software-factory',
        channel,
        correlation_id: corr,
        causation_id: cause,
        hops: cause ? 1 : 0,
        created_at: now(),
        traceparent,
      },
    },
    graphId: 'software-factory',
    channel,
    status: 'acked',
    attempt: 1,
    createdAt: now(),
    updatedAt: now(),
  };
  db.messages.unshift(r);
  return r;
}

seedMessages();

db.approvals.push(
  {
    id: nid('apr'),
    graphId: 'software-factory',
    nodeId: 'qa',
    replica: 0,
    executionId: nid('exe'),
    kind: 'tool',
    status: 'pending',
    title: 'qa wants to run shell.exec: go test ./...',
    detail: { tool: 'shell.exec', args: { command: 'go test ./...' } },
    createdAt: now(),
  },
  {
    id: nid('apr'),
    graphId: 'software-factory',
    nodeId: 'planner',
    replica: 0,
    kind: 'question',
    status: 'pending',
    title: 'Should dark mode follow the OS setting by default?',
    detail: { question: 'Should dark mode follow the OS setting by default?' },
    createdAt: now(),
  },
);

function statusOf(id: string): GraphStatus | undefined {
  const s = db.graphs.get(id);
  if (!s) return undefined;
  const g = s.graph;
  const order: ReplicaState[] = [
    'running',
    'awaiting_approval',
    'starting',
    'idle',
    'paused',
    'stopping',
    'failed',
    'completed',
    'stopped',
  ];
  const groupsFor = (c: NonNullable<Graph['spec']['channels']>[number]): GroupStats[] => {
    const consumers = c.to?.length ? c.to : g.spec.nodes.filter((n) => n.subscribe?.length).map((n) => n.id);
    const pend = (k: string) => (s.record.state === 'running' ? (hash(k + Math.floor(Date.now() / 5000)) % 4) : 0);
    if (c.type === 'queue' || !c.type) return [{ group: c.id, pending: pend(c.id), inFlight: pend(c.id + 'f') % 2, dead: 0 }];
    if (c.type === 'broadcast')
      return consumers.flatMap((node) =>
        (db.replicas.get(`${id}/${node}`) ?? []).map((r) => ({
          group: `${c.id}.${node}.${r.replica}`,
          node,
          replica: r.replica,
          pending: pend(c.id + node + r.replica),
          inFlight: 0,
          dead: 0,
        })),
      );
    return consumers.map((node) => ({ group: `${c.id}.${node}`, node, pending: pend(c.id + node), inFlight: 0, dead: c.id === 'approved' ? 1 : 0 }));
  };
  const channels = (g.spec.channels ?? []).map((c) => ({
    channelId: c.id,
    type: c.type ?? 'queue',
    from: c.from,
    to: c.to ?? [],
    subject: `${id}.${c.id}`,
    groups: groupsFor(c),
  }));
  return {
    graphId: id,
    state: s.record.state,
    desiredState: s.record.desiredState,
    runId: s.record.runId,
    version: s.record.version,
    approvalsPending: db.approvals.filter((a) => a.graphId === id && a.status === 'pending').length,
    channels,
    nodes: g.spec.nodes.map((n) => {
      const reps = db.replicas.get(`${id}/${n.id}`) ?? [];
      const state = order.find((st) => reps.some((r) => r.state === st)) ?? 'stopped';
      const pending = channels
        .flatMap((c) => c.groups.filter((gr) => gr.node === n.id || (gr.node === undefined && c.to.includes(n.id))))
        .reduce((a, b) => a + b.pending, 0);
      return { nodeId: n.id, name: n.name, state, desiredReplicas: desired(n), replicas: reps, pending, disabled: n.disabled };
    }),
  };
}

function hash(s: string): number {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0;
  return Math.abs(h);
}

function setReplicas(graphId: string, node: string | undefined, state: ReplicaState) {
  const s = db.graphs.get(graphId);
  if (!s) return;
  for (const n of s.graph.spec.nodes) {
    if (node && n.id !== node) continue;
    if (state !== 'stopped' && n.disabled) continue;
    const reps = db.replicas.get(`${graphId}/${n.id}`) ?? makeReplicas(graphId, n, state);
    for (const r of reps) {
      r.state = state;
      r.updatedAt = now();
      emit({ type: 'replica.state_changed', graphId, nodeId: n.id, replica: r.replica, data: r });
    }
    db.replicas.set(`${graphId}/${n.id}`, reps);
  }
}

let simTimer: ReturnType<typeof setInterval> | undefined;

/** Fake engine activity on running graphs. */
export function startSimulation() {
  if (simTimer) return;
  simTimer = setInterval(() => {
    for (const s of db.graphs.values()) {
      if (s.record.state !== 'running') continue;
      const gid = s.graph.metadata.id;
      const nodes = s.graph.spec.nodes.filter((n) => !n.disabled);
      const n = nodes[Math.floor(Math.random() * nodes.length)];
      const reps = db.replicas.get(`${gid}/${n.id}`) ?? [];
      const r = reps[Math.floor(Math.random() * reps.length)];
      if (!r) continue;
      if (r.state === 'running') {
        r.state = 'idle';
        r.iteration++;
        r.runIteration++;
        const exe = db.executions.find((e) => e.id === r.executionId);
        if (exe) {
          exe.status = Math.random() < 0.85 ? 'succeeded' : 'failed';
          exe.completedAt = now();
          exe.summary = exe.status === 'succeeded' ? `Handled ${n.id} work item` : undefined;
          exe.error = exe.status === 'failed' ? 'llm: context deadline exceeded' : undefined;
          exe.usage = { inputTokens: 800 + hash(exe.id) % 3000, outputTokens: 100 + hash(exe.id) % 900 };
          emit({
            type: exe.status === 'succeeded' ? 'execution.completed' : 'execution.failed',
            graphId: gid,
            nodeId: n.id,
            replica: r.replica,
            executionId: exe.id,
            data: exe,
          });
          const out = s.graph.spec.channels?.find((c) => c.from === n.id);
          if (out && exe.status === 'succeeded') {
            const rec = mkRecord(out.id, `${n.id}.output`, n.id, out.to?.[0] ?? '', { from: n.id }, nid('msg'), undefined, '');
            rec.status = 'published';
            emit({ type: 'message.published', graphId: gid, nodeId: n.id, messageId: rec.message.id, data: rec.message });
          }
        }
        r.executionId = undefined;
      } else if (r.state === 'idle') {
        r.state = 'running';
        const exe: Execution = {
          id: nid('exe'),
          graphId: gid,
          graphVersion: s.record.version,
          runId: s.record.runId ?? '',
          nodeId: n.id,
          replica: r.replica,
          iteration: r.iteration + 1,
          status: 'running',
          usage: { inputTokens: 0, outputTokens: 0 },
          toolCalls: hash(n.id) % 5,
          traceId: '4bf92f3577b34da6a3ce929d0e0e4736',
          startedAt: now(),
        };
        db.executions.unshift(exe);
        r.executionId = exe.id;
        emit({ type: 'execution.started', graphId: gid, nodeId: n.id, replica: r.replica, executionId: exe.id, data: exe });
        emit({
          type: 'agent.output',
          graphId: gid,
          nodeId: n.id,
          replica: r.replica,
          executionId: exe.id,
          data: { text: `Turn ${exe.iteration}: reading input and planning next step…` },
        });
        emit({
          type: 'tool.invoked',
          graphId: gid,
          nodeId: n.id,
          replica: r.replica,
          executionId: exe.id,
          data: { tool: 'filesystem.read', args: { path: 'README.md' } },
        });
      }
      r.updatedAt = now();
      emit({ type: 'replica.state_changed', graphId: gid, nodeId: n.id, replica: r.replica, data: r });
    }
  }, 1200);
}

function json(body: unknown, status = 200): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  });
}

function err(status: number, message: string, code: string, details?: unknown) {
  return json(details === undefined ? { error: message, code } : { error: message, code, details }, status);
}

function readGraph(init: RequestInit): Graph {
  const ct = (init.headers as Record<string, string>)?.['Content-Type'] ?? '';
  const text = String(init.body ?? '{}');
  return (ct.includes('yaml') ? parseYaml(text) : JSON.parse(text)) as Graph;
}

type MockEnv = {
  state: 'absent' | 'building' | 'ready' | 'failed';
  hash?: string;
  image?: string;
  log?: string;
  startedAt?: string;
  finishedAt?: string;
  actor?: string;
  error?: string;
  caches?: { path: string; volume: string }[];
};
const mockEnvs = new Map<string, MockEnv>();
function mockHash(text: string): string {
  let h = 0;
  for (const c of text) h = (Math.imul(h, 31) + c.charCodeAt(0)) | 0;
  return (h >>> 0).toString(16).padStart(8, '0');
}

function validate(g: Graph) {
  const errors: { path: string; message: string }[] = [];
  const warnings: { path: string; message: string }[] = [];
  if (!g?.metadata?.id && !g?.metadata?.name) errors.push({ path: 'metadata.id', message: 'id or name is required' });
  const ids = new Set((g?.spec?.nodes ?? []).map((n) => n.id));
  // A subset of graph.Validate, so editor error states can be tried in mock mode.
  (g?.spec?.nodes ?? []).forEach((n, i) => {
    if ((n.runtime?.kind ?? 'native') === 'native' && !n.prompt?.trim())
      errors.push({ path: `spec.nodes[${i}].prompt`, message: 'native agents need a prompt' });
    if (n.loop?.mode === 'scheduled' && !n.loop.schedule)
      errors.push({ path: `spec.nodes[${i}].loop.schedule`, message: 'scheduled loops need a schedule' });
  });
  (g?.spec?.channels ?? []).forEach((c, i) => {
    if (c.from && !ids.has(c.from)) errors.push({ path: `spec.channels[${i}].from`, message: `unknown node "${c.from}"` });
    (c.to ?? []).forEach((t, j) => {
      if (!ids.has(t)) errors.push({ path: `spec.channels[${i}].to[${j}]`, message: `unknown node "${t}"` });
    });
  });
  if (!g?.spec?.nodes?.length) warnings.push({ path: 'spec.nodes', message: 'graph has no nodes' });
  return { valid: errors.length === 0, errors, warnings };
}

export async function mockFetch(url: string, init: RequestInit): Promise<Response> {
  await new Promise((r) => setTimeout(r, 80));
  const u = new URL(url, 'http://mock');
  const p = u.pathname.replace(/^\/api\/v1/, '');
  const m = init.method ?? 'GET';
  const q = u.searchParams;
  let mm: RegExpMatchArray | null;

  if (p === '/healthz') return json({ status: 'ok' });
  if (p === '/info')
    return json({
      version: '0.1.0-mock',
      instance: 'mock-1',
      queue: 'nats',
      storage: 'postgres',
      providers: ['local-ollama', 'openai-compatible', 'mock'],
      runtimes: ['native', 'external', 'echo'],
      defaultProvider: 'mock',
      grafanaUrl: 'http://localhost:3000',
      identity: { name: 'admin', role: 'admin' },
    });
  if (p === '/readyz') return json({ storage: 'ok', queue: 'ok', sandbox: 'ok' });
  if (p === '/graphs' && m === 'GET') return json([...db.graphs.values()]);
  if (p === '/graphs' && m === 'POST') {
    const g = readGraph(init);
    g.metadata.id ||= g.metadata.name.toLowerCase().replace(/[^a-z0-9]+/g, '-');
    if (db.graphs.has(g.metadata.id)) return err(409, 'graph already exists', 'conflict');
    g.metadata.version = 1;
    addGraph(g);
    emit({ type: 'graph.created', graphId: g.metadata.id });
    return json(db.graphs.get(g.metadata.id), 201);
  }
  if (p === '/graphs/validate') return json(validate(readGraph(init)));
  if ((mm = p.match(/^\/graphs\/([^/]+)$/))) {
    const s = db.graphs.get(decodeURIComponent(mm[1]));
    if (!s) return err(404, 'graph not found', 'not_found');
    if (m === 'GET') return json(s);
    if (m === 'DELETE') {
      if (s.record.state === 'running') return err(409, 'graph is running', 'conflict');
      db.graphs.delete(s.graph.metadata.id);
      return new Response(null, { status: 204 });
    }
    if (m === 'PUT') {
      const ifMatch = (init.headers as Record<string, string>)?.['If-Match'];
      if (ifMatch && Number(ifMatch) !== s.record.version)
        return err(409, `version conflict: current is ${s.record.version}`, 'conflict');
      const g = readGraph(init);
      g.metadata.id = s.graph.metadata.id;
      g.metadata.version = s.record.version + 1;
      s.graph = g;
      s.record.version = g.metadata.version;
      db.versions.get(g.metadata.id)?.push(structuredClone(g));
      for (const n of g.spec.nodes) {
        if (!db.replicas.has(`${g.metadata.id}/${n.id}`))
          db.replicas.set(`${g.metadata.id}/${n.id}`, makeReplicas(g.metadata.id, n, 'stopped'));
      }
      emit({ type: 'graph.updated', graphId: g.metadata.id, data: { version: g.metadata.version } });
      return json(s);
    }
  }
  if ((mm = p.match(/^\/graphs\/([^/]+)\/versions$/)))
    return json(
      (db.versions.get(mm[1]) ?? []).map((g, i) => ({ version: g.metadata.version ?? i + 1, createdAt: now(), author: 'admin' })),
    );
  if ((mm = p.match(/^\/graphs\/([^/]+)\/export$/))) {
    const s = db.graphs.get(mm[1]);
    if (!s) return err(404, 'not found', 'not_found');
    const { stringify } = await import('yaml');
    return new Response(q.get('format') === 'json' ? JSON.stringify(s.graph, null, 2) : stringify(s.graph), {
      headers: { 'content-type': 'text/plain' },
    });
  }
  if ((mm = p.match(/^\/graphs\/([^/]+)\/environment(?:\/(build|reset))?$/))) {
    const s = db.graphs.get(decodeURIComponent(mm[1]));
    if (!s) return err(404, 'graph not found', 'not_found');
    const id = s.graph.metadata.id;
    const env = s.graph.spec.environment;
    const current = env ? mockHash(JSON.stringify(env)) : undefined;
    const st = mockEnvs.get(id) ?? { state: 'absent' as const };
    const view = () => {
      const e = mockEnvs.get(id) ?? { state: 'absent' as const };
      return { graph: id, defined: !!env, currentHash: current, upToDate: e.state === 'ready' && e.hash === current, ...e };
    };
    if (mm[2] === 'build') {
      if (!env) return err(400, 'the graph defines no environment (spec.environment)', 'invalid');
      if (st.state === 'building') return err(409, 'an environment build is already running', 'conflict');
      mockEnvs.set(id, { state: 'building', hash: current, startedAt: now(), actor: 'admin', log: `==> ${env.image ? `Pulling base image ${env.image}` : 'Building the environment Dockerfile'}\n` });
      emit({ type: 'environment.build_started', graphId: id, data: { hash: current } });
      setTimeout(() => {
        const log = `${mockEnvs.get(id)?.log ?? ''}==> Adding the sandbox user (uid 10001)\n${(env.setup ?? []).map((c) => `$ ${c}\n`).join('')}==> Environment ready: weft-env/${id}:${current}\n`;
        mockEnvs.set(id, { state: 'ready', hash: current, image: `weft-env/${id}:${current}`, startedAt: mockEnvs.get(id)?.startedAt, finishedAt: now(), actor: 'admin', log,
          caches: (env.caches ?? []).map((c) => ({ path: c, volume: `weft-env-${id}-${mockHash(c)}` })) });
        emit({ type: 'environment.ready', graphId: id, data: { hash: current } });
      }, 3000);
      return json(view(), 202);
    }
    if (mm[2] === 'reset') {
      if (s.record.state !== 'stopped' && s.record.state !== 'failed')
        return err(409, `graph ${id} is ${s.record.state}; stop it before resetting its environment`, 'conflict');
      mockEnvs.delete(id);
      emit({ type: 'environment.reset', graphId: id });
      return json(view());
    }
    return json(view());
  }
  if ((mm = p.match(/^\/graphs\/([^/]+)\/status$/))) {
    const st = statusOf(mm[1]);
    return st ? json(st) : err(404, 'not found', 'not_found');
  }
  if ((mm = p.match(/^\/graphs\/([^/]+)\/(start|stop|pause|resume|kill)$/))) {
    const s = db.graphs.get(mm[1]);
    if (!s) return err(404, 'not found', 'not_found');
    const a = mm[2];
    const st = a === 'start' || a === 'resume' ? 'running' : a === 'pause' ? 'paused' : 'stopped';
    s.record.state = st;
    s.record.desiredState = st;
    if (a === 'start') s.record.runId = nid('run');
    setReplicas(mm[1], undefined, st === 'running' ? 'idle' : st === 'paused' ? 'paused' : 'stopped');
    emit({ type: `graph.${a === 'start' ? 'started' : a === 'resume' ? 'resumed' : a === 'pause' ? 'paused' : 'stopped'}`, graphId: mm[1] });
    return json(statusOf(mm[1]));
  }
  if ((mm = p.match(/^\/graphs\/([^/]+)\/nodes\/([^/]+)\/(start|stop|pause|resume|kill)$/))) {
    const a = mm[3];
    setReplicas(mm[1], mm[2], a === 'start' || a === 'resume' ? 'idle' : a === 'pause' ? 'paused' : 'stopped');
    return json(statusOf(mm[1]));
  }
  if ((mm = p.match(/^\/graphs\/([^/]+)\/nodes\/([^/]+)\/scale$/))) {
    const { replicas } = JSON.parse(String(init.body)) as { replicas: number };
    const key = `${mm[1]}/${mm[2]}`;
    const reps = db.replicas.get(key) ?? [];
    const st = reps[0]?.state ?? 'stopped';
    const next = Array.from({ length: replicas }, (_, i) => reps[i] ?? { ...reps[0], replica: i, state: st, iteration: 0, runIteration: 0, updatedAt: now() });
    db.replicas.set(key, next);
    const n = db.graphs.get(mm[1])?.graph.spec.nodes.find((x) => x.id === mm![2]);
    if (n) n.cluster = { ...(n.cluster ?? { mode: 'fixed' }), replicas };
    emit({ type: 'node.scaled', graphId: mm[1], nodeId: mm[2], data: { replicas } });
    return json(statusOf(mm[1]));
  }
  if ((mm = p.match(/^\/graphs\/([^/]+)\/nodes\/([^/]+)\/chat$/))) {
    if (m === 'GET') return json(db.chat.filter((c) => c.graphId === mm![1] && c.nodeId === mm![2]));
    const { text, replica } = JSON.parse(String(init.body)) as { text: string; replica?: number };
    const om: OperatorMessage = { id: nid('op'), graphId: mm[1], nodeId: mm[2], replica: replica ?? -1, text, author: 'admin', createdAt: now() };
    db.chat.push(om);
    emit({ type: 'operator.message', graphId: mm[1], nodeId: mm[2], replica: om.replica, actor: 'admin', data: om });
    setTimeout(
      () =>
        emit({
          type: 'agent.output',
          graphId: om.graphId,
          nodeId: om.nodeId,
          replica: Math.max(0, om.replica),
          data: { text: `Acknowledged operator message: "${text}"` },
        }),
      900,
    );
    return json(om, 201);
  }
  if ((mm = p.match(/^\/graphs\/([^/]+)\/channels\/([^/]+)\/messages$/))) {
    const body = JSON.parse(String(init.body)) as { type: string; payload: unknown; target?: string };
    const ch = db.graphs.get(mm[1])?.graph.spec.channels?.find((c) => c.id === mm![2]);
    const r = mkRecord(mm[2], body.type, 'operator', body.target ?? '', body.payload, nid('msg'), undefined, '');
    r.graphId = mm[1];
    r.message.metadata!.injected_by = 'admin';
    r.status = ch?.approval ? 'held' : 'published';
    emit({ type: 'message.injected', graphId: mm[1], messageId: r.message.id, actor: 'admin', data: r.message });
    return json(r.message, 201);
  }
  if ((mm = p.match(/^\/graphs\/([^/]+)\/messages$/))) {
    let list = db.messages.filter((r) => r.graphId === mm![1]);
    if (q.get('channel')) list = list.filter((r) => r.channel === q.get('channel'));
    if (q.get('node')) list = list.filter((r) => r.message.source === q.get('node') || r.message.target === q.get('node'));
    if (q.get('correlationId')) list = list.filter((r) => r.message.metadata?.correlation_id === q.get('correlationId'));
    if (q.get('status')) list = list.filter((r) => r.status === q.get('status'));
    return json(list.slice(0, Number(q.get('limit') ?? 100)));
  }
  if ((mm = p.match(/^\/messages\/([^/]+)$/))) return json(db.messages.filter((r) => r.message.id === mm![1]));
  if ((mm = p.match(/^\/graphs\/([^/]+)\/executions$/))) {
    let list = db.executions.filter((e) => e.graphId === mm![1]);
    if (q.get('node')) list = list.filter((e) => e.nodeId === q.get('node'));
    return json(list.slice(0, Number(q.get('limit') ?? 50)));
  }
  if ((mm = p.match(/^\/executions\/([^/]+)$/))) {
    const e = db.executions.find((x) => x.id === mm![1]);
    return e ? json(e) : err(404, 'not found', 'not_found');
  }
  if ((mm = p.match(/^\/graphs\/([^/]+)\/events$/))) {
    const after = Number(q.get('after') ?? 0);
    return json(db.events.filter((e) => e.graphId === mm![1] && e.seq > after).slice(-Number(q.get('limit') ?? 200)));
  }
  if (p === '/approvals') {
    let list = db.approvals;
    if (q.get('status')) list = list.filter((a) => a.status === q.get('status'));
    if (q.get('graph')) list = list.filter((a) => a.graphId === q.get('graph'));
    return json(list);
  }
  if ((mm = p.match(/^\/approvals\/([^/]+)\/(approve|reject)$/))) {
    const a = db.approvals.find((x) => x.id === mm![1]);
    if (!a) return err(404, 'not found', 'not_found');
    if (a.status !== 'pending') return err(409, 'approval already decided', 'conflict');
    const body = JSON.parse(String(init.body ?? '{}')) as { comment?: string; answer?: string };
    a.status = mm[2] === 'approve' ? 'approved' : 'rejected';
    a.comment = body.comment;
    a.answer = body.answer;
    a.decidedBy = 'admin';
    a.decidedAt = now();
    emit({ type: 'approval.decided', graphId: a.graphId, nodeId: a.nodeId, data: a });
    return json(a);
  }
  if (p === '/provider-settings' && m === 'GET') return json(providerSettings());
  if (p === '/provider-plugins' && m === 'GET') return json(providerPlugins());
  if (p.startsWith('/cost/')) return mockCost(p, m, init);
  if ((mm = p.match(/^\/provider-plugins\/([^/]+)$/)) && m === 'PUT') {
    const id = decodeURIComponent(mm[1]);
    const body = JSON.parse(String(init.body ?? '{}')) as { enabled?: boolean };
    if (!providerPlugins().some((x) => x.id === id)) return err(404, `inference plugin "${id}" not found`, 'not_found');
    if (body.enabled) mockProviders.enabled.add(id);
    else mockProviders.enabled.delete(id);
    mockProviders.revision++;
    return json(providerPlugins().find((x) => x.id === id));
  }
  if (p === '/provider-settings' && m === 'PUT') {
    const body = JSON.parse(String(init.body ?? '{}')) as { default?: string };
    mockProviders.default = body.default || 'mock';
    mockProviders.revision++;
    return json(providerSettings());
  }
  if (p === '/providers' && m === 'GET') return json(providerList());
  if (p === '/providers' && m === 'POST') {
    const input = JSON.parse(String(init.body ?? '{}')) as ProviderInput;
    if (!input.name || !/^[a-z0-9][a-z0-9._-]*$/.test(input.name))
      return err(422, 'invalid provider', 'invalid', { valid: false, errors: [{ path: 'name', message: 'use lowercase letters, digits and dashes' }] });
    if (mockProviders.items.some((x) => x.name === input.name))
      return err(422, 'invalid provider', 'invalid', { valid: false, errors: [{ path: 'name', message: 'already exists' }] });
    const item: ProviderInfo = {
      name: input.name, type: input.type, baseUrl: input.baseUrl, defaultModel: input.defaultModel,
      source: 'managed', editable: true, status: 'ready',
      credential: input.apiKey && 'value' in input.apiKey ? { configured: true, source: 'stored', updatedAt: now() } : { configured: false, source: 'none' },
    };
    mockProviders.items.push(item);
    mockProviders.revision++;
    return json(item, 201);
  }
  if (p === '/providers/test' && m === 'POST') {
    const input = JSON.parse(String(init.body ?? '{}')) as ProviderInput;
    return json(mockTest(input.name || 'draft', input.defaultModel, true));
  }
  if ((mm = p.match(/^\/providers\/([^/]+)\/test$/)) && m === 'POST') {
    const name = decodeURIComponent(mm[1]);
    const prov = providerList().find((x) => x.name === name);
    if (!prov) return err(404, `provider "${name}" not found`, 'not_found');
    return json(mockTest(name, prov.defaultModel, false));
  }
  if ((mm = p.match(/^\/providers\/([^/]+)$/)) && (m === 'PUT' || m === 'DELETE')) {
    const name = decodeURIComponent(mm[1]);
    const i = mockProviders.items.findIndex((x) => x.name === name);
    if (i < 0) return err(404, `provider "${name}" not found`, 'not_found');
    mockProviders.revision++;
    if (m === 'DELETE') {
      mockProviders.items.splice(i, 1);
      return new Response(null, { status: 204 });
    }
    const input = JSON.parse(String(init.body ?? '{}')) as ProviderInput;
    const cur = mockProviders.items[i];
    const credential =
      input.apiKey && 'clear' in input.apiKey
        ? { configured: false, source: 'none' as const }
        : input.apiKey
          ? { configured: true, source: 'stored' as const, updatedAt: now() }
          : cur.credential;
    mockProviders.items[i] = { ...cur, type: input.type, baseUrl: input.baseUrl, defaultModel: input.defaultModel, credential };
    return json(mockProviders.items[i]);
  }
  if ((mm = p.match(/^\/providers\/([^/]+)\/models$/))) {
    const provider = decodeURIComponent(mm[1]);
    if (provider === 'mock') return json([{ id: 'mock-1', provider }]);
    if (provider === 'local-ollama')
      return json([
        { id: 'llama3.1:8b', provider },
        { id: 'qwen2.5-coder:7b', provider },
      ]);
    return err(404, `unknown provider "${provider}"`, 'not_found');
  }
  if (p === '/tools')
    return json(
      ['filesystem.read', 'filesystem.write', 'shell.exec', 'git.read', 'git.write', 'http.fetch', 'human.ask'].map((t) => ({
        name: t,
        capability: t,
        description: `${t} tool`,
        schema: { type: 'object' },
      })),
    );
  if ((mm = p.match(/^\/graphs\/([^/]+)\/files$/)))
    return json([
      { name: 'plans', path: 'plans', dir: true, size: 0, modTime: now() },
      { name: 'README.md', path: 'README.md', dir: false, size: 512, modTime: now() },
    ]);
  if ((mm = p.match(/^\/graphs\/([^/]+)\/files\/content$/)))
    return new Response(`# ${q.get('path')}\n\nmock file content`, { headers: { 'content-type': 'text/plain' } });
  return err(404, `mock: no route for ${m} ${p}`, 'not_found');
}

// --- model providers (console-managed ones live in memory) ---------------
const mockProviders: { revision: number; default: string; items: ProviderInfo[]; enabled: Set<string> } = {
  revision: 1,
  default: 'mock',
  items: [
    {
      name: 'local-ollama', type: 'ollama', baseUrl: 'http://ollama:11434', defaultModel: 'llama3.1:8b',
      source: 'managed', editable: true, status: 'ready', credential: { configured: false, source: 'none' },
    },
  ],
  enabled: new Set(['ollama']),
};

const mockCaps = { tools: true, json: 'native' as const, usage: true, modelListing: true, streaming: false };

function providerPlugins(): ProviderPlugin[] {
  return providerSettings().types.map((t) => ({
    id: t.type, label: t.label, builtin: true, local: t.type === 'ollama' || t.type === 'mock', defaultBaseUrl: t.defaultBaseUrl,
    key: t.keyRequired ? 'required' : 'none', capabilities: mockCaps, devOnly: t.type === 'mock' || undefined,
    enabled: t.type === 'mock' || mockProviders.enabled.has(t.type),
    enabledBy: t.type === 'mock' ? 'config' : mockProviders.enabled.has(t.type) ? 'console' : undefined,
    instances: providerList().filter((x) => x.type === t.type).map((x) => x.name),
  }));
}

function providerList(): ProviderInfo[] {
  const config: ProviderInfo[] = [
    { name: 'mock', type: 'mock', defaultModel: 'mock-1', source: 'config', editable: false, status: 'ready', credential: { configured: false, source: 'none' } },
  ];
  return [...config, ...mockProviders.items].map((x) => ({ ...x, default: x.name === mockProviders.default }));
}

function providerSettings() {
  return {
    revision: mockProviders.revision,
    default: mockProviders.default,
    configDefault: 'mock',
    secrets: { enabled: true, keySource: 'generated', keyId: 'mock0001' },
    types: [
      { type: 'anthropic', label: 'Anthropic', defaultBaseUrl: 'https://api.anthropic.com', keyRequired: true },
      { type: 'mock', label: 'Mock (offline, deterministic)', keyRequired: false },
      { type: 'ollama', label: 'Ollama', defaultBaseUrl: 'http://localhost:11434', keyRequired: false },
      { type: 'openai', label: 'OpenAI', defaultBaseUrl: 'https://api.openai.com/v1', keyRequired: true },
    ],
  };
}

function mockTest(name: string, model: string | undefined, draft: boolean): ProviderTestResult {
  return {
    provider: name, ok: true, checkedAt: now(), durationMs: 140, draft, model: model || 'mock-1',
    models: { ok: true, count: 2, defaultModelListed: true },
    inference: { ok: true, reply: 'OK', stopReason: 'stop', inputTokens: 12, outputTokens: 1, durationMs: 120 },
  };
}

// --- costs (in memory; no spend is generated by the mock) ----------------
const mockCosts = {
  prices: { version: 0, currency: 'USD', unpriced: 'allow' as const, prices: [] as unknown[] },
  policy: { taskRunBudgetMicros: 0, warningPercent: 80 },
  budgets: [] as unknown[],
};

function mockCost(p: string, m: string, init: RequestInit): Response {
  const body = () => JSON.parse(String(init.body ?? '{}'));
  const period = new Date().toISOString().slice(0, 7);
  if (p === '/cost/prices' && m === 'GET') return json(mockCosts.prices);
  if (p === '/cost/prices' && m === 'PUT') {
    mockCosts.prices = { ...body(), version: mockCosts.prices.version + 1 };
    return json(mockCosts.prices);
  }
  if (p === '/cost/policy' && m === 'GET') return json(mockCosts.policy);
  if (p === '/cost/policy' && m === 'PUT') return json((mockCosts.policy = { ...mockCosts.policy, ...body() }));
  if (p === '/cost/budgets' && m === 'GET') return json({ budgets: mockCosts.budgets, period });
  if (p === '/cost/budgets' && m === 'PUT') {
    mockCosts.budgets = body().budgets ?? [];
    return json({ budgets: mockCosts.budgets, period });
  }
  if (p === '/cost/budgets/preview') return json({ taskRunBudgetMicros: mockCosts.policy.taskRunBudgetMicros, currency: mockCosts.prices.currency, budgets: [] });
  if (p === '/cost/ledgers') return json([]);
  if (p === '/cost/spend') {
    const zero = { key: 'total', calls: 0, inputTokens: 0, outputTokens: 0, costMicros: 0, estimatedMicros: 0, unpricedCalls: 0 };
    return json({ period, currency: mockCosts.prices.currency, total: zero, byProvider: [], byTask: [], byUser: [], byProject: [], byWorkflow: [] });
  }
  return err(404, `mock: no cost route for ${m} ${p}`, 'not_found');
}
