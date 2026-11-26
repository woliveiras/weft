import type { Agent, AgentDefinition, AgentSummary } from './agents';
import type {TaskInput, TaskDraft, TaskPage, TaskReview, TaskProgress, Project} from './tasks';
import type { ChatList, ChatSession } from './chat';
import type { ExecutionTree, CancelScope, Cancellation } from './executionTree';
import type { ActivityQuery, ActivityView, HealthQuery, HealthView } from './activity';
import type { DelegationPolicy, DelegationPolicyView, DelegationProfile, DelegationProfileView } from './delegation';
import type { JiraCloudProfile, JiraCloudProfileInput, MCPHistoryPage, MCPConnection, MCPConnectionInput, MCPTemplate, IssueConnection, IssueConnectionInput, IssueTransitionDiscovery, IssueWorkflowMapping, IssueWorkflowMappingInput, LifecycleSync, LifecycleSyncPage, LifecycleSyncEvidence } from './types';
import type { Skill, SkillInput, InstructionSnapshot } from './instructions';
import type {
  Approval,
  HumanReview,
  HistoryRetentionPolicy,
  HistoryRetentionView,
  ProxyView,
  InstallationNetwork,
  NetworkPolicyView,
  EnvironmentView,
  EnvironmentCleanupResult,
  EnvironmentProposal,
  EnvironmentDiscovery,
  EnvironmentSelection,
  EnvironmentSpec,
  Execution,
  FileEntry,
  Graph,
  GraphAction,
  GraphStatus,
  GraphSummary,
  GraphVersion,
  HealthStatus,
  Info,
  WeftEvent,
  ModelInfo,
  Message,
  MessageRecord,
  OperatorMessage,
  ProviderInfo,
  ProviderInput,
  ProviderPlugin,
  ProviderSettings,
  ChatModelSettings,
  PriceCatalogue,
  SCMConnection,
  SCMKind,
  Trigger,
  TriggerInput, TriggerReadinessLimits,
  TriggerMatches,
  SCMConnectionInput,
  CostPolicy,
  BudgetDefinition,
  BudgetView,
  SpendReport,
  ProviderTestResult,
  ReadyChecks,
  ToolInfo,
  ValidationResult,
} from './types';

export const MOCK = import.meta.env.VITE_MOCK === '1';

const TOKEN_KEY = 'weftToken';

// A one-time `?token=` in the page URL (e.g. a login link printed by an
// operator script) is moved into storage and removed from the address bar
// and history right away, so it does not linger where it can leak.
(() => {
  const url = new URL(window.location.href);
  const t = url.searchParams.get('token');
  if (!t) return;
  localStorage.setItem(TOKEN_KEY, t);
  url.searchParams.delete('token');
  window.history.replaceState(null, '', url.pathname + url.search + url.hash);
})();

export function getToken(): string {
  return localStorage.getItem(TOKEN_KEY) ?? '';
}

export function setToken(t: string): void {
  if (t) localStorage.setItem(TOKEN_KEY, t);
  else localStorage.removeItem(TOKEN_KEY);
}

export function onStoredTokenChanged(listener: (token: string) => void) {
  function changed(event: StorageEvent) {
    if (event.storageArea === localStorage && (event.key === TOKEN_KEY || event.key === null)) {
      listener(getToken());
    }
  }
  window.addEventListener('storage', changed);
  return () => window.removeEventListener('storage', changed);
}

/** Error returned by the API: {"error", "code", "details"}. */
export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    public code?: string,
    public details?: unknown,
  ) {
    super(message);
  }
}

type UnauthorizedListener = () => void;
const unauthorizedListeners = new Set<UnauthorizedListener>();

/** Subscribe to 401 responses (the app shows the login dialog). */
export function onUnauthorized(fn: UnauthorizedListener): () => void {
  unauthorizedListeners.add(fn);
  return () => unauthorizedListeners.delete(fn);
}

interface RequestOptions {
  method?: string;
  body?: unknown;
  /** Send body verbatim with this content type (YAML). */
  raw?: { text: string; contentType: string };
  headers?: Record<string, string>;
  query?: Record<string, string | number | undefined | null>;
  /** HTTP statuses whose response bodies represent expected diagnostic results. */
  allowStatuses?: number[];
  /** Return response text instead of JSON. */
  text?: boolean;
}

function buildUrl(path: string, query?: RequestOptions['query']): string {
  const qs = new URLSearchParams();
  for (const [k, v] of Object.entries(query ?? {})) {
    if (v !== undefined && v !== null && v !== '') qs.set(k, String(v));
  }
  const s = qs.toString();
  return s ? `${path}?${s}` : path;
}

async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json', ...opts.headers };
  const token = getToken();
  if (token) headers.Authorization = `Bearer ${token}`;
  let body: BodyInit | undefined;
  if (opts.raw) {
    headers['Content-Type'] = opts.raw.contentType;
    body = opts.raw.text;
  } else if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json';
    body = JSON.stringify(opts.body);
  }
  const url = buildUrl(path, opts.query);
  const init: RequestInit = { method: opts.method ?? 'GET', headers, body };
  let res: Response;
  try {
    res = MOCK ? await (await import('../mock/server')).mockFetch(url, init) : await fetch(url, init);
  } catch (e) {
    throw new ApiError(0, `network error: ${(e as Error).message}`, 'network');
  }
  if (res.status === 401 && token === getToken()) {
    unauthorizedListeners.forEach((fn) => fn());
  }
  if (!res.ok && !opts.allowStatuses?.includes(res.status)) {
    let msg = `${res.status} ${res.statusText}`;
    let code: string | undefined;
    let details: unknown;
    try {
      const j = (await res.json()) as { error?: string; code?: string; details?: unknown };
      if (j.error) msg = j.error;
      code = j.code;
      details = j.details;
    } catch {
      /* body was not JSON */
    }
    throw new ApiError(res.status, msg, code, details);
  }
  if (res.status === 204) return undefined as T;
  if (opts.text) return (await res.text()) as T;
  const ct = res.headers.get('content-type') ?? '';
  if (!ct.includes('json')) return (await res.text()) as T;
  return (await res.json()) as T;
}

const enc = encodeURIComponent;
const V1 = '/api/v1';

/** Graph documents may be submitted as JSON objects or YAML text. */
export type GraphInput = Graph | { yaml: string };

function graphBody(g: GraphInput): Pick<RequestOptions, 'body' | 'raw'> {
  return 'yaml' in g ? { raw: { text: g.yaml, contentType: 'application/yaml' } } : { body: g };
}

export const api = {
  agents: () => request<AgentSummary[]>(`${V1}/agents`),
  agent: (id: string) => request<Agent>(`${V1}/agents/${enc(id)}`),
  publishAgent: (id: string, definition: AgentDefinition, version?: number) =>
    request<Agent>(version ? `${V1}/agents/${enc(id)}` : `${V1}/agents`, {
      method: version ? 'PUT' : 'POST', body: {id, definition}, headers: version ? {'If-Match': String(version)} : undefined,
    }),
  projects: () => request<Project[]>(`${V1}/projects`),
  saveProjectPreference: (id:string,revision:number,executionMode:string)=>request<Project>(`${V1}/projects/${enc(id)}/preferences`,{method:'PUT',headers:{'If-Match':String(revision)},body:{executionMode}}),
  createProject: (input:{name:string;path:string}) => request<Project>(`${V1}/projects`,{method:'POST',body:input}),
  tasks: (after?: string) => request<TaskPage>(`${V1}/tasks`, {query:{after,limit:50,order:'updated_desc'}}),
  task: (id: string) => request<TaskDraft>(`${V1}/tasks/${enc(id)}`),
  taskAuthorization: (id:string) => request<import('./tasks').TaskAuthorization>(`${V1}/tasks/${encodeURIComponent(id)}/authorization`),
  saveTaskAuthorization: (id:string,mode:import('./tasks').AuthorizationMode,scope:string,revision:number,remember=false,preferenceRevision=0) => request(`${V1}/tasks/${encodeURIComponent(id)}/authorization`,{method:'PUT',headers:{'If-Match':String(revision)},body:{mode,scope,confirm:true,remember,preferenceRevision}}),
  resolveTaskPrompt: (input:TaskInput,id?:string,revision?:number) => request<TaskDraft>(`${V1}/tasks/prompt`, {method:'POST',body:{request:input.request,graphId:input.graphId,id,revision,settings:{repositories:input.repositories,title:input.title,executionMode:input.executionMode,projectId:input.projectId,baseCommit:input.baseCommit,source:input.source}}}),
  submitTaskPrompt: (input:TaskInput,key:string,id?:string,revision?:number) => request<{task:TaskDraft;review?:TaskReview}>(`${V1}/tasks/submit`, {method:'POST',body:{key,confirm:true,request:input.request,graphId:input.graphId,id,revision,settings:{repositories:input.repositories,title:input.title,executionMode:input.executionMode,projectId:input.projectId,baseCommit:input.baseCommit,source:input.source}}}),
  repositoryImports: () => request<import('./tasks').RepositoryImportRecord[]>(`${V1}/repository-imports`),
  importRepository: (input:{url:string;ref:string;confirmed:boolean;retry?:boolean;revision?:number;authentication?:{mode:string;username?:string;token?:string}}) => request<import('./tasks').RepositoryImportRecord>(`${V1}/repository-imports`, {method:'POST',body:input}),
  cancelRepositoryImport: (id:string,revision:number) => request<import('./tasks').RepositoryImportRecord>(`${V1}/repository-imports/${enc(id)}/cancel`, {method:'POST',headers:{'If-Match':String(revision)}}),
  deleteTask: (id:string,revision:number) => request<void>(`${V1}/tasks/${enc(id)}`,{method:'DELETE',headers:{'If-Match':String(revision)},body:{confirm:true}}),
  saveTask: (input: TaskInput, id?: string, revision?: number) => request<TaskDraft>(`${V1}/tasks${id ? '/'+enc(id) : ''}`, {method:id?'PUT':'POST',body:input,headers:id?{'If-Match':String(revision)}:undefined}),
  taskProgress: (id: string) => request<TaskProgress>(`${V1}/tasks/${enc(id)}/progress`),
  startTask: (id: string, revision: number) => request<TaskDraft>(`${V1}/tasks/${enc(id)}/start`, {method:'POST',body:{confirm:true},headers:{'If-Match':String(revision)}}),
  taskRepositoryStates: (id:string)=>request<Record<string,{state:string;head:string}>>(`${V1}/tasks/${enc(id)}/repository-states`),
  retryRepositoryDelivery: (id:string,revision:number,alias:string)=>request<{delivery:string}>(`${V1}/tasks/${enc(id)}/repositories/${enc(alias)}/deliver`,{method:'POST',headers:{'If-Match':String(revision)},body:{confirm:true}}),
  previewTaskCleanup: (id:string,node='',replica=0,repository='') => request<import('./tasks').CleanupPreview>(`${V1}/tasks/${enc(id)}/cleanup?node=${enc(node)}&replica=${replica}${repository?'&repository='+enc(repository):''}`),
  cleanupTaskWorkspace: (id:string,node:string,replica:number,fingerprint:string,repository='') => request<{removed:boolean}>(`${V1}/tasks/${enc(id)}/cleanup?node=${enc(node)}&replica=${replica}${repository?'&repository='+enc(repository):''}`,{method:'POST',body:{confirm:true,fingerprint}}),
  prepareTaskEnvironment: (id:string,revision:number)=>request<TaskDraft>(`${V1}/tasks/${enc(id)}/prepare-environment`,{method:'POST',headers:{'If-Match':String(revision)},body:{confirm:true}}),
  retryTask: (id: string, revision: number) => request<TaskDraft>(`${V1}/tasks/${enc(id)}/retry`, {method:'POST',body:{confirm:true},headers:{'If-Match':String(revision)}}),
  cancelTask: (id: string) => request<TaskDraft>(`${V1}/tasks/${enc(id)}/cancel`, {method:'POST',body:{confirm:true}}),
  reviewTask: (id: string) => request<TaskReview>(`${V1}/tasks/${enc(id)}/review`),
  delegationPolicy: () => request<DelegationPolicyView>(`${V1}/delegation-policy`),
  saveDelegationPolicy: (policy: DelegationPolicy, revision: number) => request<DelegationPolicyView>(`${V1}/delegation-policy`, {method:'PUT', body:policy, headers:{'If-Match':`"${revision}"`}}),
  delegationProfiles: () => request<DelegationProfileView[]>(`${V1}/delegation-profiles`),
  saveDelegationProfile: (profile: DelegationProfile, revision?: number) => request<DelegationProfileView>(`${V1}/delegation-profiles${revision === undefined ? '' : '/'+enc(profile.id)}`, {method:revision === undefined ? 'POST' : 'PUT', body:profile, headers:revision === undefined ? {} : {'If-Match':`"${revision}"`}}),
  mcpOAuthConfig: () => request<{callbackUrl:string}>(`${V1}/mcp/oauth/config`),
  previewMCPOAuth: (id: string) => request<{mode:string;reason:string}>(`${V1}/mcp/connections/${enc(id)}/oauth/preview`),
  recoverMCPRegistration: (id: string, expectRevision: number) => request<MCPConnection>(`${V1}/mcp/connections/${enc(id)}/oauth/registration/recover`, {method:'POST', body:{expectRevision,acknowledgeDuplicateClientRisk:true}}),
  startMCPOAuth: (id: string, mode?: string) => request<{url: string; state: string}>(`${V1}/mcp/connections/${enc(id)}/oauth/start${mode?`?registrationMode=${enc(mode)}`:""}`, {method:'POST'}),
  completeMCPOAuth: (state: string) => request<MCPConnection>(`${V1}/mcp/oauth/complete`, {method:'POST', body:{state}}),
  revokeMCPOAuth: (id: string) => request<MCPConnection>(`${V1}/mcp/connections/${enc(id)}/oauth/revoke`, {method:'POST'}),
  mcpTemplates: () => request<MCPTemplate[]>(`${V1}/mcp/templates`),
  deleteMCP: (id: string, revision: number) => request<void>(`${V1}/mcp/connections/${enc(id)}`, {method:'DELETE', headers:{'If-Match':String(revision)}}),
  mcpHistory: (query: string) => request<MCPHistoryPage>(`${V1}/mcp/history${query ? `?${query}` : ""}`),
  mcpConnections: () => request<MCPConnection[]>(`${V1}/mcp/connections`),
  mcpTools: () => request<ToolInfo[]>(`${V1}/mcp/tools`),
  saveMCP: (input: MCPConnectionInput, revision?: number) => request<MCPConnection>(`${V1}/mcp/connections${revision ? '/'+enc(input.id) : ''}`, {method: revision ? 'PUT' : 'POST', headers: revision ? {'If-Match': String(revision)} : undefined, body:input}),
  refreshMCP: (id: string, action: 'test' | 'tools/refresh') => request<MCPConnection>(`${V1}/mcp/connections/${enc(id)}/${action}`, {method:'POST'}),
  skills: () => request<Skill[]>(`${V1}/skills`),
  saveSkill: (input: SkillInput, revision?: number) => request<Skill>(`${V1}/skills${revision ? '/' + enc(input.id) : ''}`, {method: revision ? 'PUT' : 'POST', body: input, headers: revision ? {'If-Match': String(revision)} : undefined}),
  deleteSkill: (id: string, revision: number) => request<Skill>(`${V1}/skills/${enc(id)}`, {method: 'DELETE', headers: {'If-Match': String(revision)}}),
  instructions: (id: string, node: string) => request<InstructionSnapshot>(`${V1}/graphs/${enc(id)}/instructions`, {query: {node}}),
  executionInstructions: (id: string) => request<InstructionSnapshot>(`${V1}/executions/${enc(id)}/instructions`),

  reviewRepositoryDiff:(id:string,alias:string)=>request<{diff:string}>(`${V1}/reviews/${enc(id)}/repositories/${enc(alias)}/diff`),
  reviews: () => request<HumanReview[]>(`${V1}/reviews`),
  reviewAction: (id: string, action: string, body: Record<string, unknown> = {}) => request<HumanReview>(`${V1}/reviews/${enc(id)}/${enc(action)}`, { method: 'POST', body }),
  previewCredential: (id: string, browser: boolean) => request<{token: string; url: string}>(`${V1}/reviews/${enc(id)}/credential`, { method: 'POST', body: {browser} }),
  previewLogs: (id: string) => request<{logs: string}>(`${V1}/reviews/${enc(id)}/logs`, {method: 'POST', body: {}}),
  environmentBlockedHistory: (id: string) => request<{ requests: (import("./types").ProxyRequest & { proxy: string; phase: string })[]; retentionDays: number; limit: number; collectedAt?: string }>(`${V1}/graphs/${encodeURIComponent(id)}/environment/blocked-history`),
  environmentProxies: (id: string) => request<ProxyView>(`${V1}/graphs/${encodeURIComponent(id)}/environment/proxies`),
  historyRetention: () => request<HistoryRetentionView>(`${V1}/blocked-history-retention`),
  saveHistoryRetention: (policy: HistoryRetentionPolicy, revision: number) => request<HistoryRetentionView>(`${V1}/blocked-history-retention`, { method: 'PUT', body: { policy, revision, confirm: true } }),
  networkPolicy: () => request<NetworkPolicyView>(`${V1}/network-policy`),
  previewNetworkPolicy: (network: InstallationNetwork, revision: number) => request<NetworkPolicyView>(`${V1}/network-policy/preview`, { method: 'POST', body: { network, revision } }),
  saveNetworkPolicy: (network: InstallationNetwork, revision: number) => request<NetworkPolicyView>(`${V1}/network-policy`, { method: 'PUT', body: { network, revision } }),
  info: () => request<Info>(`${V1}/info`),
  health: () => request<HealthStatus>('/healthz'),
  ready: () => request<ReadyChecks>('/readyz', { allowStatuses: [503] }),

  listGraphs: () => request<GraphSummary[]>(`${V1}/graphs`),
  getGraph: (id: string, version?: number) =>
    request<GraphSummary>(`${V1}/graphs/${enc(id)}`, { query: { version } }),
  createGraph: (g: GraphInput) => request<GraphSummary>(`${V1}/graphs`, { method: 'POST', ...graphBody(g) }),
  updateGraph: (id: string, g: GraphInput, ifMatch?: number) =>
    request<GraphSummary>(`${V1}/graphs/${enc(id)}`, {
      method: 'PUT',
      ...graphBody(g),
      headers: ifMatch ? { 'If-Match': String(ifMatch) } : undefined,
    }),
  deleteGraph: (id: string, force = false, reason?: string) => request<void>(`${V1}/graphs/${enc(id)}`, {
    method: 'DELETE', query: force ? { force: 'true' } : undefined, body: force && reason ? { reason } : undefined,
  }),
  validateGraph: (g: GraphInput) =>
    request<ValidationResult>(`${V1}/graphs/validate`, { method: 'POST', ...graphBody(g) }),
  graphVersions: (id: string) => request<GraphVersion[]>(`${V1}/graphs/${enc(id)}/versions`),
  exportInstallation: () => request<string>(`${V1}/config/export`, {text:true}),
  exportGraph: (id: string, format: 'yaml' | 'json' = 'yaml') =>
    request<string>(`${V1}/graphs/${enc(id)}/export`, { query: { format }, text: true }),
  exportUrl: (id: string, format: 'yaml' | 'json' = 'yaml') => `${V1}/graphs/${enc(id)}/export?format=${format}`,

  graphAction: (id: string, action: GraphAction) =>
    request<GraphStatus>(`${V1}/graphs/${enc(id)}/${action}`, { method: 'POST' }),
  status: (id: string) => request<GraphStatus>(`${V1}/graphs/${enc(id)}/status`),
  previewEnvironmentCleanup: () => request<EnvironmentCleanupResult>(`${V1}/environments/cleanup`, { method: 'POST', body: { dryRun: true } }),
  abandonDeferredEnvironmentCleanup: (graph: string, reason: string) => request<void>(`${V1}/environments/cleanup`, { method: 'POST', body: { dryRun: false, abandonDeferred: { graph, reason } } }),
  retryDeferredEnvironmentCleanup: (graph: string) => request<void>(`${V1}/environments/cleanup`, { method: 'POST', body: { dryRun: false, deferredGraphs: [graph] } }),
  cleanupEnvironments: (generations: string[]) => request<EnvironmentCleanupResult>(`${V1}/environments/cleanup`, { method: 'POST', body: { dryRun: false, generations } }),
  environmentProposal: (id: string) => request<EnvironmentProposal | null>(`${V1}/graphs/${enc(id)}/environment/proposal`),
  proposeEnvironment: (id: string, resume: boolean) => request<EnvironmentProposal>(`${V1}/graphs/${enc(id)}/environment/proposal`, { method: 'POST', body: { resume } }),
  cancelEnvironmentProposal: (id: string) => request<EnvironmentProposal>(`${V1}/graphs/${enc(id)}/environment/proposal/cancel`, { method: 'POST' }),
  retryEnvironmentProposal: (id: string) => request<EnvironmentProposal>(`${V1}/graphs/${enc(id)}/environment/proposal/retry`, { method: 'POST' }),
  environment: (id: string) => request<EnvironmentView>(`${V1}/graphs/${enc(id)}/environment`),
  discoverEnvironment: (id: string) => request<EnvironmentDiscovery>(`${V1}/graphs/${enc(id)}/environment/discovery`),
  resolveEnvironment: (id: string, selection: EnvironmentSelection) =>
    request<EnvironmentSpec>(`${V1}/graphs/${enc(id)}/environment/resolve`, { method: 'POST', body: selection }),
  buildEnvironment: (id: string, force = false) =>
    request<EnvironmentView>(`${V1}/graphs/${enc(id)}/environment/build`, { method: 'POST', body: { force } }),
  resetEnvironment: (id: string) =>
    request<EnvironmentView>(`${V1}/graphs/${enc(id)}/environment/reset`, { method: 'POST' }),

  nodeAction: (id: string, node: string, action: GraphAction) =>
    request<GraphStatus>(`${V1}/graphs/${enc(id)}/nodes/${enc(node)}/${action}`, { method: 'POST' }),
  scaleNode: (id: string, node: string, replicas: number) =>
    request<GraphStatus>(`${V1}/graphs/${enc(id)}/nodes/${enc(node)}/scale`, {
      method: 'PUT',
      body: { replicas },
    }),
  chatHistory: (id: string, node: string) =>
    request<OperatorMessage[]>(`${V1}/graphs/${enc(id)}/nodes/${enc(node)}/chat`),
  sendChat: (id: string, node: string, text: string, replica?: number) =>
    request<OperatorMessage>(`${V1}/graphs/${enc(id)}/nodes/${enc(node)}/chat`, {
      method: 'POST',
      body: { text, replica: replica ?? -1 },
    }),

  injectMessage: (
    id: string,
    channel: string,
    msg: { type: string; payload: unknown; target?: string; metadata?: Record<string, unknown> },
  ) => request<Message>(`${V1}/graphs/${enc(id)}/channels/${enc(channel)}/messages`, { method: 'POST', body: msg }),
  messages: (
    id: string,
    f: { channel?: string; node?: string; correlationId?: string; status?: string; limit?: number } = {},
  ) => request<MessageRecord[]>(`${V1}/graphs/${enc(id)}/messages`, { query: f }),
  message: (messageId: string) => request<MessageRecord[]>(`${V1}/messages/${enc(messageId)}`),

  executions: (id: string, f: { node?: string; status?: string; limit?: number } = {}) =>
    request<Execution[]>(`${V1}/graphs/${enc(id)}/executions`, { query: f }),
  executionTree: (id:string) => request<ExecutionTree>(`${V1}/executions/${enc(id)}/tree`),
  cancelExecution: (id:string,scope:CancelScope) => request<{request?:Cancellation;alreadyTerminal:boolean}>(`${V1}/executions/${enc(id)}/cancel`, {method:'POST',body:{scope,confirm:true}}),
  execution: (execId: string) => request<Execution>(`${V1}/executions/${enc(execId)}`),

  events: (id: string, f: { after?: number; node?: string; types?: string; limit?: number } = {}) =>
    request<WeftEvent[]>(`${V1}/graphs/${enc(id)}/events`, { query: f }),

  activity: (q: ActivityQuery = {}) => request<ActivityView>(`${V1}/activity`, { query: { ...q } }),
  activityHealth: (q: HealthQuery = {}) => request<HealthView>(`${V1}/activity/health`, { query: { ...q } }),
  approvals: (f: { graph?: string; status?: string } = {}) => request<Approval[]>(`${V1}/approvals`, { query: f }),
  decide: (approvalId: string, decision: 'approve' | 'reject', body: { comment?: string; answer?: string }) =>
    request<Approval>(`${V1}/approvals/${enc(approvalId)}/${decision}`, { method: 'POST', body }),

  providers: () => request<ProviderInfo[]>(`${V1}/providers`),
  models: (provider: string) => request<ModelInfo[]>(`${V1}/providers/${enc(provider)}/models`),
  providerSettings: () => request<ProviderSettings>(`${V1}/provider-settings`),
  chatModelSettings: () => request<ChatModelSettings>(`${V1}/chat/settings`),
  chatSessions: (archived: boolean, after?: string) =>
    request<ChatList>(`${V1}/chat/sessions`, {query:{archived:String(archived),after}}),
  chatSession: (id: string) => request<ChatSession>(`${V1}/chat/sessions/${enc(id)}`),
  sendConversationMessage: (id: string | undefined, requestId: string, text: string) =>
    request<ChatSession>(id ? `${V1}/chat/sessions/${enc(id)}/messages` : `${V1}/chat/sessions`, {method:'POST', body:{requestId,text}}),
  retryChat: (id: string, message: string) =>
    request<ChatSession>(`${V1}/chat/sessions/${enc(id)}/messages/${enc(message)}/retry`, {method:'POST'}),
  renameChat: (id:string, previousTitle:string, title:string) =>
    request<ChatSession>(`${V1}/chat/sessions/${enc(id)}/title`, {method:'PUT',body:{previousTitle,title}}),
  chatPRDiff: (id: string, operation: string) =>
    request<{head: string; base: string; diff: string}>(`${V1}/chat/sessions/${enc(id)}/operations/${enc(operation)}/pr-diff`),
  prepareChatPRComments: (id: string, operation: string, commentIds: number[]) =>
    request<ChatSession>(`${V1}/chat/sessions/${enc(id)}/operations/${enc(operation)}/pr-comment-review`, {method:'POST',body:{commentIds}}),
  reconcileChatPRComments: (id: string, operation: string) =>
    request<ChatSession>(`${V1}/chat/sessions/${enc(id)}/operations/${enc(operation)}/pr-comment-receipts`, {method:'POST'}),
  decideChatReview: (id: string, operation: string, digest: string, decision: 'confirm' | 'decline') =>
    request<ChatSession>(`${V1}/chat/sessions/${enc(id)}/operations/${enc(operation)}/${decision}`, {method:'POST',body:{digest}}),
  archiveChat: (id: string, archived: boolean) =>
    request<ChatSession>(`${V1}/chat/sessions/${enc(id)}/${archived?'archive':'restore'}`, {method:'POST'}),
  readChatResult: (id: string, message: string) =>
    request<ChatSession>(`${V1}/chat/sessions/${enc(id)}/messages/${enc(message)}/read`, {method:'POST'}),
  saveChatModel: (model: NonNullable<ChatModelSettings['model']>, revision: number) =>
    request<ChatModelSettings>(`${V1}/chat/settings`, {
      method: 'PUT', body: model, headers: { 'If-Match': String(revision) },
    }),
  providerPlugins: () => request<ProviderPlugin[]>(`${V1}/provider-plugins`),
  triggers: (workflow: string) => request<Trigger[]>(`${V1}/triggers`, { query: { workflow } }),
  updateTrigger: (id: string, input: TriggerInput) => request<Trigger>(`${V1}/triggers/${enc(id)}`, { method: 'PUT', body: input }),
  triggerPolicy: () => request<TriggerReadinessLimits>(`${V1}/trigger-policy`),
  createTrigger: (input: TriggerInput) => request<Trigger>(`${V1}/triggers`, { method: 'POST', body: input }),
  triggerAction: (id: string, action: 'pause' | 'resume') => request<Trigger>(`${V1}/triggers/${enc(id)}/${action}`, { method: 'POST' }),
  deleteTrigger: (id: string) => request<void>(`${V1}/triggers/${enc(id)}`, { method: 'DELETE' }),
  resolveTriggerHold: (id: string, key: string, generation: string, force: boolean) => request<Trigger>(`${V1}/triggers/${enc(id)}/resolve-hold`, { method: 'POST', body: { key, generation, force } }),
  triggerMatches: (id: string) => request<TriggerMatches>(`${V1}/triggers/${enc(id)}/matches`),
  scmKinds: () => request<SCMKind[]>(`${V1}/scm/kinds`),
  lifecycleSyncEvidence: (id:string) => request<LifecycleSyncEvidence>(`${V1}/lifecycle-syncs/${enc(id)}`),
  lifecycleSyncs: (connection:string, after?:string, kind?:string) => request<LifecycleSyncPage>(`${V1}/lifecycle-syncs`, {query:{connection,after,kind}}),
  reconcileLifecycleSync: (id:string,revision:number) => request<LifecycleSync>(`${V1}/lifecycle-syncs/${enc(id)}/reconcile`,{method:'POST',headers:{'If-Match':String(revision)},body:{confirm:true}}),
  retryLifecycleSync: (id:string,revision:number) => request<LifecycleSync>(`${V1}/lifecycle-syncs/${enc(id)}/retry`,{method:'POST',headers:{'If-Match':String(revision)},body:{confirm:true}}),
  jiraCloudProfiles: () => request<JiraCloudProfile[]>(`${V1}/jira-cloud-profiles`),
  jiraCloudProfile: (name:string) => request<JiraCloudProfile>(`${V1}/jira-cloud-profiles/${enc(name)}`),
  previewJiraCloudProfile: (input:JiraCloudProfileInput) => request<JiraCloudProfile>(`${V1}/jira-cloud-profiles/preview`, {method:'POST',body:input}),
  saveJiraCloudProfile: (input:JiraCloudProfileInput, revision?:number) => request<JiraCloudProfile>(`${V1}/jira-cloud-profiles${revision===undefined?'':'/'+enc(input.name)}`, {method:revision===undefined?'POST':'PUT',body:input,headers:revision===undefined?undefined:{'If-Match':String(revision)}}),
  cloudWorkflowMappings: (connection:string) => request<IssueWorkflowMapping[]>(`${V1}/jira-cloud-workflow-mappings`, {query:{connection}}),
  cloudWorkflowMapping: (name:string) => request<IssueWorkflowMapping>(`${V1}/jira-cloud-workflow-mappings/${enc(name)}`),
  saveCloudWorkflowMapping: (input:IssueWorkflowMappingInput, revision?:number) => request<IssueWorkflowMapping>(`${V1}/jira-cloud-workflow-mappings${revision===undefined?'':'/'+enc(input.name)}`, {method:revision===undefined?'POST':'PUT',body:input,headers:revision===undefined?undefined:{'If-Match':String(revision)}}),
  discoverCloudTransitions: (name:string, revision:number, issueKey:string) => request<IssueTransitionDiscovery>(`${V1}/jira-cloud-profiles/${enc(name)}/transitions`, {method:'POST',body:{issueKey},headers:{'If-Match':String(revision)}}),
  issueWorkflowMappings: (connection: string) => request<IssueWorkflowMapping[]>(`${V1}/issue-workflow-mappings`, {query: {connection}}),
  issueWorkflowMapping: (name: string) => request<IssueWorkflowMapping>(`${V1}/issue-workflow-mappings/${enc(name)}`),
  saveIssueWorkflowMapping: (input: IssueWorkflowMappingInput, revision?: number) => request<IssueWorkflowMapping>(`${V1}/issue-workflow-mappings${revision === undefined ? '' : '/'+enc(input.name)}`, {method: revision === undefined ? 'POST' : 'PUT', body: input, headers: revision === undefined ? undefined : {'If-Match': String(revision)}}),
  discoverIssueTransitions: (name: string, revision: number, issueKey: string) => request<IssueTransitionDiscovery>(`${V1}/issue-connections/${enc(name)}/transitions`, {method: 'POST', headers: {'If-Match': String(revision)}, body: {issueKey}}),
  issueConnections: () => request<IssueConnection[]>(`${V1}/issue-connections`),
  issueConnection: (name: string) => request<IssueConnection>(`${V1}/issue-connections/${enc(name)}`),
  saveIssueConnection: (input: IssueConnectionInput, revision?: number) => request<IssueConnection>(`${V1}/issue-connections${revision === undefined ? '' : '/'+enc(input.name)}`, {
    method: revision === undefined ? 'POST' : 'PUT', body: input, headers: revision === undefined ? undefined : {'If-Match': String(revision)},
  }),
  testIssueConnection: (name: string, revision: number) => request<{ok: boolean; error?: string; identity?: {key: string; name: string}}>(`${V1}/issue-connections/${enc(name)}/test`, {method: 'POST', headers: {'If-Match': String(revision)}}),
  scmConnections: () => request<SCMConnection[]>(`${V1}/scm/connections`),
  createSCMConnection: (input: SCMConnectionInput) => request<SCMConnection>(`${V1}/scm/connections`, { method: 'POST', body: input }),
  deleteSCMConnection: (name: string) => request<void>(`${V1}/scm/connections/${enc(name)}`, { method: 'DELETE' }),
  testSCMConnection: (name: string) =>
    request<{ ok: boolean; error?: string; identity?: { user: string; repository: string; defaultBranch?: string; canPush: boolean } }>(`${V1}/scm/connections/${enc(name)}/test`, { method: 'POST' }),
  prices: () => request<PriceCatalogue>(`${V1}/cost/prices`),
  savePrices: (input: Pick<PriceCatalogue, 'currency' | 'unpriced' | 'prices'>) =>
    request<PriceCatalogue>(`${V1}/cost/prices`, { method: 'PUT', body: input }),
  costPolicy: () => request<CostPolicy>(`${V1}/cost/policy`),
  saveCostPolicy: (input: Pick<CostPolicy, 'taskRunBudgetMicros' | 'warningPercent'>) =>
    request<CostPolicy>(`${V1}/cost/policy`, { method: 'PUT', body: input }),
  budgetDefinitions: () => request<{ budgets: BudgetDefinition[]; period: string }>(`${V1}/cost/budgets`),
  saveBudgetDefinitions: (budgets: BudgetDefinition[]) =>
    request<{ budgets: BudgetDefinition[]; period: string }>(`${V1}/cost/budgets`, { method: 'PUT', body: { budgets } }),
  budgetLedgers: () => request<BudgetView[]>(`${V1}/cost/ledgers`),
  budget: (scope: string, id: string) => request<BudgetView>(`${V1}/cost/budgets/${enc(scope)}/${enc(id)}`),
  raiseBudget: (scope: string, id: string, limitMicros: number) =>
    request<BudgetView>(`${V1}/cost/budgets/${enc(scope)}/${enc(id)}/raise`, { method: 'POST', body: { limitMicros } }),
  budgetPreview: (workflow: string, project?: string, repositories?:TaskInput["repositories"]) =>
    request<{ taskRunBudgetMicros: number; currency: string; budgets: BudgetView[] }>(`${V1}/cost/budgets/preview`, { query: { workflow, project: project ?? '', ...(repositories?.length?{projects:repositories.map(r=>r.projectId).join(' ')}:{}) } }),
  spend: (graph?: string) => request<SpendReport>(`${V1}/cost/spend`, { query: graph ? { graph } : undefined }),
  setProviderPluginEnabled: (id: string, enabled: boolean, revision?: number) =>
    request<ProviderPlugin>(`${V1}/provider-plugins/${enc(id)}`, {
      method: 'PUT',
      body: { enabled },
      headers: revision ? { 'If-Match': String(revision) } : undefined,
    }),
  setDefaultProvider: (name: string, revision?: number) =>
    request<ProviderSettings>(`${V1}/provider-settings`, {
      method: 'PUT',
      body: { default: name },
      headers: revision ? { 'If-Match': String(revision) } : undefined,
    }),
  createProvider: (input: ProviderInput, revision?: number) =>
    request<ProviderInfo>(`${V1}/providers`, {
      method: 'POST',
      body: input,
      headers: revision ? { 'If-Match': String(revision) } : undefined,
    }),
  updateProvider: (name: string, input: ProviderInput, revision?: number) =>
    request<ProviderInfo>(`${V1}/providers/${enc(name)}`, {
      method: 'PUT',
      body: input,
      headers: revision ? { 'If-Match': String(revision) } : undefined,
    }),
  deleteProvider: (name: string, revision?: number) =>
    request<void>(`${V1}/providers/${enc(name)}`, {
      method: 'DELETE',
      headers: revision ? { 'If-Match': String(revision) } : undefined,
    }),
  /** Lists models and sends one tiny prompt through the gateway. */
  testProvider: (name: string) => request<ProviderTestResult>(`${V1}/providers/${enc(name)}/test`, { method: 'POST' }),
  /** Tests unsaved settings; a saved provider's stored credential is reused when none is given. */
  testProviderDraft: (input: ProviderInput) =>
    request<ProviderTestResult>(`${V1}/providers/test`, { method: 'POST', body: input }),
  tools: () => request<ToolInfo[]>(`${V1}/tools`),

  gitInspection: (id: string, view: 'status' | 'diff' | 'history', f: { repository?:string; path?: string; node?: string; replica?: number } = {}) =>
    request<import('./types').GitInspection>(`${V1}/graphs/${enc(id)}/git/${view}`, { query: f }),
  checkpoints: (id: string, repository?:string) =>
    request<import('./types').Checkpoint[]>(`${V1}/graphs/${enc(id)}/checkpoints`,{query:{repository}}),
  restoreCheckpoint: (id: string, checkpoint: string, repository?:string) =>
    request<import('./types').CheckpointRestore>(`${V1}/graphs/${enc(id)}/checkpoints/restore`, { method: 'POST', body: { checkpoint, confirm: true,repository } }),
  files: (id: string, f: { path?: string; node?: string; replica?: number } = {}) =>
    request<FileEntry[]>(`${V1}/graphs/${enc(id)}/files`, { query: f }),
  fileContent: (id: string, f: { path: string; node?: string; replica?: number }) =>
    request<string>(`${V1}/graphs/${enc(id)}/files/content`, { query: f, text: true }),

};

/** URL for the SSE stream; EventSource cannot send headers, so the token rides in the query. */
export function eventStreamUrl(params: { graph?: string; after?: number }): string {
  return buildUrl(`${V1}/events/stream`, {
    graph: params.graph,
    after: params.after,
    access_token: getToken() || undefined,
  });
}

export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  if (e instanceof Error) return e.message;
  return String(e);
}
