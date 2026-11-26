import { useEffect, useRef, useState } from 'react';
import { api, ApiError, errorMessage, MOCK } from '../api/client';
import type { ChatAgentReview, ChatMessage, ChatSession, ChatSessionSummary, ChatTask } from '../api/chat';
import { useAsync } from '../hooks/useData';
import { Btn, ErrorBox, Modal } from './common';
import { taskSectionLink } from './taskDetailPresentation';
import './chat.css';

type OpenChat = (id?: string, archived?: boolean) => void;

const accessLost = (error: unknown) => error instanceof ApiError && [401,403,404].includes(error.status);

export function ChatPage({id, archived, canCreateAgent = false, onOpen}: {id?: string; archived:boolean; canCreateAgent?:boolean; onOpen:OpenChat}) {
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
    const focus = evidence.current && document.getElementById(evidence.current);
    if (focus instanceof HTMLElement) {
      evidence.current = undefined;
      focus.focus();
    }
  }, [id]);
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

  async function send() {
    if (!modelAvailable || !draft.trim() || sending || waiting) return;
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
        {' or '}<a href="#/tasks">open Tasks</a>.
      </p>}
      {id && !session && conversation.loading && <p role="status">Loading conversation…</p>}
      {id && conversation.error && <><ErrorBox error={session ? conversation.error : 'Conversation unavailable.'}/>
        <Btn onClick={() => void conversation.reload()}>Retry loading conversation</Btn></>}
      {!id && !archived && <p className="muted">Ask for a task’s current state, follow its work, or prepare a reusable catalog agent.</p>}
      {session?.watches?.filter(watch => !watch.complete).map(watch => <p className="chat-watch" key={watch.requestId}>
        {watch.unknown ? 'Status temporarily unknown' : watch.task.decisionNeeded ? 'Decision required' : `Following task: ${watch.task.state}`} · <a href={taskSectionLink(watch.task.id,'overview')}>{watch.task.id}</a>
        {watch.unknown && <span className="muted"> · Last seen <time dateTime={watch.observedAt}>{new Date(watch.observedAt).toLocaleString()}</time></span>}
      </p>)}
      <div className="chat-transcript" aria-label="Conversation">
        {session?.messages.map(message => <Message key={message.id} value={message}
          now={now} canCreateAgent={canCreateAgent && !sending}
          onDecide={(review, decision) => void mutate(() => api.decideChatAgent(session.id, review.id, review.digest, decision))}
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
        <Btn tone="primary" type="submit" disabled={!modelAvailable || sending || waiting || !draft.trim()}>
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

function Message({value, now, canCreateAgent, onView, onRead, onRetry, onDecide}: {
  value:ChatMessage; now:number; canCreateAgent:boolean; onView:(anchor:string)=>void; onRead:()=>void;
  onRetry?:()=>void; onDecide:(review:ChatAgentReview, decision:'confirm'|'decline')=>void;
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
    {(value.state === 'pending' || value.state === 'interpreting') && <p role="status">Interpreting request…</p>}
    {onRetry && <Btn onClick={onRetry}>Retry saved request</Btn>}
    {value.task && <TaskLink task={value.task} message={value.id} onView={onView}/>}
    {review && <AgentReviewCard review={review} now={now} canConfirm={canCreateAgent}
      onDecide={decision => onDecide(review,decision)} onView={onView}/>}
    {value.unread && <Btn onClick={onRead}>Mark result read</Btn>}
  </article>;
}

function AgentReviewCard({review, now, canConfirm, onDecide, onView}: {
  review:ChatAgentReview; now:number; canConfirm:boolean; onDecide:(decision:'confirm'|'decline')=>void; onView:(anchor:string)=>void;
}) {
  const definition = review.definition;
  const expired = now >= Date.parse(review.expiresAt);
  const anchor = `chat-agent-${review.id}`;
  return <section className="chat-agent-review" aria-label={`Catalog agent ${definition.name}`}>
    <h3>{definition.name}</h3>
    <dl>
      <dt>Agent ID</dt><dd>{review.agentId}</dd>
      <dt>Model</dt><dd>{definition.model.provider} · {definition.model.name}</dd>
      <dt>Runtime</dt><dd>{definition.runtime.kind} · {definition.isolation?.mode} · Read-only workspace</dd>
      <dt>Tool grants</dt><dd>None</dd>
    </dl>
    <h4>Instructions</h4>
    <p>{definition.prompt}</p>
    {review.state === 'awaiting_confirmation' && <>
      <p>This creates a reusable catalog definition. No workflow starts.</p>
      {expired
        ? <p role="status">Confirmation expired. Ask for a new review.</p>
        : <><p>Review expires at <time dateTime={review.expiresAt}>{new Date(review.expiresAt).toLocaleTimeString()}</time>.</p>
          <div className="btn-row">
            <Btn tone="primary" disabled={!canConfirm} onClick={() => onDecide('confirm')}>Create agent</Btn>
            <Btn disabled={!canConfirm} onClick={() => onDecide('decline')}>Decline</Btn>
          </div>
          {canConfirm && <p className="small muted">When this is the only pending review, you can type “Confirm this proposal” or “Decline this proposal”.</p>}</>}
    </>}
    {review.state === 'applying' && <p role="status">Creation accepted. Checking the catalog result…</p>}
    {review.state === 'conflict' && <p role="status">The reviewed configuration changed. Ask for a new review.</p>}
    {review.state === 'unknown' && <p role="status">Creation outcome is temporarily unknown. Check Agents before asking to try again.</p>}
    {review.state === 'declined' && <p role="status">Proposal declined.</p>}
    {review.state === 'created' && <>
      <p role="status">Catalog version {review.resultVersion} created.</p>
      <a id={anchor} href={`#/agents/${encodeURIComponent(review.agentId)}`} onClick={() => onView(anchor)}>View agent</a>
    </>}
  </section>;
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
