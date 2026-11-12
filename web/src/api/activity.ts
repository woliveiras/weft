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
  triggerHolds?: { triggerId: string; triggerName: string; workflow: string; key: string; url?: string; reason: string; round: number; eligibility?: string; publication?: string; disabled?: boolean }[];
  triggerHoldsTruncated?: boolean;
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

/** GET /api/v1/activity/health — see docs/api.md (Activity health). */
export interface HealthQuery {
  /** 24h or 7d. */
  window?: string;
  /** Rows per ranking, 1–50. */
  ranking?: number;
}

export interface HealthRunRef {
  id: string;
  graphId: string;
  status: ExecutionStatus;
  startedAt: string;
}

export interface HealthErrorGroup {
  /** Normalized: first line only, ids/paths/URLs/numbers as placeholders, credentials redacted. */
  message: string;
  count: number;
  lastAt: string;
  lastRun: HealthRunRef;
}

export interface HealthRow {
  /** Unique within its list; what rankings refer to. */
  key: string;
  workflow: string;
  workflowName?: string;
  /** Agents only. */
  nodeId?: string;
  runs: number;
  finished: number;
  succeeded: number;
  failed: number;
  cancelled: number;
  /** failed / finished; null with no finished run. */
  failureRate: number | null;
  p50Ms: number | null;
  p90Ms: number | null;
  usage: Usage;
  /** Null when cost could not be read. */
  costMicros: number | null;
  topErrors: HealthErrorGroup[];
  otherErrors: number;
  lastRun?: HealthRunRef;
  lastFailure?: HealthRunRef;
}

export interface HealthRanking {
  agents: string[];
  workflows: string[];
}

export interface HealthView {
  window: string;
  since: string;
  generatedAt: string;
  currency?: string;
  /** Null when executions could not be read (see errors). */
  agents: HealthRow[] | null;
  workflows: HealthRow[] | null;
  rankings: { failsMost: HealthRanking; slowest: HealthRanking; mostExpensive: HealthRanking | null } | null;
  limits: { executions: number; costEvents: number; rows: number; ranking: number; errors: number };
  truncated: { executions: boolean; cost: boolean; agents: boolean; workflows: boolean };
  errors: ActivitySectionError[];
}
