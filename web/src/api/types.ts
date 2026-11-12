import type { DelegationPolicyView, DelegationProfileView } from './delegation';
// TypeScript mirrors of the Go domain types (server/internal/domain, runtime,
// llm, tools). Field names are the Go JSON tags. Durations are Go duration
// strings ("90s", "2h") or "" when unset.

export type Duration = string;

export interface Message {
  id: string;
  type: string;
  source: string;
  target: string;
  payload: unknown;
  metadata: Record<string, unknown> | null;
}

export interface Graph {
  apiVersion: string;
  kind: string;
  metadata: GraphMetadata;
  spec: GraphSpec;
}

export interface GraphMetadata {
  id: string;
  name: string;
  description?: string;
  labels?: Record<string, string>;
  version?: number;
  createdAt?: string;
  updatedAt?: string;
}

export interface GraphSpec {
  instructions?: import("./instructions").InstructionRefs;
  parameters?: Parameter[];
  workspace?: { path?: string; git?: boolean; repo?: string };
  environment?: EnvironmentSpec;
  defaults?: Partial<Pick<GraphNode, 'runtime' | 'model' | 'tools' | 'isolation' | 'loop'>>;
  nodes: GraphNode[];
  channels?: Channel[];
  /** Declared verification commands run by the check.run tool. */
  checks?: DeclaredCheck[];
  maxHops?: number;
}

/** A declared check: a name (test, lint, build, custom) plus a command. */
export interface DeclaredCheck {
  name: string;
  command: string;
  cwd?: string;
  timeoutSec?: number;
}

/** spec.environment: the container image agents' commands run in (docker isolation). */
export interface EnvironmentSelection {
 source: string;
 path: string;
 sha256: string;
 service?: string;
 target?: string;
}
export interface EnvironmentDiscovery {
 source: string;
 status: 'configured' | 'review_required' | 'invalid' | 'proposal_required';
 candidates: { path: string; sha256: string; services?: string[]; targets?: string[]; error?: string }[];
}
export interface EnvironmentSpec {
 companions?: { name: string; image: string; command?: string[] }[];
 origin?: EnvironmentSelection & { files: Record<string, string>; dockerfilePath?: string; contextSHA256?: string };

  image?: string;
  dockerfile?: string;
  context?: string;
  target?: string;
  setup?: string[];
  network?: { setup?: 'none' | 'bridge' | 'egress'; setupAllowedHosts?: string[]; allowedHosts?: string[]; run?: 'none' | 'bridge' | 'egress' };
  caches?: string[];
  timeout?: Duration;
  cpu?: string;
  memory?: string;
}

export type EnvironmentState = 'absent' | 'building' | 'ready' | 'failed';

/** GET /graphs/{id}/environment: build state compared with the saved definition. */
export interface EnvironmentView {
  phase?: string;
  reuseDecision?: string;
  buildReasons?: string[];
  changeReasons?: string[];
  graph: string;
  state: EnvironmentState;
  defined: boolean;
  scope?: string;
  reuseKey?: string;
  reused?: boolean;
  inputs?: string;
  currentHash?: string;
  hash?: string;
  upToDate: boolean;
  image?: string;
  caches?: { path: string; volume: string }[];
  startedAt?: string;
  finishedAt?: string;
  error?: string;
  log?: string;
  actor?: string;
}

export interface Parameter {
  name: string;
  value: string;
  description?: string;
}

export type RuntimeKind = 'native' | 'external' | 'echo';
export type LoopMode = 'once' | 'continuous' | 'scheduled' | 'event';
export type StopPolicy = 'never' | 'auto' | 'iterations' | 'duration' | 'idle';
export type ClusterMode = 'fixed' | 'autoscale';
export type ClusterComms = 'isolated' | 'ring';
export type IsolationMode = 'process' | 'docker' | 'kubernetes';
export type ChannelType = 'queue' | 'topic' | 'fanout' | 'broadcast';

export interface RuntimeRef {
  kind?: RuntimeKind;
  framework?: string;
  agent?: string;
  endpoint?: string;
  command?: string[];
  image?: string;
  env?: Record<string, string>;
}

export interface ModelRef {
  provider?: string;
  name?: string;
  temperature?: number;
  maxTokens?: number;
}

export interface ToolGrant {
  name: string;
  paths?: string[];
  commands?: string[];
  hosts?: string[];
  /** SCM connections git.push and pull request tools may use (empty grants none). */
  repositories?: string[];
  requiresApproval?: boolean;
  rateLimit?: number;
}

export interface LoopSpec {
  mode?: LoopMode;
  schedule?: string;
  interval?: Duration;
  stop?: { policy?: StopPolicy; maxIterations?: number; maxDuration?: Duration; idleTimeout?: Duration };
  batchSize?: number;
}

export interface ClusterSpec {
  mode?: ClusterMode;
  replicas?: number;
  minReplicas?: number;
  maxReplicas?: number;
  comms?: ClusterComms;
  scaleDownAfter?: Duration;
}

export interface IsolationSpec {
  mode?: IsolationMode;
  image?: string;
  cpu?: string;
  memory?: string;
  network?: string;
  env?: Record<string, string>;
}

export interface GraphNode {
  skillIds?: string[] | null;
  repositoryGuidance?: import("./instructions").RepositoryGuidancePolicy;
  id: string;
  name?: string;
  description?: string;
  runtime?: RuntimeRef;
  model?: ModelRef;
  prompt?: string;
  // Grants may come back as bare strings from hand-written documents.
  tools?: (ToolGrant | string)[];
  loop?: LoopSpec;
  cluster?: ClusterSpec;
  isolation?: IsolationSpec;
  workspace?: { mode?: 'shared' | 'worktree'; subdir?: string; readOnly?: boolean };
  approval?: { beforeTurn?: boolean; beforeEmit?: boolean; timeout?: Duration; onTimeout?: string };
  subscribe?: string[];
  limits?: { turnTimeout?: Duration; maxToolCalls?: number; maxTokens?: number };
  disabled?: boolean;
  config?: Record<string, unknown>;
  position?: { x: number; y: number } | null;
}

export interface Channel {
  id: string;
  type?: ChannelType;
  from?: string;
  to?: string[];
  topic?: string;
  types?: string[];
  when?: string;
  approval?: boolean;
  maxDeliver?: number;
  ackWait?: Duration;
  retention?: Duration;
  label?: string;
}

export type GraphState = 'stopped' | 'running' | 'paused' | 'stopping' | 'failed';
export type ReplicaState =
  | 'stopped'
  | 'starting'
  | 'idle'
  | 'running'
  | 'awaiting_approval'
  | 'paused'
  | 'stopping'
  | 'completed'
  | 'failed';
export type ExecutionStatus =
  | 'pending'
  | 'running'
  | 'awaiting_approval'
  | 'succeeded'
  | 'failed'
  | 'cancelled'
  | 'interrupted';
export type MessageStatus =
  | 'published'
  | 'held'
  | 'rejected'
  | 'delivered'
  | 'acked'
  | 'nacked'
  | 'dead_lettered'
  | 'dropped';

export interface GraphRecord {
  id: string;
  version: number;
  desiredState: GraphState;
  state: GraphState;
  runId?: string;
  startedAt?: string;
  updatedAt: string;
}

export interface GraphSummary {
  graph: Graph;
  record: GraphRecord;
}

export interface ReplicaStatus {
  graphId: string;
  nodeId: string;
  replica: number;
  state: ReplicaState;
  iteration: number;
  runIteration: number;
  executionId?: string;
  instance?: string;
  detail?: string;
  workdir?: string;
  branch?: string;
  deadline?: string;
  updatedAt: string;
}

export interface GroupStats {
  group: string;
  node?: string;
  replica?: number;
  pending: number;
  inFlight: number;
  dead: number;
}

export interface NodeStatus {
  nodeId: string;
  name?: string;
  state: ReplicaState;
  desiredReplicas: number;
  replicas: ReplicaStatus[] | null;
  pending: number;
  disabled?: boolean;
}

export interface ChannelStatus {
  channelId: string;
  type: ChannelType;
  from?: string;
  to: string[] | null;
  subject?: string;
  groups: GroupStats[] | null;
}

export interface GraphStatus {
  graphId: string;
  state: GraphState;
  desiredState: GraphState;
  runId?: string;
  version: number;
  nodes: NodeStatus[] | null;
  channels: ChannelStatus[] | null;
  approvalsPending: number;
}

export interface Usage {
  inputTokens: number;
  outputTokens: number;
  /** Parts of inputTokens read from / written to the provider's prompt cache, when reported. */
  cacheReadTokens?: number;
  cacheWriteTokens?: number;
}

export interface Execution {
  parentExecutionId?: string;
  delegationTemplate?: string;
  delegationConfig?: {policy:DelegationPolicyView;profile:DelegationProfileView};
  delegationObjective?: string;
  delegationModel?: {provider?: string; name?: string};
  delegationAudit?: {runtime: string; effectiveGrants: ToolGrant[]; workspaceAccess: boolean; recursiveAccess: boolean; maxOutputTokens: number; decision: string; decidedBy?: string; decidedAt?: string};
  approvalId?: string;

  id: string;
  graphId: string;
  graphVersion: number;
  runId: string;
  nodeId: string;
  replica: number;
  iteration: number;
  status: ExecutionStatus;
  inputIds?: string[];
  outputIds?: string[];
  summary?: string;
  error?: string;
  signal?: string;
  usage: Usage;
  toolCalls: number;
  traceId?: string;
  instance?: string;
  startedAt: string;
  completedAt?: string;
}

export interface MessageRecord {
  message: Message;
  graphId: string;
  channel: string;
  group?: string;
  status: MessageStatus;
  attempt: number;
  consumer?: string;
  error?: string;
  createdAt: string;
  updatedAt: string;
}

export type ApprovalKind = 'tool' | 'message' | 'turn' | 'question' | 'environment' | 'delegation';
export type ApprovalStatus = 'pending' | 'approved' | 'rejected' | 'expired';

export interface Approval {
  id: string;
  graphId: string;
  nodeId?: string;
  replica: number;
  executionId?: string;
  kind: ApprovalKind;
  status: ApprovalStatus;
  title: string;
  detail?: unknown;
  answer?: string;
  decidedBy?: string;
  comment?: string;
  createdAt: string;
  expiresAt?: string;
  decidedAt?: string;
}

export interface OperatorMessage {
  id: string;
  graphId: string;
  nodeId: string;
  replica: number;
  text: string;
  author: string;
  createdAt: string;
  consumedAt?: string;
}

export interface WeftEvent {
  seq: number;
  id: string;
  type: string;
  time: string;
  graphId?: string;
  nodeId?: string;
  replica?: number;
  executionId?: string;
  messageId?: string;
  actor?: string;
  traceId?: string;
  level?: string;
  data?: unknown;
}

export interface Info {
  nativeProfile?: boolean;
  version: string;
  instance: string;
  queue: string;
  storage: string;
  providers: unknown[];
  runtimes: string[];
  defaultProvider?: string;
  grafanaUrl?: string;
  identity?: { name: string; role: 'viewer' | 'operator' | 'admin' };
}

/** Where a provider credential comes from; the value itself is never returned. */
export interface CredentialState {
  configured: boolean;
  /** stored (encrypted by Weft) | env | file | none */
  source: 'stored' | 'env' | 'file' | 'none';
  env?: string;
  updatedAt?: string;
  error?: string;
}

export interface ProviderTestResult {
  provider: string;
  ok: boolean;
  checkedAt: string;
  durationMs: number;
  draft?: boolean;
  model?: string;
  models: { ok: boolean; count: number; error?: string; defaultModelListed?: boolean };
  inference: {
    ok: boolean;
    reply?: string;
    stopReason?: string;
    inputTokens?: number;
    outputTokens?: number;
    durationMs?: number;
    error?: string;
  };
  /** Whether the model calls tools in the structured form agents need (advisory). */
  tools?: { checked: boolean; ok: boolean; reply?: string; error?: string };
}

export interface ProviderInfo {
  name: string;
  type: string;
  baseUrl?: string;
  defaultModel?: string;
  default?: boolean;
  timeoutSec?: number;
  maxRetries?: number;
  concurrency?: number;
  /** Context size in tokens: sent to Ollama as num_ctx; for other types used to budget and check prompts. 0/absent = the type's default. */
  contextWindow?: number;
  /** "small": compact tool schemas, call shapes and one tool call per reply for small local models. */
  modelGuidance?: '' | 'small';
  /** Cloud project and region (Gemini on Vertex AI); identifiers, never credentials. */
  project?: string;
  region?: string;
  /** config: defined in the server configuration (read-only here); managed: created in the console. */
  source?: 'config' | 'managed';
  editable?: boolean;
  credential?: CredentialState;
  status?: 'ready' | 'unavailable';
  error?: string;
  lastTest?: ProviderTestResult;
  updatedAt?: string;
  updatedBy?: string;
}

export interface ProviderTypeInfo {
  type: string;
  label: string;
  defaultBaseUrl?: string;
  keyRequired: boolean;
  /** Optional instance settings the type honours: contextWindow, project, region. */
  settings?: string[];
}

export interface TriggerSource {
  kind: 'github' | 'jira';
  connectionId: string;
  repository?: string;
  cloudId?: string;
  query?: string;
}

export interface TriggerIssue {
  source: string;
  key: string;
  title: string;
  url?: string;
  labels: string[];
}

export interface TriggerReadinessLimits {
  inputBytes: number;
  outputTokens: number;
  seconds: number;
  moneyMicros: number;
  currency: string;
  maxRounds: number;
  maxHolds: number;
}

export interface TriggerInput {
  readinessDisabled?: boolean;
  name: string;
  workflow: string;
  nodeId: string;
  resultNodeId?: string;
  source: TriggerSource;
  label: string;
  owner?: string;
  intervalMinutes: number;
  scmConnection?: string;
  projectId?: string;
  executionMode?: string;
  authorizationMode: 'ask' | 'workspace' | 'full';
}

export interface Trigger extends TriggerInput {
  id: string;
  state: 'active' | 'paused' | 'finished';
  pausedReason?: string;
  consent: { actor: string; workflowVersion: number; authorizationMode: string; recordedAt: string };
  lastRunAt?: string;
  nextRunAt?: string;
  readinessBlock?: string;
  holdResolutions?: { key: string; reason: string; round: number; resolvedAt: string }[];
  holds?: { openedAt: string; key: string; url?: string; message: string; reason: string; round: number; eligibility?: string; publication?: string; publicationMessage?: string }[];
  currentTask?: string;
  currentIssue?: TriggerIssue;
  skipped?: { key: string; title?: string; reason: string }[];
  notes?: string[];
  lastError?: string;
}

export interface TriggerMatches {
  next?: TriggerIssue;
  waiting: TriggerIssue[];
  skipped: { key: string; title?: string; reason: string }[];
  busy?: string;
  error?: string;
}

/** A server-held SCM connection (never carries its token). */
export interface SCMConnection {
  name: string;
  kind: 'github' | 'bitbucket-dc' | string;
  repository: string;
  apiUrl?: string;
  gitUrl?: string;
  branchPrefix?: string;
  requirePullRequestApproval?: boolean;
  tokenConfigured: boolean;
  updatedAt?: string;
  updatedBy?: string;
}

export interface SCMConnectionInput {
  name?: string;
  kind: string;
  repository: string;
  apiUrl?: string;
  gitUrl?: string;
  branchPrefix?: string;
  requirePullRequestApproval?: boolean;
  token?: string;
}

/** A model price in micro-units of the catalogue currency per million tokens. */
export interface Price {
  provider?: string;
  type?: string;
  model: string;
  inputMicrosPerMTok: number;
  outputMicrosPerMTok: number;
  cacheReadMicrosPerMTok?: number;
  cacheWriteMicrosPerMTok?: number;
}

export interface PriceCatalogue {
  version: number;
  currency: string;
  unpriced: 'allow' | 'refuse';
  prices: Price[];
  updatedAt?: string;
  updatedBy?: string;
}

export interface CostPolicy {
  taskRunBudgetMicros: number;
  warningPercent: number;
  updatedAt?: string;
  updatedBy?: string;
}

export type BudgetScope = 'run' | 'installation' | 'user' | 'project' | 'workflow';

export interface BudgetDefinition {
  scope: Exclude<BudgetScope, 'run'>;
  id: string;
  limitMicros: number;
  warningPercent?: number;
  interruptPercent?: number;
}

export interface BudgetView {
  scope: BudgetScope;
  id: string;
  currency?: string;
  limitMicros: number;
  chargedMicros: number;
  estimatedMicros: number;
  reservedMicros: number;
  remainingMicros: number;
  reached: boolean;
  paused: boolean;
  raisedBy?: string;
}

export interface SpendLine {
  key: string;
  model?: string;
  calls: number;
  inputTokens: number;
  outputTokens: number;
  costMicros: number;
  estimatedMicros: number;
  unpricedCalls: number;
}

export interface SpendReport {
  period: string;
  currency: string;
  total: SpendLine;
  byProvider: SpendLine[];
  byTask: SpendLine[];
  byUser: SpendLine[];
  byProject: SpendLine[];
  byWorkflow: SpendLine[];
  truncated?: boolean;
}

/** An installed inference plugin (provider type) and the providers built from it. */
export interface ProviderPlugin {
  id: string;
  label: string;
  description?: string;
  builtin: boolean;
  local: boolean;
  defaultBaseUrl?: string;
  key: 'required' | 'optional' | 'none';
  capabilities: { tools: boolean; json: 'native' | 'instruction'; usage: boolean; modelListing: boolean; streaming: boolean };
  settings?: string[];
  devOnly?: boolean;
  enabled: boolean;
  /** "config": provided by server configuration and cannot be disabled here. */
  enabledBy?: 'config' | 'console';
  updatedAt?: string;
  updatedBy?: string;
  instances: string[];
}

export interface ProviderSettings {
  revision: number;
  default: string;
  configDefault?: string;
  secrets: { enabled: boolean; keySource: string; keyId?: string };
  types: ProviderTypeInfo[];
}

/** Credential change: omit to keep, {value} to replace, {clear: true} to remove. */
export type CredentialUpdate = { value: string } | { clear: true };

export interface ProviderInput {
  name?: string;
  type: string;
  baseUrl?: string;
  defaultModel?: string;
  timeoutSec?: number;
  maxRetries?: number;
  concurrency?: number;
  contextWindow?: number;
  modelGuidance?: '' | 'small';
  project?: string;
  region?: string;
  apiKey?: CredentialUpdate;
}

export interface ModelInfo {
  id: string;
  provider: string;
  contextSize?: number;
}

export interface ReadyChecks {
  storage: string;
  queue: string;
  /** Dedicated Docker daemon for agent sandboxes: "ok", "not configured", or the error. Does not affect readiness. */
  sandbox?: string;
}

export interface HealthStatus {
  status: string;
}

export interface ToolInfo {
  name: string;
  capability: string;
  description: string;
  schema: unknown;
}

export interface ValidationIssue {
  path: string;
  message: string;
}

export interface ValidationResult {
  valid: boolean;
  errors: ValidationIssue[] | null;
  warnings: ValidationIssue[] | null;
}

export interface GraphVersion {
  version: number;
  createdAt: string;
  author?: string;
}

export interface FileEntry {
  name: string;
  path: string;
  dir: boolean;
  size: number;
  modTime: string;
}

export type GraphAction = 'start' | 'stop' | 'pause' | 'resume' | 'kill';

// Well-known metadata keys (domain/message.go).
export const META = {
  graph: 'graph',
  channel: 'channel',
  runId: 'run_id',
  correlationId: 'correlation_id',
  causationId: 'causation_id',
  executionId: 'execution_id',
  hops: 'hops',
  createdAt: 'created_at',
  traceparent: 'traceparent',
  injectedBy: 'injected_by',
  replica: 'replica',
} as const;

export function grantName(g: ToolGrant | string): string {
  return typeof g === 'string' ? g : g.name;
}

export function traceIdFromParent(tp: unknown): string | undefined {
  // W3C traceparent: version-traceid-spanid-flags
  if (typeof tp !== 'string') return undefined;
  const parts = tp.split('-');
  return parts.length >= 4 ? parts[1] : undefined;
}

export interface EnvironmentProposal {
 id: string;
 graphId: string;
 graphVersion: number;
 state: 'analyzing' | 'awaiting_approval' | 'building' | 'ready' | 'failed' | 'rejected' | 'expired' | 'cancelled';
 error?: string;
 resume: boolean;
 analysis?: { environment: EnvironmentSpec; rationale: string; tools: string[] };
}

export interface EnvironmentCleanupItem {
  id: string;
  image: string;
  caches?: { path: string; volume: string }[];
  usedBy?: string[];
  eligible: boolean;
  removed: boolean;
  missing?: boolean;
  error?: string;
}

export interface EnvironmentCleanupResult {
  dryRun: boolean;
  generations: EnvironmentCleanupItem[];
}

export type EnvironmentNetworkMode = 'none' | 'bridge' | 'egress';
export interface InstallationNetwork {
  setupNetworks: EnvironmentNetworkMode[];
  runNetworks: EnvironmentNetworkMode[];
  egressHosts: string[] | null;
}
export interface NetworkPolicyView {
  revision: number;
  source: 'server' | 'installation';
  network: InstallationNetwork;
  updatedBy?: string;
  updatedAt?: string;
  usage?: { graph: string; state: string; hosts: string[]; blocked: boolean; reason?: string }[];
}

export interface ProxyRequest {
 time: string; host: string; outcome: 'allowed' | 'denied' | 'failed'; reason: string; durationMs: number;
}
export interface ProxyStatus {
 name: string; phase: string; state: string; requests: ProxyRequest[]; warning?: string; capturedAt: string;
}
export interface ProxyView { proxies: ProxyStatus[]; lastPreparation?: ProxyStatus; truncated: boolean }

export interface HistoryRetentionPolicy { days: number; maxEvents: number }
export interface HistoryRetentionView { policy: HistoryRetentionPolicy; revision: number; updatedAt?: string; updatedBy?: string }

/** A worktree checkpoint (NAT-07): an agent workspace recorded on a hidden Git ref. */
export interface Checkpoint {
  id: string;
  ref: string;
  commit: string;
  tree: string;
  nodeId: string;
  replica: number;
  sequence: number;
  executionId?: string;
  reason: 'before_edit' | 'before_restore' | string;
  tool?: string;
  createdAt: string;
}

export interface CheckpointRestore {
  restored: Checkpoint;
  saved: Checkpoint;
  written: number;
  removed: number;
}

export interface GitInspection {
  graphId: string;
  nodeId?: string;
  replica?: number;
  repository: boolean;
  directory: string;
  branch: string;
  content: string;
  attribution: string;
}

export interface HumanReview {
 id: string;
 graphId: string;
 nodeId: string;
 replica: number;
 request: { title: string; instructions: string; command?: string[]; checks?: string[]; externalUrl?: string };
 candidate: string;
 state: string;
 session: string;
 generation: number;
 revision: number;
 url: string;
 sessionExpires: string;
 expiresAt: string;
 method?: string;
 comment?: string;
 error?: string;
 decidedBy?: string;
}

export interface MCPTemplateRef { id: string; version: number }
export interface MCPTemplate extends MCPTemplateRef { parameters?: {name:string;label:string;default:string;options?:string[];inputType?:'url'|'text'}[]; authenticationModes?: string[]; name: string; service: string; endpoint: string; authentication: string; guidance: string; source: string; reviewedAt: string; validation: string }
export interface MCPOAuthConfig { registrationMode?: string; issuer: string; clientId: string; scopes: string[] }
export interface MCPOAuth extends MCPOAuthConfig { registrationStatus?: string; registrationDiagnostic?: {code: string; httpStatus?: number}; effectiveRegistrationMode?: string; status: string; expiresAt?: string; grantedScopes?: string[]; clientSecretConfigured: boolean; revocation?: string }
export interface MCPArgumentRule { tool: string; argument?: string; values?: string[]; arguments?: string[]; combinations?: string[][] }
export interface MCPAtlassianPolicy { products: ('jira'|'confluence')[]; cloudIds: string[] }
export interface MCPLocalProcess { command:string; args:string[]; directory:string }
export interface MCPConnection {
 localProcess?: MCPLocalProcess;
 environmentKeys?: string[];
 argumentPolicy?: MCPArgumentRule[];
 atlassianPolicy?: MCPAtlassianPolicy;
 transportProfile?: 'stateless-2026-07-28';
 credentialType?: 'bearer'|'basic'|'langsmith';
 metadataHeaders?: Record<string,string>;
 callsPerMinute?: number;
  oauth?: MCPOAuth;
  template?: MCPTemplateRef;
  templateParameters?: Record<string,string>;
  id: string; name: string; service: string; endpoint: string; enabled: boolean;
  revision: number; status: string; credentialConfigured: boolean; error?: string;
  toolsRevision: number; tools: {name: string; description?: string; inputSchema: unknown}[];
  lastTest?: string; updatedAt: string; updatedBy: string;
}
export interface MCPConnectionInput {
 localProcess?: MCPLocalProcess;
 processEnvironment?: Record<string,string>;
 argumentPolicy?: MCPArgumentRule[];
 atlassianPolicy?: MCPAtlassianPolicy;
 transportProfile?: 'stateless-2026-07-28';
 credentialType?: 'bearer'|'basic'|'langsmith';
 metadataHeaders?: Record<string,string>;
 callsPerMinute?: number;
  oauth?: MCPOAuthConfig;
  oauthClientSecret?: {value?: string; clear?: boolean};
  template?: MCPTemplateRef;
  templateParameters?: Record<string,string>;
  id: string; name: string; service: string; endpoint: string; enabled: boolean;
  credential?: {value?: string; username?: string; clear?: boolean};
}

export interface MCPHistoryEntry {seq:number;time:string;type:string;connection:string;graph:string;node:string;execution:string;tool:string;outcome:string;durationMs:number;approval?:string;summary:string}
export interface MCPHistoryPage {entries:MCPHistoryEntry[];nextBefore?:number;hasMore:boolean}
