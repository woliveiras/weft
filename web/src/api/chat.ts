import type { AuthorizationMode, TaskAuthorization } from './tasks';

export interface ChatTask {
  repositories?:{alias?:string;project:string;base:string;preparation:string;delivery?:string;pullRequest?:number}[];
  otherRepositories?:number;
  retryOf?:string;
  id: string;
  state: string;
  graphId?: string;
  executionId?: string;
  decisionNeeded?: boolean;
  settledBlock?: boolean;
}

export interface ChatExecution {
  id: string; root: string; graph: string; state: string; nodes: number; active: number; cancellationRequested: boolean;
}

export interface ChatMessage {
  execution?: ChatExecution;
  id: string;
  role: 'user' | 'assistant';
  text: string;
  state?: 'pending' | 'interpreting' | 'interpreted' | 'unavailable' | 'clarification' | 'discovery' | 'grounded';
  attempt?: string;
  destination?: string;
  task?: ChatTask;
  review?: ChatOperationReview;
  unread?: boolean;
}

interface ChatReviewState {
  id: string;
  digest: string;
  expiresAt: string;
  state: 'queued' | 'consumed' | 'run_ended' | 'applied' | 'incomplete' | 'awaiting_confirmation' | 'applying' | 'created' | 'conflict' | 'unknown' | 'declined' | 'accepted';
  resultVersion?: number;
  resultId?: string;
}

interface ChatAgentReview extends ChatReviewState {
  workflow?: never;
  agentId: string;
  definition: {
    name: string;
    prompt: string;
    model: { provider: string; name: string };
    runtime: { kind: string };
    isolation?: { mode: string };
    readOnly: boolean;
  };
}

interface ChatWorkflowReview extends ChatReviewState {
  workflow: {
    graph: {
      metadata: {id: string; name: string};
      spec: {
        nodes: [{agent: {id: string; version: number}; id: string}];
        task: {nodeId: string; resultNodeId: string; channelId: string; messageType: string};
      };
    };
    agentName: string;
    runtime: string;
    readOnly: boolean;
    toolNames?: string[];
  };
}

export interface ChatTaskDraftReview extends ChatReviewState {
  taskDraft: {
    repositories?: {alias:string; project:string; commit:string; revision:number}[];
    input: {
      title: string;
      request: string;
      outcome: string;
      graphId: string;
      graphVersion: number;
      nodeId: string;
      resultNodeId: string;
      channelId?: string;
      messageType?: string;
    };
    workflowRevision: string;
  };
}

export interface ChatConfigurationReview extends ChatReviewState {
  configuration: {
    document: string;
    intakeConsent?: boolean;
    objects: {
      key: string; action: string; revision: string; consent?: string; warning?: string;
      changes: {path: string; before?: unknown; after?: unknown}[];
    }[];
  };
  configReceipts?: { key: string; result: string; revision?: string }[];
}

export interface ChatGuidanceReview extends ChatReviewState {
  guidance: {graph: string; version: number; run: string; node: string; replica: string; text: string};
  consumedAt?: string;
}

export interface ChatTaskStartReview extends ChatReviewState {
  taskStart: {
    id: string;
    title: string;
    workflowId: string;
    workflowVersion: number;
    taskRevision: number;
    consent: {scope: string; mode: string; revision: number};
    build?:{graph:string;hash:string;completed:string};
    repositories?:{project:string;alias?:string;commit:string;revision:number;checks?:string[]}[];
    issueSource?:string;
    agentGrants: {id: string; isolation: string; tools?: string[]; constraints?:string[]}[];
  };
}

interface ManagedSkillText {
  id: string; title: string; body: string; description: string; enabled: boolean; global: boolean;
}
export interface ChatSkillReview extends ChatReviewState {
  skill: {delete?: boolean; before?: ManagedSkillText; after: ManagedSkillText; revision: number};
}

export interface ChatPRReview extends ChatReviewState {
  pr: ChatPRAnalysis;
  prSummary?: string;
  prComments?: ChatPRDraftComment[];
}

interface ChatPRAnalysis {
  number: number; provider: string; connection: string; repository: string; head: string; base: string; draft?: boolean;
  graph: string; node: string; version: number; modelProvider: string; modelName: string;
}

export interface ChatPRDraftComment {
  id: number; path: string; side: 'LEFT' | 'RIGHT'; line: number; body: string;
}

export interface ChatPRPublicationReview extends ChatReviewState {
  prPublication: {analysis: ChatPRAnalysis; analysisId: string; executionId: string; comments: ChatPRDraftComment[]};
  prReceipts?: {state: 'published' | 'failed' | 'unknown'; id?: number; code?: string}[];
}

export interface ChatInstructionReview extends ChatReviewState {
  instructions: {graph: string; node: string; version: number; before: string; after: string; catalogId?: string; catalogVersion?: number};
}

export interface ChatWorkflowControlReview extends ChatReviewState {
  workflowControl: {
    action: 'start' | 'pause' | 'resume' | 'stop' | 'kill';
    scope: {
      workflowId: string;
      nodeId?: string;
      version: number;
      runId: string;
      desiredState: string;
      nodePaused?: boolean;
      nodeStopped?: boolean;
    };
  };
  controlResult?: {runId: string; desired: string; observed: string; settled: boolean};
}

export interface ChatEnvironmentReview extends ChatReviewState {
  environment:{task:string; revision:number; workflow:string; workflowVersion:number; workflowDigest:string; definitionHash:string; definition:string; repositories?:string[]};
}
export interface ChatWorkflowEnvironmentReview extends ChatReviewState {
  workflowEnvironment:{workflow:string;workflowVersion:number;workflowDigest:string;definitionHash:string;definition:string};
}
export interface ChatDraftText { title:string; request:string; outcome:string }
export interface ChatCorrectionReview extends ChatReviewState {
  taskCorrection:{task:string; revision:number; workflow:string; workflowVersion:number; workflowDigest:string; before:ChatDraftText; after:ChatDraftText};
}
export interface ChatTaskRetryReview extends ChatReviewState {
  taskRetry: {id: string; title: string; revision: number; workflowId: string; workflowVersion: number; repositoryCount: number};
}

export interface ChatCatalogPublicationReview extends ChatReviewState {
  publication: {id: string; name: string; previousVersion: number; revision: number; before: string; after: string};
}

export interface ChatAgentAdoptionReview extends ChatReviewState {
  adoption: {
    workflowId: string; nodeId: string; workflowVersion: number; agentId: string;
    currentVersion: number; targetVersion: number; before: string; after: string;
  };
}

export interface ChatApprovalReview extends ChatReviewState {
  approval: {
    id:string; title:string; graphId:string; taskId?:string; nodeId?:string; executionId?:string; replica:number;
    kind:'message'|'turn'|'question'; decision:'approve'|'reject'; answer?:string; comment?:string;
  };
}

export interface ChatTaskCancellationReview extends ChatReviewState {
  taskCancel: {
    id:string; title:string; revision:number; executionGraphId:string; executionVersion:number; runId:string; pendingApprovals:number;
  };
}

export interface ChatTaskAuthorizationReview extends ChatReviewState {
  taskAuthorization: {
    id:string; title:string; taskRevision:number; workflowId:string; workflowVersion:number; executionMode?:string;
    current:{mode:AuthorizationMode; scope:string; revision:number}; mode:AuthorizationMode;
    agents:NonNullable<TaskAuthorization['capabilities']>;
    repositories?:{alias?:string; projectId:string; baseCommit:string; scmConnection?:string; repository?:string; targetBranch?:string}[];
  };
}

export interface ChatBudgetRaiseReview extends ChatReviewState {
  budgetRaise: {
    target: {scope: string; id: string; period?: string; currency: string; limitMicros: number; revision: number;
      definitionsRevision: number; pausedGraphs?: string[]};
    subject: string; actor: string; limitMicros: number;
  };
  budgetResult?: {revision: number; reached: boolean; resumes?: {graphId: string; runId: string; state: string}[]};
}

export interface ChatWorkflowScaleReview extends ChatReviewState {
  workflowScale: {
    scope: ChatWorkflowControlReview['workflowControl']['scope'] & {nodeId: string; nodeDisabled?: boolean; autoscale: boolean; minimum: number; current: number};
    size: number;
  };
  scaleResult?: {version: number; activeReplicas: number; state: 'converging'|'saved'|'observed'|'failed'|'superseded'};
}

export interface ChatExecutionCancelReview extends ChatReviewState {
  executionCancel: {selected:string; root:string; graph:string; run:string; mode:"tree"|"child"};
}

export interface ChatTriggerReview extends ChatReviewState {
  triggerControl: {
    id: string; name: string; action: 'pause'|'resume'; actor: string;
    workflow: string; workflowVersion: number; node: string; resultNode?: string;
    sourceKind: string; connection: string; sourceProject: string; label: string; query?: string; owner?: string;
    project?: string; executionMode?: string; scm?: string; authorization: string;
    intervalMinutes: number; currentTask?: string; claiming?: string; operations?: string[];
  };
}

export type ChatOperationReview = ChatExecutionCancelReview | ChatTriggerReview | ChatWorkflowScaleReview | ChatBudgetRaiseReview | ChatEnvironmentReview | ChatWorkflowEnvironmentReview | ChatCorrectionReview | ChatSkillReview | ChatPRReview | ChatPRPublicationReview | ChatInstructionReview | ChatGuidanceReview | ChatAgentReview | ChatWorkflowReview |
  ChatConfigurationReview | ChatTaskDraftReview | ChatTaskStartReview | ChatWorkflowControlReview |
  ChatTaskRetryReview | ChatCatalogPublicationReview | ChatAgentAdoptionReview | ChatApprovalReview | ChatTaskCancellationReview |
  ChatTaskAuthorizationReview;

export interface ChatSession {
  id: string;
  title?: string;
  archived: boolean;
  titleRetryState?: 'pending' | 'completed' | 'failed';
  titleRetryStarted?: string;
  messages: ChatMessage[];
  watches?: { task: ChatTask; complete?: boolean; observedAt: string; unknown?: boolean; requestId: string }[];
}

export interface ChatSessionSummary {
  id: string;
  title: string;
  lastActivity: string;
  running: number;
  unread: number;
  unreadSuccess: number;
  unreadFailure: number;
  unreadCancellation: number;
  decisions: number;
  unknown: number;
}

export interface ChatList {
  items: ChatSessionSummary[];
  shared: boolean;
  archivedUnread: number;
  next?: string;
}
