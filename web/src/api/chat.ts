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
  review?: ChatAgentReview;
  unread?: boolean;
}

export interface ChatAgentReview {
  id: string;
  agentId: string;
  definition: {
    name: string;
    prompt: string;
    model: { provider: string; name: string };
    runtime: { kind: string };
    isolation?: { mode: string };
    readOnly: boolean;
  };
  digest: string;
  expiresAt: string;
  state: 'awaiting_confirmation' | 'applying' | 'created' | 'conflict' | 'unknown' | 'declined';
  resultVersion?: number;
}

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
