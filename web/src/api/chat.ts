export interface ChatTask {
  id: string;
  state: string;
  graphId?: string;
  executionId?: string;
  decisionNeeded?: boolean;
  settledBlock?: boolean;
}

export interface ChatMessage {
  id: string;
  role: 'user' | 'assistant';
  text: string;
  state?: 'pending' | 'interpreting' | 'interpreted' | 'unavailable' | 'clarification';
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
  state: 'queued' | 'consumed' | 'run_ended' | 'applied' | 'incomplete' | 'awaiting_confirmation' | 'applying' | 'created' | 'conflict' | 'unknown' | 'declined';
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

export type ChatOperationReview = ChatGuidanceReview | ChatAgentReview | ChatWorkflowReview | ChatConfigurationReview | ChatTaskDraftReview;

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
