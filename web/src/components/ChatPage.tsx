import { ChatModelSection } from './ChatModelSection';
import { ChatSkillCard } from './ChatSkillCard';
import { runLink } from './runPresentation';
import { ChatPRCard } from './ChatPRCard';
import { takeChatReturnFocus } from './chatConnectionReturn';
import { ChatInstructionCard } from './ChatInstructionCard';
import { ChatGuidanceCard } from './ChatGuidanceCard';
import { ChatConfigurationCard } from './ChatConfigurationCard';
import { ChatManualLink } from './ChatManualLink';
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { api, ApiError, errorMessage, MOCK } from '../api/client';
import type { ChatAgentAdoptionReview, ChatApprovalReview, ChatCatalogPublicationReview, ChatMessage, ChatOperationReview,
  ChatSession, ChatSessionSummary, ChatTask, ChatTaskCancellationReview, ChatTaskDraftReview, ChatTaskStartReview, ChatWorkflowControlReview,
  ChatBudgetRaiseReview, ChatEnvironmentReview, ChatCorrectionReview, ChatTaskAuthorizationReview, ChatTaskRetryReview,
  ChatWorkflowScaleReview, ChatTriggerReview, ChatWorkflowEnvironmentReview } from '../api/chat';
import { useAsync } from '../hooks/useData';
import { Btn, ErrorBox, Modal } from './common';
import { taskSectionLink } from './taskDetailPresentation';
import { settingsPath } from './graphSettingsSections';
import './chat.css';

type OpenChat = (id?: string, archived?: boolean) => void;

const accessLost = (error: unknown) => error instanceof ApiError && [401,403,404].includes(error.status);

export function ChatPage({id, archived, canCreateAgent = false, canOperate = false, onOpen}: {
  id?: string; archived:boolean; canCreateAgent?:boolean; canOperate?:boolean; onOpen:OpenChat;
}) {
  const settings = useAsync(api.chatModelSettings, []);
  const [cursor, setCursor] = useState<{archived:boolean; after?:string}>({archived});
  const after = cursor.archived === archived ? cursor.after : undefined;
  const role = canCreateAgent ? 'admin' : canOperate ? 'operator' : 'viewer';
  const pageKey = `${role}/${archived}/${after ?? ''}`;
  const list = useAsync(async () => ({...await api.chatSessions(archived,after),pageKey}), [pageKey], accessLost);
  const visiblePage = list.data?.pageKey === pageKey ? list.data : undefined;
  const conversation = useAsync(() => id ? api.chatSession(id) : Promise.resolve(undefined), [id], accessLost);
  const session = conversation.data?.id === id ? conversation.data : undefined;
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [newDraft, setNewDraft] = useState(0);
  const [busy, setBusy] = useState<ReadonlySet<string>>(new Set());
  const pending = useRef(new Set<string>());
  const view = useRef(0);
  useLayoutEffect(() => {
    view.current++;
    return () => { view.current++; };
  }, [id, archived, newDraft]);
  const [error, setError] = useState<string>();
  const [drawer, setDrawer] = useState(false);
  const [modelEditorOpen, setModelEditorOpen] = useState(false);
  const modelPanel = useRef<HTMLDivElement>(null);
  const openModelEditor = () => {
    setModelEditorOpen(true);
    window.requestAnimationFrame(() => modelPanel.current?.focus());
  };
  const [details, setDetails] = useState<string>();
  const [now, setNow] = useState(Date.now);
  const keys = useRef(new Map<string, {id:string; text:string}>());
  const heading = useRef<HTMLHeadingElement>(null);
  const drawerButton = useRef<HTMLButtonElement>(null);
  const drawerClose = useRef<HTMLButtonElement>(null);
  const evidence = useRef<string | undefined>(undefined);
  const requestedRead = useRef(new Set<string>());
  const draftKey = id ?? `new-${newDraft}`;
  const sending = busy.has(draftKey);
  const draft = drafts[draftKey] ?? '';
  const modelAvailable = !MOCK && settings.data?.status === 'configured';
  const waiting = session?.messages.some(message => message.state === 'pending' || message.state === 'interpreting');
  const headingTitle = session ? session.title || 'New chat' : 'Chat';
  const titlePending = session?.titleRetryState === 'pending' && !!session.titleRetryStarted &&
    now - Date.parse(session.titleRetryStarted) < 60_000;
  const archivedUnread = visiblePage?.archivedUnread ?? 0;
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
    view.current++;
    setDrawer(false);
    setDetails(undefined);
    setError(undefined);
    if (!next) setCursor({archived:archive});
    onOpen(next, archive);
    window.requestAnimationFrame(() => heading.current?.focus());
  };
  const newChat = () => {
    setNewDraft(previous => previous + 1);
    navigate();
  };

  const textDecision = /^(confirm this proposal|confirme esta proposta|decline this proposal|recuse esta proposta)$/i.test(draft.trim().replace(/\.$/, '').trim());
  const canSend = modelAvailable || (!MOCK && (draft.trim() === '/export' || /^\s*\/(?:inspect|find|choose)(?:\s|$)/.test(draft) || (!!session && (textDecision || session.messages.at(-1)?.state === 'discovery'))));

  async function send() {
    if (!canSend || !draft.trim() || sending || waiting) return;
    const previous = keys.current.get(draftKey);
    const requestId = previous?.text === draft ? previous.id
      : Array.from(crypto.getRandomValues(new Uint8Array(16)), value => value.toString(16).padStart(2,'0')).join('');
    keys.current.set(draftKey, {id:requestId,text:draft});
    await mutate(async () => {
      const next = await api.sendConversationMessage(id, requestId, draft);
      keys.current.delete(draftKey);
      setDrafts(previous => previous[draftKey] === draft ? {...previous, [draftKey]:''} : previous);
      return next;
    }, next => { if (!id) onOpen(next.id); });
  }

  async function mutate(action: () => Promise<ChatSession>, whenVisible?: (next: ChatSession) => void) {
    if (pending.current.has(draftKey)) return;
    const origin = view.current;
    pending.current.add(draftKey);
    setBusy(new Set(pending.current));
    setError(undefined);
    try {
      const result = await action();
      if (view.current === origin) {
        conversation.setData(result);
        whenVisible?.(result);
      }
    } catch (error) {
      if (view.current === origin) {
        if (accessLost(error)) conversation.setData(undefined);
        setError(errorMessage(error));
      }
    } finally {
      pending.current.delete(draftKey);
      setBusy(new Set(pending.current));
      void list.reload();
    }
  }

  async function markRead(message: string) {
    const readKey = `${id}/${message}`;
    if (!id || requestedRead.current.has(readKey)) return;
    const origin = view.current;
    requestedRead.current.add(readKey);
    try {
      const result = await api.readChatResult(id, message);
      if (view.current === origin) conversation.setData(result);
      void list.reload();
    } catch {
      if (view.current === origin) setError('Unable to mark this result as read. Try again when connected.');
    } finally {
      requestedRead.current.delete(readKey);
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
          onClick={() => void mutate(() => api.archiveChat(session.id,!session.archived), next => onOpen(next.id,next.archived))}>
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
      {!MOCK && canCreateAgent && !modelAvailable && !modelEditorOpen && settings.data &&
        <Btn onClick={openModelEditor}>Choose Chat model here</Btn>}
      {!MOCK && canCreateAgent && modelEditorOpen && <div ref={modelPanel} tabIndex={-1}>
        <ChatModelSection canEdit onSaved={settings.setData}/>
        <Btn onClick={() => {
          setModelEditorOpen(false);
          heading.current?.focus();
        }}>Close model settings</Btn>
      </div>}
      {id && !session && conversation.loading && <p role="status">Loading conversation…</p>}
      {id && conversation.error && <><ErrorBox error={session ? conversation.error : 'Conversation unavailable.'}/>
        <Btn onClick={() => void conversation.reload()}>Retry loading conversation</Btn></>}
      {!id && !archived && <p className="muted">Ask to prepare a task, follow its progress, or create a reusable agent or workflow.</p>}
      {session?.watches?.filter(watch => !watch.complete).map(watch => <p className="chat-watch" key={watch.requestId}>
        {watch.unknown ? 'Status temporarily unknown' : watch.task.decisionNeeded ? 'Decision required' : `Following task: ${watch.task.state}`} · <a href={taskSectionLink(watch.task.id,'overview')}>{watch.task.id}</a>
        {watch.unknown && <span className="muted"> · Last seen <time dateTime={watch.observedAt}>{new Date(watch.observedAt).toLocaleString()}</time></span>}
      </p>)}
      <div className="chat-transcript" aria-label="Conversation">
        {session?.messages.map(message => <Message key={message.id} sessionId={session.id} value={message}
          now={now} canCreateAgent={canCreateAgent && !sending} canOperate={canOperate && !sending}
          onModelSettings={canCreateAgent ? openModelEditor : undefined}
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
      {!drawer && <section aria-label={archived ? 'Archived' : 'Recents'}>{sessionList}</section>}
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
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const preview = !expanded && !dismissed && (hovered || focused);
  useEffect(() => {
    if (!preview) return;
    const dismiss = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.stopPropagation();
        setDismissed(true);
      }
    };
    document.addEventListener('keydown', dismiss, true);
    return () => document.removeEventListener('keydown', dismiss, true);
  }, [preview]);
  const otherUnread = Math.max(0,item.unread - item.unreadSuccess - item.unreadFailure - item.unreadCancellation);
  const status = [
    otherUnread ? `${otherUnread} unread ${otherUnread === 1 ? "reply" : "replies"}` : '',
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
  return <li className="chat-session-row" onPointerLeave={() => setHovered(false)} onKeyDown={event => {
    if (event.key === 'Escape' && expanded) {
      event.stopPropagation();
      setDismissed(true);
      onDetails();
    }
  }}>
    <div className="chat-session-line">
      <button type="button" className="chat-session-title" onClick={onOpen}
        onPointerEnter={event => { if (event.pointerType !== 'touch') { setHovered(true); setDismissed(false); } }}
        onFocus={event => { setFocused(event.currentTarget.matches(':focus-visible')); setDismissed(false); }}
        onBlur={() => setFocused(false)} aria-describedby={`chat-row-${item.id}`}
        aria-current={current?'page':undefined}>{title}</button>
      <span className="chat-session-indicators" role={status ? 'img' : undefined} aria-label={status || undefined}>
        {!!item.running && <span className="chat-running" aria-hidden="true"/>}
        {!!otherUnread && <span className="chat-reply-dot" aria-hidden="true"/>}
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
    {preview && <div className="chat-session-preview" aria-hidden="true"><strong>{title}</strong><div>{description}</div></div>}
  </li>;
}

function Message({sessionId, value, now, canCreateAgent, canOperate, onView, onRead, onRetry, onDecide, onModelSettings}: {
  sessionId:string;
  onModelSettings?:()=>void;
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
    {value.destination === 'chat-model' && onModelSettings && <Btn onClick={onModelSettings}>Choose Chat model here</Btn>}
    {value.destination && <ChatManualLink destination={value.destination} message={value.id} onView={onView}/>}
    {(value.state === 'pending' || value.state === 'interpreting') && <p role="status">Interpreting request…</p>}
    {onRetry && <Btn onClick={onRetry}>Retry saved request</Btn>}
    {value.execution && <section aria-label="Execution evidence">
      <p>{value.execution.id} · {value.execution.state} · {value.execution.active} of {value.execution.nodes} executions active</p>
      <p>Tree root · {value.execution.root}</p>
      {value.execution.cancellationRequested && <p>Cancellation requested</p>}
      <a id={`chat-execution-${value.id}`} href={runLink(value.execution.graph,value.execution.id)}
        onClick={() => onView(`chat-execution-${value.id}`)}>View execution</a>
    </section>}
    {value.task && <TaskLink task={value.task} message={value.id} onView={onView}/>}
    {review && <OperationReviewCard sessionId={sessionId} review={review} now={now} canConfirm={('executionCancel' in review || 'taskCorrection' in review || 'pr' in review || 'taskDraft' in review || 'taskRetry' in review || 'guidance' in review || 'workflowControl' in review || 'workflowScale' in review || 'workflowEnvironment' in review || 'approval' in review) ? canOperate : canCreateAgent}
      onDecide={decision => onDecide(review,decision)} onView={onView}/>}
    {value.unread && <Btn onClick={onRead}>Mark result read</Btn>}
  </article>;
}

function OperationReviewCard({sessionId, review, now, canConfirm, onDecide, onView}: {
  sessionId:string; review:ChatOperationReview; now:number; canConfirm:boolean; onDecide:(decision:'confirm'|'decline')=>void; onView:(anchor:string)=>void;
}) {
  if ('budgetRaise' in review) return <BudgetRaiseCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  if ('skill' in review) return <ChatSkillCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  if ('pr' in review) return <ChatPRCard sessionId={sessionId} review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  if ('instructions' in review) return <ChatInstructionCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  if ('guidance' in review) return <ChatGuidanceCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide}/>;
  if ('configuration' in review) return <ChatConfigurationCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide}/>;
  if ('taskDraft' in review) return <TaskDraftReviewCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  if ('taskStart' in review) return <TaskStartReviewCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  if ('taskCancel' in review) return <TaskCancellationCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  if ('taskAuthorization' in review) return <TaskAuthorizationCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  if ('workflowControl' in review) return <WorkflowControlCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  if ('executionCancel' in review) {
    const target = review.executionCancel;
    const anchor = `chat-cancel-${review.id}`;
    return <section className="chat-agent-review" aria-label="Review execution cancellation">
      <h3>{target.mode === 'tree' ? 'Cancel execution tree' : 'Cancel delegated child'}</h3>
      <p>{target.graph} · Run {target.run}</p>
      <p>Selected execution · {target.selected}</p><p>Tree root · {target.root}</p>
      {target.mode === 'tree' && <p>The request covers the root and its descendants.</p>}
      {review.state === 'awaiting_confirmation' && <ReviewDecisionControls expiresAt={review.expiresAt} now={now}
        canConfirm={canConfirm} confirmLabel="Request cancellation" onDecide={onDecide}/>}
      {review.state === 'applying' && <p role="status">Recording cancellation…</p>}
      {review.state === 'accepted' && <p role="status">Cancellation requested. Waiting for the execution scope to settle.</p>}
      {review.state === 'applied' && <p role="status">The selected scope settled with an observed cancellation.</p>}
      {review.state === 'incomplete' && <p role="status">The scope settled before any execution reported cancellation.</p>}
      {review.state === 'unknown' && <p role="status">Cancellation receipt unavailable.</p>}
      {review.state === 'declined' && <p role="status">Proposal declined.</p>}
      {review.state === 'conflict' && <p role="status">The execution or its cancellation request changed. Inspect its current state.</p>}
      <a id={anchor} href={runLink(target.graph,target.selected)} onClick={()=>onView(anchor)}>View execution</a>
    </section>;
  }
  if ('workflowScale' in review) return <WorkflowScaleCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  if ('triggerControl' in review) return <TriggerControlCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  if ('environment' in review || 'workflowEnvironment' in review) return <EnvironmentPreparationCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  if ('taskCorrection' in review) return <CorrectionCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  if ('taskRetry' in review) return <TaskRetryReviewCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  if ('publication' in review) return <CatalogPublicationCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  if ('adoption' in review) return <AgentAdoptionCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
  if ('approval' in review) return <ApprovalDecisionCard review={review} now={now} canConfirm={canConfirm} onDecide={onDecide} onView={onView}/>;
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

const budgetResumeLabels: Record<string, string> = {
  resumed: 'Running observed', pending: 'Waiting to observe resumption', budget_reached: 'Still blocked by an applicable budget',
  run_ended: 'Attempt ended after the resume command', stopped: 'Run stopped', run_changed: 'Run changed',
  not_applicable: 'This allowance no longer applies', deleted: 'Run no longer exists', already_running: 'Already running',
  unbound: 'Original run could not be verified', superseded: 'Resume command was superseded', unknown: 'Resumption could not be verified',
};

function budgetAmount(micros:number, currency:string) {
  const amount = (micros / 1_000_000).toFixed(6).replace(/\.?0+$/, '');
  return `${amount} ${currency}`;
}

function BudgetRaiseCard({review, now, canConfirm, onDecide, onView}: {
  review:ChatBudgetRaiseReview; now:number; canConfirm:boolean; onDecide:(decision:'confirm'|'decline')=>void; onView:(anchor:string)=>void;
}) {
  const {target, subject, actor, limitMicros} = review.budgetRaise;
  const result = review.budgetResult;
  const anchor = `chat-budget-${review.id}`;
  return <section className="chat-agent-review" aria-label={`Increase budget for ${subject}`}>
    <h3>Increase current budget for {subject}</h3>
    <dl>
      <dt>Scope</dt><dd>{target.period ? `${target.scope} · ${target.period} UTC` : 'This task attempt'}</dd>
      <dt>Ledger</dt><dd>{target.id} · Revision {target.revision}</dd>
      {target.period && <><dt>Recurring definition revision</dt><dd>{target.definitionsRevision}</dd></>}
      <dt>Requested by</dt><dd>{actor}</dd>
      <dt>Current limit</dt><dd>{budgetAmount(target.limitMicros,target.currency)}</dd>
      <dt>Proposed limit</dt><dd>{budgetAmount(limitMicros,target.currency)}</dd>
    </dl>
    <p>{target.period ? 'This change applies to the current month. The recurring definition stays in effect for future months.'
      : 'This change applies to this task attempt. The default limit for future attempts stays in effect.'}</p>
    {target.pausedGraphs?.length ? <><p>Runs held by this ledger may resume under the current budget policy.</p>
      <ul>{target.pausedGraphs.map(graph => <li key={graph}>{graph}</li>)}</ul></> : null}
    {review.state === 'awaiting_confirmation' && <ReviewDecisionControls expiresAt={review.expiresAt} now={now}
      canConfirm={canConfirm} confirmLabel="Increase current budget" onDecide={onDecide}/>}
    {review.state === 'applying' && <p role="status">Applying the reviewed increase…</p>}
    {review.state === 'conflict' && <p role="status">The ledger or its definition changed. Request a fresh review.</p>}
    {review.state === 'declined' && <p role="status">Proposal declined.</p>}
    {review.state === 'unknown' && <p role="status">The latest outcome could not be verified. Inspect the budget before requesting another increase.</p>}
    {!!result?.revision && <p role="status">Limit raised to {budgetAmount(limitMicros,target.currency)}. Ledger revision {result.revision}.</p>}
    {result?.reached && <p>The new allowance is still exhausted.</p>}
    {review.state === 'accepted' && <p role="status">Watching the affected runs.</p>}
    {result?.resumes?.length ? <ul>{result.resumes.map(resume => <li key={resume.graphId}>
      {resume.graphId}{resume.runId && ` · ${resume.runId}`} · {budgetResumeLabels[resume.state] ?? 'Resumption could not be verified'}
    </li>)}</ul> : null}
    <a id={anchor} href="#/models/budgets" onClick={() => onView(anchor)}>Open budgets</a>
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
    {!!review.taskDraft.repositories?.length && <ul aria-label="Reviewed repository pins">
      {review.taskDraft.repositories.map(repo => <li key={repo.alias}>{repo.alias} · {repo.project} · Revision {repo.revision} · <code>{repo.commit}</code></li>)}
    </ul>}
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
      {task.repositories?.map(repo=><div key={repo.alias || repo.project}><dt>{repo.alias || 'Repository'}</dt><dd>{repo.project} · revision {repo.revision} · {repo.commit}{repo.checks?.length ? ' · Checks ' + repo.checks.join(', ') : ''}</dd></div>)}
      {task.issueSource && <><dt>Issue source</dt><dd>{task.issueSource}</dd></>}
      {task.build && <><dt>Prepared environment</dt><dd>{task.build.graph} · {task.build.hash} · {task.build.completed}</dd></>}
      {task.agentGrants.map(agent => <div key={agent.id}>
        <dt>{agent.id}</dt><dd>{agent.isolation} · {agent.tools?.join(', ') || 'No tool grants'}{agent.constraints?.map(rule=><pre key={rule}>{rule}</pre>)}</dd>
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

function WorkflowScaleCard({review, now, canConfirm, onDecide, onView}: {
  review:ChatWorkflowScaleReview; now:number; canConfirm:boolean; onDecide:(decision:'confirm'|'decline')=>void; onView:(anchor:string)=>void;
}) {
  const {scope, size} = review.workflowScale;
  const result = review.scaleResult;
  const anchor = `chat-scale-${review.id}`;
  const target = `${scope.workflowId} / ${scope.nodeId}`;
  const stopped = scope.nodeStopped || scope.nodeDisabled || scope.desiredState === 'stopped';
  return <section className="chat-agent-review" aria-label={`Scale ${target}`}>
    <h3>Scale {scope.nodeId}</h3>
    <dl>
      <dt>Target</dt><dd>{target} · Version {scope.version}</dd>
      {scope.runId && <><dt>Reviewed run</dt><dd>{scope.runId}</dd></>}
      <dt>{scope.autoscale ? 'Autoscale ceiling' : 'Fixed replica count'}</dt><dd>{scope.current} → {size}</dd>
      {scope.autoscale && <><dt>Autoscale minimum</dt><dd>{scope.minimum}</dd></>}
    </dl>
    <p>{scope.autoscale ? 'This saves a workflow revision. The scheduler chooses the active count within the reviewed bounds.'
      : size === 0 ? 'This lets active turns drain and stops this node.'
      : 'This saves a runtime override for this node.'}</p>
    {stopped && size > 0 && <p>The new policy applies when the node next runs.</p>}
    {review.state === 'awaiting_confirmation' && <ReviewDecisionControls expiresAt={review.expiresAt} now={now} canConfirm={canConfirm}
      confirmLabel={scope.autoscale ? 'Save autoscale ceiling' : 'Save replica count'} onDecide={onDecide}/>}
    {review.state === 'applying' && <p role="status">Applying the reviewed scale…</p>}
    {review.state === 'accepted' && <p role="status">Replica policy saved. Waiting for the runtime to settle.</p>}
    {result?.state === 'saved' && <p role="status">Policy saved · Version {result.version}. The node is inactive.</p>}
    {result?.state === 'observed' && <p role="status">Observed {result.activeReplicas} active replicas · Version {result.version}.</p>}
    {review.state === 'conflict' && <p role="status">The target changed or a workflow guard blocked the action. Inspect the node and request a new review.</p>}
    {review.state === 'incomplete' && <p role="status">The policy was saved. Inspect the node to see why the runtime did not settle under this review.</p>}
    {review.state === 'unknown' && <p role="status">The outcome could not be verified. Inspect the current workflow before trying again.</p>}
    {review.state === 'declined' && <p role="status">Proposal declined.</p>}
    <a id={anchor} href={`#/g/${encodeURIComponent(scope.workflowId)}/canvas?sel=${encodeURIComponent('node:' + scope.nodeId)}`}
      onClick={() => onView(anchor)}>View node</a>
  </section>;
}

function TriggerControlCard({review, now, canConfirm, onDecide, onView}: {
  review:ChatTriggerReview; now:number; canConfirm:boolean; onDecide:(decision:'confirm'|'decline')=>void; onView:(anchor:string)=>void;
}) {
  const trigger = review.triggerControl;
  const resume = trigger.action === 'resume';
  const command = resume ? 'Resume issue intake' : 'Pause issue intake';
  const anchor = `chat-trigger-${review.id}`;
  return <section className="chat-agent-review" aria-label={`${command} for ${trigger.name}`}>
    <h3>{command}</h3>
    <dl>
      <dt>Trigger</dt><dd>{trigger.name} · {trigger.id}</dd>
      <dt>Source</dt><dd>{trigger.sourceKind} · {trigger.connection} · {trigger.sourceProject}</dd>
      <dt>Match label</dt><dd>{trigger.label}</dd>
      {trigger.query && <><dt>Match query</dt><dd>{trigger.query}</dd></>}
      {trigger.owner && <><dt>Issue owner</dt><dd>{trigger.owner}</dd></>}
      <dt>Workflow</dt><dd>{trigger.workflow}{trigger.workflowVersion > 0 && ` · Version ${trigger.workflowVersion}`}
        {' · '}{trigger.node}{trigger.resultNode && trigger.resultNode !== trigger.node && ` → ${trigger.resultNode}`}</dd>
      {trigger.project && <><dt>Project</dt><dd>{trigger.project}</dd></>}
      {trigger.scm && <><dt>SCM connection</dt><dd>{trigger.scm}</dd></>}
      {resume && <><dt>Consent</dt><dd>{trigger.actor} · {trigger.authorization}{trigger.executionMode && ` · ${trigger.executionMode}`}</dd>
        <dt>Poll interval</dt><dd>{trigger.intervalMinutes} minutes</dd></>}
      {trigger.currentTask && <><dt>Admitted task</dt><dd>{trigger.currentTask}</dd></>}
      {trigger.claiming && <><dt>Admission under review</dt><dd>{trigger.claiming}</dd></>}
    </dl>
    {resume && trigger.operations?.length ? <details><summary>Reviewed tracker operations</summary>
      <ul>{trigger.operations.map(operation => <li key={operation}>{operation}</li>)}</ul></details> : null}
    <p>{resume ? 'Confirmation records fresh consent for this trigger to admit matching issues.'
      : 'Pause stops new intake. An admission already underway can settle, and admitted tasks follow their own lifecycle.'}</p>
    {review.state === 'awaiting_confirmation' && <ReviewDecisionControls expiresAt={review.expiresAt} now={now} canConfirm={canConfirm}
      confirmLabel={command} onDecide={onDecide}/>}
    {review.state === 'applying' && <p role="status">Recording the reviewed intake change…</p>}
    {review.state === 'applied' && <p role="status">{resume ? 'Issue intake resumed with reviewed consent.' : 'Future issue intake paused.'}</p>}
    {review.state === 'conflict' && <p role="status">The trigger or its consent scope changed. Inspect Triggers and request a fresh review.</p>}
    {review.state === 'unknown' && <p role="status">The outcome could not be verified. Inspect Triggers before trying again.</p>}
    {review.state === 'declined' && <p role="status">Proposal declined.</p>}
    <a id={anchor} href={settingsPath(trigger.workflow,'triggers')} onClick={() => onView(anchor)}>Open workflow triggers</a>
  </section>;
}

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

function EnvironmentPreparationCard({review, now, canConfirm, onDecide, onView}: {
  review:ChatEnvironmentReview|ChatWorkflowEnvironmentReview; now:number; canConfirm:boolean; onDecide:(decision:'confirm'|'decline')=>void; onView:(anchor:string)=>void;
}) {
  const task = 'environment' in review ? review.environment : undefined;
  const target = 'environment' in review ? review.environment : review.workflowEnvironment;
  const anchor = 'chat-environment-' + review.id;
  const name = task ? 'Prepare task environment' : 'Prepare workflow environment';
  return <section className="chat-agent-review" aria-label={name}>
    <h3>{name}</h3>
    <p>{task && <>{task.task} · revision {task.revision} · </>}{target.workflow} v{target.workflowVersion}</p>
    {task?.repositories?.map(repository=><p key={repository}>{repository}</p>)}
    <pre>{target.definition}</pre>
    {!task && <details><summary>Reviewed definition hash</summary><code>{target.definitionHash}</code></details>}
    <p>Preparing the environment can build an image and run its setup. Starting {task ? 'task agents' : 'the workflow'} takes a separate decision.</p>
    {review.state === 'awaiting_confirmation' && <ReviewDecisionControls expiresAt={review.expiresAt} now={now} canConfirm={canConfirm} confirmLabel="Prepare environment" onDecide={onDecide}/>}
    {review.state === 'applied' && <p role="status">Environment ready for the reviewed definition.</p>}
    {(review.state === 'applying' || review.state === 'queued') && <p role="status">Checking environment preparation…</p>}
    {review.state === 'unknown' && <p role="status">Build outcome unknown. Open {task ? 'the task' : 'Environment settings'} for diagnostics.</p>}
    {review.state === 'incomplete' && <p role="status">The reviewed definition is unavailable. Inspect the build state in {task ? 'Tasks' : 'Environment settings'}.</p>}
    {review.state === 'declined' && <p role="status">Preparation declined.</p>}
    {review.state === 'conflict' && <p role="status">The reviewed state changed. Request a fresh review.</p>}
    <a id={anchor} href={task ? taskSectionLink(task.task,'overview') : settingsPath(target.workflow,'environment')}
      onClick={()=>onView(anchor)}>{task ? 'Inspect task' : 'Inspect workflow environment'}</a>
  </section>;
}

function CorrectionCard({review, now, canConfirm, onDecide, onView}: {
  review:ChatCorrectionReview; now:number; canConfirm:boolean; onDecide:(decision:'confirm'|'decline')=>void; onView:(anchor:string)=>void;
}) {
  const {taskCorrection: edit} = review;
  const anchor = 'chat-correction-' + review.id;
  return <section className="chat-agent-review" aria-label="Task draft correction">
    <h3>Edit task draft</h3>
    <p>{edit.task} · {edit.workflow} v{edit.workflowVersion} · draft revision {edit.revision}</p>
    <h4>Current</h4><p>{edit.before.title}</p><p>{edit.before.request}</p><p>{edit.before.outcome}</p>
    <h4>Proposed</h4><p>{edit.after.title}</p><p>{edit.after.request}</p><p>{edit.after.outcome}</p>
    {review.state === 'awaiting_confirmation' && <ReviewDecisionControls expiresAt={review.expiresAt} now={now} canConfirm={canConfirm} confirmLabel="Save draft correction" onDecide={onDecide}/>}
    {review.state === 'applied' && <p role="status">Task draft revision {review.resultVersion} saved.</p>}
    {review.state === 'applying' && <p role="status">Saving the draft correction…</p>}
    {review.state === 'unknown' && <p role="status">Save outcome unknown. Open the task to inspect its current revision.</p>}
    {review.state === 'declined' && <p role="status">Correction declined.</p>}
    {review.state === 'conflict' && <p role="status">The task or workflow changed. Request a fresh review.</p>}
    <a id={anchor} href={taskSectionLink(edit.task,'overview')} onClick={()=>onView(anchor)}>Open task draft</a>
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

function ApprovalDecisionCard({review, now, canConfirm, onDecide, onView}: {
  review:ChatApprovalReview; now:number; canConfirm:boolean; onDecide:(decision:'confirm'|'decline')=>void; onView:(anchor:string)=>void;
}) {
  const {approval} = review;
  const action = approval.decision === 'approve' ? 'Approve' : 'Reject';
  const anchor = `chat-approval-${review.id}`;
  return <section className="chat-agent-review" aria-label={`${action} request ${approval.id}`}>
    <h3>{approval.title || 'Pending request'}</h3>
    <dl>
      <dt>Approval ID</dt><dd>{approval.id}</dd>
      <dt>Request type</dt><dd>{approval.kind}</dd>
      <dt>Execution target</dt><dd>{approval.graphId}{approval.nodeId && ` / ${approval.nodeId}`}
        {approval.nodeId && approval.replica >= 0 && ` · Replica ${approval.replica}`}</dd>
      {approval.executionId && <><dt>Execution</dt><dd>{approval.executionId}</dd></>}
      {approval.taskId && <><dt>Task</dt><dd>{approval.taskId}</dd></>}
      {approval.answer && <><dt>Reply</dt><dd>{approval.answer}</dd></>}
      {approval.comment && <><dt>Comment</dt><dd>{approval.comment}</dd></>}
    </dl>
    {review.state === 'awaiting_confirmation' && <>
      <p>Record the decision for this exact request. Open Approvals for the full request details.</p>
      <ReviewDecisionControls expiresAt={review.expiresAt} now={now} canConfirm={canConfirm} confirmLabel={`${action} request`} onDecide={onDecide}/>
    </>}
    {review.state === 'applying' && <p role="status">Decision submitted. Checking the recorded outcome…</p>}
    {review.state === 'conflict' && <p role="status">The approval expired or changed. Check its current state in Approvals.</p>}
    {review.state === 'unknown' && <p role="status">The decision outcome is temporarily unknown. Inspect Approvals before trying again.</p>}
    {review.state === 'declined' && <p role="status">Proposal declined.</p>}
    {review.state === 'applied' && <p role="status">{approval.decision === 'approve' ? 'Approval' : 'Rejection'} recorded.</p>}
    <div className="chat-evidence">
      <a id={anchor} href="#/approvals" onClick={() => onView(anchor)}>Open Approvals</a>
      {approval.taskId && <a id={`${anchor}-task`} href={taskSectionLink(approval.taskId,'overview')}
        onClick={() => onView(`${anchor}-task`)}>Open task</a>}
    </div>
  </section>;
}

function TaskCancellationCard({review, now, canConfirm, onDecide, onView}: {
  review:ChatTaskCancellationReview; now:number; canConfirm:boolean; onDecide:(decision:'confirm'|'decline')=>void; onView:(anchor:string)=>void;
}) {
  const {taskCancel:task} = review;
  const anchor = `chat-cancel-${review.id}`;
  return <section className="chat-agent-review" aria-label={`Cancel task ${task.title}`}>
    <h3>{task.title}</h3>
    <dl>
      <dt>Task</dt><dd>{task.id} · Revision {task.revision}</dd>
      <dt>Execution</dt><dd>{task.executionGraphId} · Version {task.executionVersion}</dd>
      <dt>Run</dt><dd>{task.runId}</dd>
      <dt>Pending approvals</dt><dd>{task.pendingApprovals}</dd>
    </dl>
    {review.state === 'awaiting_confirmation' && <>
      <p>Interrupt this task attempt and close its pending approvals. Captured evidence and workspaces are retained.</p>
      <ReviewDecisionControls expiresAt={review.expiresAt} now={now} canConfirm={canConfirm} confirmLabel="Cancel task attempt" onDecide={onDecide}/>
    </>}
    {review.state === 'applying' && <p role="status">Cancellation requested. Checking the task record…</p>}
    {review.state === 'accepted' && <p role="status">Cancellation request recorded. Follow the task result for the confirmed outcome.</p>}
    {review.state === 'conflict' && <p role="status">The task or its active run changed. Inspect the task and request a fresh review.</p>}
    {review.state === 'unknown' && <p role="status">Cancellation is temporarily unknown. Inspect the task before trying again.</p>}
    {review.state === 'declined' && <p role="status">Proposal declined.</p>}
    <a id={anchor} href={taskSectionLink(task.id,'overview')} onClick={() => onView(anchor)}>Open task</a>
  </section>;
}

const taskModeLabels = {ask:'Ask for approval',workspace:'Workspace access',full:'Full access'};

function TaskAuthorizationCard({review, now, canConfirm, onDecide, onView}: {
  review:ChatTaskAuthorizationReview; now:number; canConfirm:boolean; onDecide:(decision:'confirm'|'decline')=>void; onView:(anchor:string)=>void;
}) {
  const {taskAuthorization:auth} = review;
  const anchor = `chat-authorization-${review.id}`;
  return <section className="chat-agent-review" aria-label={`Authorize task ${auth.title}`}>
    <h3>{auth.title}</h3>
    <dl>
      <dt>Task</dt><dd>{auth.id} · Revision {auth.taskRevision}</dd>
      <dt>Workflow</dt><dd>{auth.workflowId} · Version {auth.workflowVersion}</dd>
      <dt>Execution environment</dt><dd>{auth.executionMode || 'Workflow default'}</dd>
      <dt>Current mode</dt><dd>{taskModeLabels[auth.current.mode]}{auth.current.revision === 0 ? ' · Default' : ` · Revision ${auth.current.revision}`}</dd>
      <dt>Proposed mode</dt><dd>{taskModeLabels[auth.mode]}</dd>
    </dl>
    {auth.repositories && <ul aria-label="Repositories">{auth.repositories.map(repo => <li key={repo.projectId}>
      {repo.alias && `${repo.alias} · `}{repo.projectId} · {repo.baseCommit}{repo.scmConnection && ` · ${repo.scmConnection}`}
      {repo.repository && ` · ${repo.repository}`}{repo.targetBranch && ` · ${repo.targetBranch}`}
    </li>)}</ul>}
    {auth.agents.map(agent => <details key={agent.id}>
      <summary>{agent.id} · {agent.isolation} · {agent.grants?.length || 0} tool grants</summary>
      {agent.grants?.length ? <pre>{JSON.stringify(agent.grants,null,2)}</pre> : <p>No declared tool grants.</p>}
    </details>)}
    {review.state === 'awaiting_confirmation' && <>
      <p>Record this authorization for the selected task. Mandatory approvals and installation rules still apply.</p>
      <ReviewDecisionControls expiresAt={review.expiresAt} now={now} canConfirm={canConfirm} confirmLabel="Save task authorization" onDecide={onDecide}/>
    </>}
    {review.state === 'applying' && <p role="status">Authorization requested. Checking the task settings…</p>}
    {review.state === 'applied' && <p role="status">Authorization revision {review.resultVersion} saved.</p>}
    {review.state === 'conflict' && <p role="status">The reviewed task or authorization changed. Inspect its current settings before requesting a new review.</p>}
    {review.state === 'unknown' && <p role="status">The authorization outcome is temporarily unknown. Inspect the task settings before trying again.</p>}
    {review.state === 'declined' && <p role="status">Proposal declined.</p>}
    <a id={anchor} href={taskSectionLink(auth.id,'settings')} onClick={() => onView(anchor)}>Open Settings &amp; access</a>
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
    {!!task.repositories?.length && <section aria-label="Repository evidence">
      <h4>Repositories</h4>
      <ul>{task.repositories.map(row=><li key={row.alias || row.project}>
        <strong>{row.alias || row.project}</strong> · {row.project} @ <code>{row.base}</code> · {row.preparation}
        {row.delivery && <> · {row.delivery}</>}{row.pullRequest && <> · PR #{row.pullRequest}</>}
      </li>)}</ul>
      {!!task.otherRepositories && <p>{task.otherRepositories} more in Tasks.</p>}
    </section>}
    {task.retryOf && <a href={taskSectionLink(task.retryOf,'overview')} id={anchor+'-prior'} onClick={()=>onView(anchor+'-prior')}>Previous attempt</a>}
    <a id={anchor} href={taskSectionLink(task.id,target)} onClick={() => onView(anchor)}>
      {result ? 'View task result' : 'Open task'}
    </a>
    {result && task.graphId && task.executionId && <a id={`${anchor}-execution`}
      href={`#/g/${encodeURIComponent(task.graphId)}/runs?execution=${encodeURIComponent(task.executionId)}&section=result`}
      onClick={() => onView(`${anchor}-execution`)}>View execution</a>}
  </div>;
}
