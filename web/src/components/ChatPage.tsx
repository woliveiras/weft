import { ChatSkillCard } from './ChatSkillCard';
import { ChatPRCard } from './ChatPRCard';
import { takeChatReturnFocus } from './chatConnectionReturn';
import { ChatInstructionCard } from './ChatInstructionCard';
import { ChatGuidanceCard } from './ChatGuidanceCard';
import { ChatConfigurationCard } from './ChatConfigurationCard';
import { ChatManualLink } from './ChatManualLink';
import { useEffect, useRef, useState } from 'react';
import { api, ApiError, errorMessage, MOCK } from '../api/client';
import type { ChatAgentAdoptionReview, ChatCatalogPublicationReview, ChatMessage, ChatOperationReview,
  ChatSession, ChatSessionSummary, ChatTask, ChatTaskDraftReview, ChatTaskStartReview, ChatWorkflowControlReview,
  ChatTaskRetryReview } from '../api/chat';
import { useAsync } from '../hooks/useData';
import { Btn, ErrorBox, Modal } from './common';
import { taskSectionLink } from './taskDetailPresentation';
import './chat.css';

type OpenChat = (id?: string, archived?: boolean) => void;

const accessLost = (error: unknown) => error instanceof ApiError && [401,403,404].includes(error.status);

export function ChatPage({id, archived, canCreateAgent = false, canOperate = false, onOpen}: {
  id?: string; archived:boolean; canCreateAgent?:boolean; canOperate?:boolean; onOpen:OpenChat;
}) {
  const settings = useAsync(api.chatModelSettings, []);
  const [cursor, setCursor] = useState<{archived:boolean; after?:string}>({archived});
  const after = cursor.archived === archived ? cursor.after : undefined;
  const pageKey = `${archived}/${after ?? ''}`;
  const list = useAsync(async () => ({...await api.chatSessions(archived,after),pageKey}), [archived,after], accessLost);
  const visiblePage = list.data?.pageKey === pageKey ? list.data : undefined;
  const conversation = useAsync(() => id ? api.chatSession(id) : Promise.resolve(undefined), [id], accessLost);
  const session = conversation.data?.id === id ? conversation.data : undefined;
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string>();
  const [drawer, setDrawer] = useState(false);
  const [details, setDetails] = useState<string>();
  const [now, setNow] = useState(Date.now);
  const keys = useRef(new Map<string, {id:string; text:string}>());
  const heading = useRef<HTMLHeadingElement>(null);
  const drawerButton = useRef<HTMLButtonElement>(null);
  const drawerClose = useRef<HTMLButtonElement>(null);
  const evidence = useRef<string | undefined>(undefined);
  const requestedRead = useRef(new Set<string>());
  const draftKey = id ?? 'new';
  const draft = drafts[draftKey] ?? '';
  const modelAvailable = !MOCK && settings.data?.status === 'configured';
  const waiting = session?.messages.some(message => message.state === 'pending' || message.state === 'interpreting');
  const headingTitle = session ? session.title || 'New chat' : 'Chat';
  const titlePending = session?.titleRetryState === 'pending' && !!session.titleRetryStarted &&
    now - Date.parse(session.titleRetryStarted) < 60_000;
  const archivedUnread = list.data?.archivedUnread ?? 0;
  const archivedLabel = archivedUnread ? `Archived · ${archivedUnread} unread` : 'Archived';

  useEffect(() => {
    const timer = window.setInterval(() => {
      setNow(Date.now());
      void list.reload();
      if (id) void conversation.reload();
    }, 2000);
    return () => window.clearInterval(timer);
  }, [id, list.reload, conversation.reload]);

  useEffect(() => {
    const anchor = evidence.current || (session && takeChatReturnFocus(session.id));
    const focus = anchor && document.getElementById(anchor);
    if (focus instanceof HTMLElement) {
      evidence.current = undefined;
      focus.focus();
    }
  }, [id, session?.id]);
  useEffect(() => { if (drawer) drawerClose.current?.focus(); }, [drawer]);

  const closeDrawer = () => {
    setDrawer(false);
    drawerButton.current?.focus();
  };
  const navigate = (next?: string, archive = false) => {
    setDrawer(false);
    setDetails(undefined);
    setError(undefined);
    if (!next) setCursor({archived:archive});
    onOpen(next, archive);
    window.requestAnimationFrame(() => heading.current?.focus());
  };
  const newChat = () => {
    setDrafts(previous => ({...previous, new:''}));
    keys.current.delete('new');
    navigate();
  };

  const canSend = modelAvailable || (!MOCK && /^\s*\/inspect(?:\s|$)/.test(draft));

  async function send() {
    if (!canSend || !draft.trim() || sending || waiting) return;
    const previous = keys.current.get(draftKey);
    const requestId = previous?.text === draft ? previous.id
      : Array.from(crypto.getRandomValues(new Uint8Array(16)), value => value.toString(16).padStart(2,'0')).join('');
    keys.current.set(draftKey, {id:requestId,text:draft});
    await mutate(async () => {
      const next = await api.sendConversationMessage(id, requestId, draft);
      keys.current.delete(draftKey);
      setDrafts(previous => ({...previous, [draftKey]:''}));
      if (!id) onOpen(next.id);
      return next;
    });
  }

  async function mutate(action: () => Promise<ChatSession>) {
    setError(undefined);
    setSending(true);
    try {
      const session = await action();
      conversation.setData(session);
      void list.reload();
    } catch (error) {
      if (accessLost(error)) {
        conversation.setData(undefined);
        void list.reload();
      }
      setError(errorMessage(error));
    } finally {
      setSending(false);
    }
  }

  async function markRead(message: string) {
    if (!id || requestedRead.current.has(message)) return;
    requestedRead.current.add(message);
    try {
      conversation.setData(await api.readChatResult(id, message));
      void list.reload();
    } catch {
      setError('Unable to mark this result as read. Try again when connected.');
    } finally {
      requestedRead.current.delete(message);
    }
  }

  const sessionList = <>
    <Btn onClick={newChat}>New chat</Btn>
    <nav className="chat-list-tabs" aria-label="Session views">
      <button type="button" aria-current={!archived ? 'page' : undefined} onClick={() => navigate()}>Recents</button>
      <button type="button" aria-current={archived ? 'page' : undefined} onClick={() => navigate(undefined,true)}>
        {archivedLabel}
      </button>
    </nav>
    {list.loading && !visiblePage && <p role="status">Loading sessions…</p>}
    {list.error && <ErrorBox error={list.error}/>}
    {visiblePage?.items.length === 0 && <p className="muted">{archived ? 'No archived conversations on this page.' : 'No conversations on this page.'}</p>}
    <ul className="chat-sessions">
      {visiblePage?.items.map(item => <SessionRow key={item.id} item={item} current={item.id === id}
        expanded={details === item.id} onOpen={() => navigate(item.id,archived)} onDetails={() => setDetails(details === item.id ? undefined : item.id)}/>)}
    </ul>
    <div className="btn-row">
      {after && <Btn onClick={() => setCursor({archived})}>Newest conversations</Btn>}
      {visiblePage?.next && <Btn onClick={() => setCursor({archived,after:visiblePage.next})}>Older conversations</Btn>}
    </div>
  </>;

  return <div className="page chat-page">
    <section className="chat-conversation" aria-labelledby="chat-heading">
      <header className="page-head chat-header">
        <div>
          <h1 id="chat-heading" tabIndex={-1} ref={heading}>{headingTitle}</h1>
          {list.data?.shared && <p className="muted">Shared local history</p>}
          {session?.title === 'Untitled chat' && <>
            <p className="muted" role="status">{titlePending ? 'Generating a title…' : 'A title could not be generated.'}</p>
            {!session.titleRetryState && modelAvailable && !waiting && <Btn disabled={sending}
              onClick={() => void mutate(() => api.retryChatTitle(session.id))}>Retry title</Btn>}
          </>}
        </div>
        <button ref={drawerButton} className="btn chat-sessions-toggle" type="button"
          aria-expanded={drawer} onClick={() => setDrawer(!drawer)}>Sessions</button>
        {session && <Btn disabled={sending}
          onClick={() => void mutate(async () => {
            const next = await api.archiveChat(session.id,!session.archived);
            onOpen(next.id,next.archived);
            return next;
          })}>
          {session.archived ? 'Restore conversation' : 'Archive conversation'}
        </Btn>}
      </header>

      {error && <ErrorBox error={error}/>}
      {settings.error && <><ErrorBox error={settings.error}/><Btn onClick={() => void settings.reload()}>Check Chat model</Btn></>}
      {MOCK && <p className="card">Conversational requests require a running Weft server.</p>}
      {!MOCK && !modelAvailable && settings.data && <p className="card">
        {settings.data.status === 'unconfigured' ? 'No Chat model selected.' : 'The selected Chat model provider is unavailable.'}{' '}
        <a href="#/models/chat">Open Chat model settings</a>
        {' or '}<a href="#/tasks">open Tasks</a>. Use <code>/inspect</code> to see available read-only queries.
      </p>}
      {id && !session && conversation.loading && <p role="status">Loading conversation…</p>}
      {id && conversation.error && <><ErrorBox error={session ? conversation.error : 'Conversation unavailable.'}/>
        <Btn onClick={() => void conversation.reload()}>Retry loading conversation</Btn></>}
      {!id && !archived && <p className="muted">Ask to prepare a task, follow its progress, or create a reusable agent or workflow.</p>}
      {session?.watches?.filter(watch => !watch.complete).map(watch => <p className="chat-watch" key={watch.requestId}>
        {watch.unknown ? 'Status temporarily unknown' : watch.task.decisionNeeded ? 'Decision required' : `Following task: ${watch.task.state}`} · <a href={taskSectionLink(watch.task.id,'overview')}>{watch.task.id}</a>
        {watch.unknown && <span className="muted"> · Last seen <time dateTime={watch.observedAt}>{new Date(watch.observedAt).toLocaleString()}</time></span>}
      </p>)}
      <div className="chat-transcript" aria-label="Conversation">
        {session?.messages.map(message => <Message key={message.id} value={message}
          now={now} canCreateAgent={canCreateAgent && !sending} canOperate={canOperate && !sending}
          onDecide={(review, decision) => void mutate(() => api.decideChatReview(session.id, review.id, review.digest, decision))}
          onView={anchor => { evidence.current = anchor; }}
          onRead={() => void markRead(message.id)}
          onRetry={modelAvailable && !sending && canRetry(message,now) ? () => void mutate(() => api.retryChat(session.id,message.id)) : undefined}/>)}
      </div>
      {(!id || session) && !archived && <form className="chat-composer" onSubmit={event => { event.preventDefault(); void send(); }}>
        <label>Message<textarea value={draft} onChange={event => {
          const text = event.currentTarget.value;
          setDrafts(previous => ({...previous,[draftKey]:text}));
        }}
          rows={3} maxLength={4096} /></label>
        <Btn tone="primary" type="submit" disabled={!canSend || sending || waiting || !draft.trim()}>
          {sending ? 'Sending…' : 'Send'}
        </Btn>
      </form>}
    </section>

    <aside className="chat-panel" aria-label="Chat sessions">
      <section aria-label={archived ? 'Archived' : 'Recents'}>{sessionList}</section>
    </aside>
    {drawer && <Modal title="Sessions" onClose={closeDrawer} keyboard>
      <button ref={drawerClose} className="btn" type="button" onClick={closeDrawer}>Close sessions</button>
      {sessionList}
    </Modal>}
  </div>;
}

function canRetry(message: ChatMessage, now: number) {
  return message.state === 'unavailable' || message.state === 'pending' ||
    (message.state === 'interpreting' && !!message.attempt && now - Date.parse(message.attempt) > 60_000);
}

function SessionRow({item, current, expanded, onOpen, onDetails}: {
  item: ChatSessionSummary; current: boolean; expanded: boolean; onOpen: () => void; onDetails: () => void;
}) {
  const status = [
    item.running ? `${item.running} running` : '',
    item.unreadSuccess ? `${item.unreadSuccess} successful result unread` : '',
    item.unreadFailure ? `${item.unreadFailure} failed or blocked result unread` : '',
    item.unreadCancellation ? `${item.unreadCancellation} cancelled task unread` : '',
    item.decisions ? `${item.decisions} decision required` : '',
    item.unknown ? `${item.unknown} status temporarily unknown` : '',
  ].filter(Boolean).join(', ');
  const title = item.title || 'New chat';
  const lastActive = new Date(item.lastActivity).toLocaleString();
  const description = [!item.title ? 'Generating title' : item.title === 'Untitled chat' ? 'Title unavailable' : '', lastActive, status].filter(Boolean).join(' · ');
  return <li className="chat-session-row" onKeyDown={event => {
    if (event.key === 'Escape' && expanded) onDetails();
  }}>
    <div className="chat-session-line">
      <button type="button" className="chat-session-title" onClick={onOpen} aria-describedby={`chat-row-${item.id}`}
        aria-current={current?'page':undefined}>{title}</button>
      <span className="chat-session-indicators" role={status ? 'img' : undefined} aria-label={status || undefined}>
        {!!item.running && <span className="chat-running" aria-hidden="true"/>}
        {!!item.unreadSuccess && <span className="chat-success" aria-hidden="true"/>}
        {!!(item.unreadFailure || item.unreadCancellation || item.decisions || item.unknown) && <span className="chat-attention" aria-hidden="true">!</span>}
      </span>
      <button type="button" className="btn chat-details" aria-label={`Details for ${title}`} aria-expanded={expanded} onClick={onDetails}>⋯</button>
    </div>
    {expanded && <div className="chat-session-details">
      <strong>{title}</strong><br/>
      <time dateTime={item.lastActivity}>{lastActive}</time>
      {status && <div>{status}</div>}
    </div>}
    <span className="chat-session-description" id={`chat-row-${item.id}`}>{description}</span>
    {!expanded && <div className="chat-session-preview" aria-hidden="true" data-title={title} data-description={description}/>}
  </li>;
}

function Message({value, now, canCreateAgent, canOperate, onView, onRead, onRetry, onDecide}: {
  value:ChatMessage; now:number; canCreateAgent:boolean; canOperate:boolean; onView:(anchor:string)=>void; onRead:()=>void;
  onRetry?:()=>void; onDecide:(review:ChatOperationReview, decision:'confirm'|'decline')=>void;
}) {
  const panel = useRef<HTMLElement>(null);
  const review = value.review;
  const read = useRef(onRead);
  read.current = onRead;
  useEffect(() => {
    if (!value.unread || !panel.current || typeof IntersectionObserver === 'undefined') return;
    const observer = new IntersectionObserver(entries => {
      if (document.visibilityState === 'visible' && entries.some(entry => entry.isIntersecting)) read.current();
    }, {threshold:0.65});
    observer.observe(panel.current);
    return () => observer.disconnect();
  }, [value.unread]);

  return <article className={`chat-message chat-message-${value.role}`} ref={panel}>
    <span className="small muted">{value.role === 'user' ? 'You' : 'Weft'}</span>
    <p>{value.text}</p>
    {value.destination && <ChatManualLink destination={value.destination} message={value.id} onView={onView}/>}
    {(value.state === 'pending' || value.state === 'interpreting') && <p role="status">Interpreting request…</p>}
    {onRetry && <Btn onClick={onRetry}>Retry saved request</Btn>}
    {value.task && <TaskLink task={value.task} message={value.id} onView={onView}/>}
    {review && <OperationReviewCard review={review} now={now} canConfirm={('pr' in review || 'taskDraft' in review || 'taskRetry' in review || 'guidance' in review || 'workflowControl' in review) ? canOperate : canCreateAgent}
      onDecide={decision => onDecide(review,decision)} onView={onView}/>}
    {value.unread && <Btn onClick={onRead}>Mark result read</Btn>}
  </article>;
}

function OperationReviewCard({review, now, canConfirm, onDecide, onView}: {
  review:ChatOperationReview; now:number; canConfirm:boolean; onDecide:(decision:'confirm'|'decline')=>void; onView:(anchor:string)=>void;
}) {
  if ('skill' in review) return <ChatSkillCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  if ('pr' in review) return <ChatPRCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  if ('instructions' in review) return <ChatInstructionCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  if ('guidance' in review) return <ChatGuidanceCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide}/>;
  if ('configuration' in review) return <ChatConfigurationCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide}/>;
  if ('taskDraft' in review) return <TaskDraftReviewCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  if ('taskStart' in review) return <TaskStartReviewCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  if ('workflowControl' in review) return <WorkflowControlCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  if ('taskRetry' in review) return <TaskRetryReviewCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  if ('publication' in review) return <CatalogPublicationCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  if ('adoption' in review) return <AgentAdoptionCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  const workflow = review.workflow;
  const name = review.workflow ? review.workflow.graph.metadata.name : review.definition.name;
  const kind = workflow ? 'workflow' : 'agent';
  const anchor = `chat-${kind}-${review.id}`;
  const href = review.workflow ? `#/g/${encodeURIComponent(review.workflow.graph.metadata.id)}/canvas`
    : `#/agents/${encodeURIComponent(review.agentId)}`;
  return <section className="chat-agent-review" aria-label={`${workflow ? 'Workflow' : 'Catalog agent'} ${name}`}>
    <h3>{name}</h3>
    {review.workflow ? <dl>
      <dt>Workflow ID</dt><dd>{review.workflow.graph.metadata.id}</dd>
      <dt>Catalog agent</dt><dd>{review.workflow.agentName} · {review.workflow.graph.spec.nodes[0].agent.id}
        {' · Version '}{review.workflow.graph.spec.nodes[0].agent.version}</dd>
      <dt>Runtime</dt><dd>{review.workflow.runtime} · {review.workflow.readOnly ? 'Read-only workspace' : 'Writable workspace'}</dd>
      <dt>Inherited tool grants</dt><dd>{review.workflow.toolNames?.join(', ') || 'None'}</dd>
      <dt>Task input</dt><dd>{review.workflow.graph.spec.task.channelId} → {review.workflow.graph.spec.task.nodeId}
        {' · '}{review.workflow.graph.spec.task.messageType}</dd>
    </dl> : <>
      <dl>
        <dt>Agent ID</dt><dd>{review.agentId}</dd>
        <dt>Model</dt><dd>{review.definition.model.provider} · {review.definition.model.name}</dd>
        <dt>Runtime</dt><dd>{review.definition.runtime.kind} · {review.definition.isolation?.mode} · Read-only workspace</dd>
        <dt>Tool grants</dt><dd>None</dd>
      </dl>
      <h4>Instructions</h4>
      <p>{review.definition.prompt}</p>
    </>}
    {review.state === 'awaiting_confirmation' && <>
      <p>{workflow ? 'This saves a stopped workflow with one agent. Tasks are created and started separately.'
        : 'This creates a reusable catalog definition. No workflow starts.'}</p>
      <ReviewDecisionControls expiresAt={review.expiresAt} now={now} canConfirm={canConfirm}
        confirmLabel={`Create ${kind}`} onDecide={onDecide}/>
    </>}
    {review.state === 'applying' && <p role="status">Creation accepted. Checking the result…</p>}
    {review.state === 'conflict' && <p role="status">The reviewed configuration changed. Ask for a new review.</p>}
    {review.state === 'unknown' && <p role="status">Creation outcome is temporarily unknown.
      {' '}Check <a id={anchor} href={workflow ? '#/graphs' : '#/agents'} onClick={() => onView(anchor)}>
        {workflow ? 'Workflows' : 'Agents'}</a> before asking to try again.</p>}
    {review.state === 'declined' && <p role="status">Proposal declined.</p>}
    {review.state === 'created' && <>
      <p role="status">{workflow ? 'Workflow' : 'Catalog'} version {review.resultVersion} created.</p>
      <a id={anchor} href={href} onClick={() => onView(anchor)}>View {kind}</a>
    </>}
  </section>;
}

function TaskDraftReviewCard({review, now, canConfirm, onDecide, onView}: {
  review:ChatTaskDraftReview; now:number; canConfirm:boolean; onDecide:(decision:'confirm'|'decline')=>void; onView:(anchor:string)=>void;
}) {
  const {input} = review.taskDraft;
  const anchor = `chat-task-draft-${review.id}`;
  return <section className="chat-agent-review" aria-label={`Task draft ${input.title}`}>
    <h3>{input.title}</h3>
    <dl>
      <dt>Workflow</dt><dd>{input.graphId} · Version {input.graphVersion}</dd>
      <dt>Entry agent</dt><dd>{input.nodeId}</dd>
      <dt>Result agent</dt><dd>{input.resultNodeId}</dd>
      <dt>Input</dt><dd>{input.channelId ? `${input.channelId} · ${input.messageType}` : 'Direct agent request'}</dd>
      <dt>Outcome</dt><dd>{input.outcome}</dd>
    </dl>
    <h4>Request</h4>
    <p>{input.request}</p>
    {review.state === 'awaiting_confirmation' && <>
      <p>This saves a draft without repository access or execution authorization. Starting it requires a separate review.</p>
      <ReviewDecisionControls expiresAt={review.expiresAt} now={now} canConfirm={canConfirm}
        confirmLabel="Create task draft" onDecide={onDecide}/>
    </>}
    {review.state === 'applying' && <p role="status">Request accepted. Checking the saved draft…</p>}
    {review.state === 'conflict' && <p role="status">The reviewed workflow changed. Ask for a new task draft proposal.</p>}
    {review.state === 'unknown' && <p role="status">The draft outcome is temporarily unknown. Check <a id={anchor} href="#/tasks" onClick={() => onView(anchor)}>Tasks</a> before trying again.</p>}
    {review.state === 'declined' && <p role="status">Proposal declined.</p>}
    {review.state === 'created' && review.resultId && <>
      <p role="status">Task draft saved. Review it in Tasks before starting.</p>
      <a id={anchor} href={taskSectionLink(review.resultId,'overview')} onClick={() => onView(anchor)}>View task draft</a>
    </>}
  </section>;
}

function TaskStartReviewCard({review, now, canConfirm, onDecide, onView}: {
  review:ChatTaskStartReview; now:number; canConfirm:boolean; onDecide:(decision:'confirm'|'decline')=>void; onView:(anchor:string)=>void;
}) {
  const {taskStart:task} = review;
  const anchor = `chat-start-${review.id}`;
  return <section className="chat-agent-review" aria-label={`Start task ${task.title}`}>
    <h3>{task.title}</h3>
    <dl>
      <dt>Task ID</dt><dd>{task.id} · Revision {task.taskRevision}</dd>
      <dt>Workflow</dt><dd>{task.workflowId} · Version {task.workflowVersion}</dd>
      <dt>Authorization</dt><dd>{task.consent.mode === 'ask' ? 'Ask for approval as required' : task.consent.mode}
        {task.consent.revision === 0 ? ' · Default' : ` · Revision ${task.consent.revision}`}</dd>
      {task.agentGrants.map(agent => <div key={agent.id}>
        <dt>{agent.id}</dt><dd>{agent.isolation} · {agent.tools?.join(', ') || 'No tool grants'}</dd>
      </div>)}
    </dl>
    {review.state === 'awaiting_confirmation' && <>
      <p>This admits the task to run within the reviewed scope. Existing approvals still apply.</p>
      <ReviewDecisionControls expiresAt={review.expiresAt} now={now} canConfirm={canConfirm} confirmLabel="Start task" onDecide={onDecide}/>
    </>}
    {review.state === 'applying' && <p role="status">Start requested. Checking admission…</p>}
    {review.state === 'conflict' && <p role="status">The task is no longer ready under this review. Check its requirements before requesting a new start.</p>}
    {review.state === 'unknown' && <p role="status">Task admission is temporarily unknown. Inspect the saved task before trying again.</p>}
    {review.state === 'declined' && <p role="status">Proposal declined.</p>}
    {review.state === 'accepted' && <p role="status">Task admission recorded. Follow its state and result in this conversation.</p>}
    <a id={anchor} href={taskSectionLink(task.id,'overview')} onClick={() => onView(anchor)}>Open task</a>
  </section>;
}

const workflowControlCopy = {
  start: ['Start', 'Request execution in this scope. Starting a stopped workflow creates a new run.'],
  pause: ['Pause new turns', 'Pause new turns in this scope while active turns continue.'],
  resume: ['Resume', 'Allow new turns in this scope under the existing budget and runtime policy.'],
  stop: ['Drain and stop', 'Stop taking new work and let active turns drain. Pending reviews in this scope can be canceled.'],
  kill: ['Interrupt now', 'Interrupt active turns immediately in this scope. Pending reviews in this scope can be canceled.'],
};

function WorkflowControlCard({review, now, canConfirm, onDecide, onView}: {
  review:ChatWorkflowControlReview; now:number; canConfirm:boolean; onDecide:(decision:'confirm'|'decline')=>void; onView:(anchor:string)=>void;
}) {
  const {scope, action} = review.workflowControl;
  const [command, effect] = workflowControlCopy[action];
  const target = scope.nodeId ? `${scope.workflowId} / ${scope.nodeId}` : scope.workflowId;
  const anchor = `chat-control-${review.id}`;
  const selection = scope.nodeId ? `?sel=${encodeURIComponent('node:' + scope.nodeId)}` : '';
  const current = scope.nodeStopped ? 'stopped' : scope.nodePaused ? 'paused' : scope.desiredState;
  return <section className="chat-agent-review" aria-label={`${command} ${target}`}>
    <h3>{command} {scope.nodeId ? 'agent' : 'workflow'}</h3>
    <dl>
      <dt>Target</dt><dd>{target} · Version {scope.version}</dd>
      <dt>Current desired state</dt><dd>{current}</dd>
      {scope.runId && <><dt>Reviewed run</dt><dd>{scope.runId}</dd></>}
      {review.controlResult && <>
        <dt>Requested state</dt><dd>{review.controlResult.desired}</dd>
        <dt>Observed runtime state</dt><dd>{review.controlResult.observed}</dd>
        <dt>Run</dt><dd>{review.controlResult.runId}</dd>
      </>}
    </dl>
    {review.state === 'awaiting_confirmation' && <>
      <p>{effect} Trigger intake has separate controls.</p>
      <ReviewDecisionControls expiresAt={review.expiresAt} now={now} canConfirm={canConfirm} confirmLabel={command} onDecide={onDecide}/>
    </>}
    {review.state === 'applying' && <p role="status">Control requested. Checking the recorded response…</p>}
    {review.state === 'accepted' && <p role="status">Control recorded. Waiting for the runtime to reach the requested state.</p>}
    {review.state === 'applied' && <p role="status">The runtime reached the requested state.</p>}
    {review.state === 'conflict' && <p role="status">The reviewed workflow or runtime changed. Inspect the current state and request a fresh review.</p>}
    {review.state === 'incomplete' && <p role="status">The control was recorded. Inspect the workflow for unfinished work.</p>}
    {review.state === 'unknown' && <p role="status">The control outcome is temporarily unknown. Inspect the workflow before trying again.</p>}
    {review.state === 'declined' && <p role="status">Proposal declined.</p>}
    <a id={anchor} href={`#/g/${encodeURIComponent(scope.workflowId)}/canvas${selection}`} onClick={() => onView(anchor)}>Open workflow</a>
  </section>;
}

function TaskRetryReviewCard({review, now, canConfirm, onDecide, onView}: {
  review:ChatTaskRetryReview; now:number; canConfirm:boolean; onDecide:(decision:'confirm'|'decline')=>void; onView:(anchor:string)=>void;
}) {
  const {taskRetry:source} = review;
  const anchor = `chat-retry-${review.id}`;
  return <section className="chat-agent-review" aria-label={`Retry task ${source.title}`}>
    <h3>{source.title}</h3>
    <dl>
      <dt>Failed task</dt><dd>{source.id} · Revision {source.revision}</dd>
      <dt>Workflow</dt><dd>{source.workflowId} · Version {source.workflowVersion}</dd>
      <dt>Repositories copied</dt><dd>{source.repositoryCount}</dd>
    </dl>
    {review.state === 'awaiting_confirmation' && <>
      <p>Prepare one linked draft with the same saved input. A new start requires its own review and authorization.</p>
      <ReviewDecisionControls expiresAt={review.expiresAt} now={now} canConfirm={canConfirm} confirmLabel="Prepare retry draft" onDecide={onDecide}/>
    </>}
    {review.state === 'applying' && <p role="status">Retry requested. Checking the linked draft…</p>}
    {review.state === 'conflict' && <p role="status">The failed attempt changed or its linked draft is unavailable. Inspect the original task before requesting a new review.</p>}
    {review.state === 'unknown' && <p role="status">The retry outcome is temporarily unknown. Check the original task before trying again.</p>}
    {review.state === 'declined' && <p role="status">Proposal declined.</p>}
    {review.state === 'created' && review.resultId && <>
      <p role="status">The linked retry is available.</p>
      <a id={`${anchor}-result`} href={taskSectionLink(review.resultId,'overview')} onClick={() => onView(`${anchor}-result`)}>View retry</a>
    </>}
    <a id={anchor} href={taskSectionLink(source.id,'overview')} onClick={() => onView(anchor)}>View failed task</a>
  </section>;
}

function CatalogPublicationCard({review, now, canConfirm, onDecide, onView}: {
  review:ChatCatalogPublicationReview; now:number; canConfirm:boolean; onDecide:(decision:'confirm'|'decline')=>void; onView:(anchor:string)=>void;
}) {
  const {publication} = review;
  const anchor = `chat-publication-${review.id}`;
  return <section className="chat-agent-review" aria-label={`Publish shared instructions for ${publication.name}`}>
    <h3>{publication.name}</h3>
    <dl>
      <dt>Catalog agent</dt><dd>{publication.id}</dd>
      <dt>Current version</dt><dd>{publication.previousVersion}</dd>
    </dl>
    <h4>Current shared instructions</h4><p>{publication.before || 'None'}</p>
    <h4>Proposed shared instructions</h4><p>{publication.after}</p>
    {review.state === 'awaiting_confirmation' && <>
      <p>This publishes a new version in Agents. Each workflow adopts the version separately.</p>
      <ReviewDecisionControls expiresAt={review.expiresAt} now={now} canConfirm={canConfirm} confirmLabel="Publish agent version" onDecide={onDecide}/>
    </>}
    {review.state === 'applying' && <p role="status">Publication requested. Checking the catalog…</p>}
    {review.state === 'conflict' && <p role="status">The reviewed catalog changed. Inspect the current version before requesting a new review.</p>}
    {review.state === 'unknown' && <p role="status">The publication outcome is temporarily unknown. Inspect the catalog before trying again.</p>}
    {review.state === 'declined' && <p role="status">Proposal declined.</p>}
    {review.state === 'created' && <p role="status">Catalog version {review.resultVersion} published.</p>}
    <a id={anchor} href={`#/agents/${encodeURIComponent(publication.id)}`} onClick={() => onView(anchor)}>View agent</a>
  </section>;
}

function AgentAdoptionCard({review, now, canConfirm, onDecide, onView}: {
  review:ChatAgentAdoptionReview; now:number; canConfirm:boolean; onDecide:(decision:'confirm'|'decline')=>void; onView:(anchor:string)=>void;
}) {
  const {adoption:pinned} = review;
  const anchor = `chat-adoption-${review.id}`;
  return <section className="chat-agent-review" aria-label={`Adopt agent version for ${pinned.workflowId} / ${pinned.nodeId}`}>
    <h3>{pinned.agentId}</h3>
    <dl>
      <dt>Workflow node</dt><dd>{pinned.workflowId} / {pinned.nodeId} · Workflow version {pinned.workflowVersion}</dd>
      <dt>Catalog version</dt><dd>Version {pinned.currentVersion} → Version {pinned.targetVersion}</dd>
    </dl>
    <h4>Current shared instructions</h4><p>{pinned.before || 'None'}</p>
    <h4>New shared instructions</h4><p>{pinned.after || 'None'}</p>
    {review.state === 'awaiting_confirmation' && <>
      <p>This saves a workflow version with this node's new pin and preserves its local instructions and parameters. Subsequent turns use the saved workflow version.</p>
      <ReviewDecisionControls expiresAt={review.expiresAt} now={now} canConfirm={canConfirm} confirmLabel="Adopt agent version" onDecide={onDecide}/>
    </>}
    {review.state === 'applying' && <p role="status">Adoption requested. Checking the stored pin…</p>}
    {review.state === 'conflict' && <p role="status">The workflow or catalog binding changed. Inspect the node before requesting another review.</p>}
    {review.state === 'unknown' && <p role="status">The adoption outcome is temporarily unknown. Inspect the workflow before trying again.</p>}
    {review.state === 'declined' && <p role="status">Proposal declined.</p>}
    {review.state === 'created' && <p role="status">Workflow version {review.resultVersion} saved with catalog version {pinned.targetVersion}.</p>}
    <a id={anchor} href={`#/g/${encodeURIComponent(pinned.workflowId)}/canvas?sel=${encodeURIComponent('node:' + pinned.nodeId)}`}
      onClick={() => onView(anchor)}>Open selected node</a>
  </section>;
}

function ReviewDecisionControls({expiresAt, now, canConfirm, confirmLabel, onDecide}: {
  expiresAt:string; now:number; canConfirm:boolean; confirmLabel:string; onDecide:(decision:'confirm'|'decline')=>void;
}) {
  if (now >= Date.parse(expiresAt)) return <p role="status">Confirmation expired. Ask for a new review.</p>;
  return <>
    <p>Review expires at <time dateTime={expiresAt}>{new Date(expiresAt).toLocaleTimeString()}</time>.</p>
    <div className="btn-row">
      <Btn tone="primary" disabled={!canConfirm} onClick={() => onDecide('confirm')}>{confirmLabel}</Btn>
      <Btn disabled={!canConfirm} onClick={() => onDecide('decline')}>Decline</Btn>
    </div>
    {canConfirm && <p className="small muted">When this is the only pending review, you can type “Confirm this proposal” or “Decline this proposal”.</p>}
  </>;
}

function TaskLink({task, message, onView}: {task:ChatTask; message:string; onView:(anchor:string)=>void}) {
  const result = task.executionId && (task.settledBlock || ['succeeded','failed','cancelled','interrupted'].includes(task.state));
  const target = result ? 'changes' : 'overview';
  const anchor = `chat-task-${message}`;
  return <div className="chat-evidence">
    <a id={anchor} href={taskSectionLink(task.id,target)} onClick={() => onView(anchor)}>
      {result ? 'View task result' : 'Open task'}
    </a>
    {result && task.graphId && task.executionId && <a id={`${anchor}-execution`}
      href={`#/g/${encodeURIComponent(task.graphId)}/runs?execution=${encodeURIComponent(task.executionId)}&section=result`}
      onClick={() => onView(`${anchor}-execution`)}>View execution</a>}
  </div>;
}
