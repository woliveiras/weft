import type { ExecutionStatus, GraphState, ReplicaState, Usage } from './types';

/** GET /api/v1/activity — see docs/api.md (Activity). */
export type ActivityStatusFilter = '' | 'active' | 'succeeded' | 'failed' | 'cancelled';

export interface ActivityQuery {
  window?: string;
  limit?: number;
  status?: ActivityStatusFilter;
  workflow?: string;
  agent?: string;
}

export interface ActivityNeed {
  /** An approval kind (tool, question, message, turn, delegation, environment) or "review". */
  kind: string;
  id: string;
  title: string;
  graphId: string;
  graphName?: string;
  task?: string;
  nodeId?: string;
  replica: number;
  executionId?: string;
  createdAt: string;
  expiresAt?: string;
}

export interface ActivityReplica {
  nodeId: string;
  replica: number;
  state: ReplicaState;
  phase: string;
  detail?: string;
  executionId?: string;
  startedAt?: string;
  elapsedMs?: number;
  planStep?: string;
  usage: Usage;
  toolCalls: number;
  /** Null when cost could not be read. */
  costMicros: number | null;
}

export interface ActivityGroup {
  graphId: string;
  graphName?: string;
  task?: string;
  workflow: string;
  state: GraphState;
  replicas: ActivityReplica[];
}

export interface ActivityRow {
  id: string;
  graphId: string;
  graphName?: string;
  task?: string;
  workflow: string;
  nodeId: string;
  replica: number;
  status: ExecutionStatus;
  startedAt: string;
  completedAt?: string;
  durationMs: number;
  toolCalls: number;
  error?: string;
  usage: Usage;
  costMicros: number | null;
}

export interface ActivitySectionError {
  section: 'needsYou' | 'reviews' | 'running' | 'history' | 'cost' | string;
  code: 'forbidden' | 'unavailable' | 'failed' | string;
  message: string;
}

export interface ActivityView {
  window: string;
  since: string;
  generatedAt: string;
  currency?: string;
  counters: { running: number | null; waiting: number | null; failed: number | null; finished: number | null; cancelled: number | null };
  /** Each list is null when its section failed (see errors). */
  needsYou: ActivityNeed[] | null;
  running: ActivityGroup[] | null;
  history: ActivityRow[] | null;
  options: { workflows: { id: string; name?: string }[]; agents: string[] } | null;
  limits: { history: number; needsYou: number; executions: number; activeGraphs: number; replicas: number; costEvents: number };
  truncated: { needsYou: boolean; running: boolean; executions: boolean; history: boolean; cost: boolean };
  errors: ActivitySectionError[];
}
