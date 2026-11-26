import { useEffect, useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { ChatSessionSummary } from '../api/chat';
import { Btn, ErrorBox, Modal } from './common';

export function ChatSessionSearch({archived,onClose,onSelect}:{
  archived:boolean; onClose:()=>void; onSelect:(id:string,archived:boolean)=>void;
}) {
  const [query,setQuery] = useState('');
  const [scope,setScope] = useState(archived);
  const [items,setItems] = useState<ChatSessionSummary[]>([]);
  const [loading,setLoading] = useState(true);
  const [error,setError] = useState('');
  const [retry,setRetry] = useState(0);
  const term = query.trim().toLocaleLowerCase();
  const scanOlder = term.length > 0;

  useEffect(() => {
    let closed = false;
    async function load() {
      setLoading(true);
      setError('');
      setItems([]);
      const found = new Map<string,ChatSessionSummary>();
      let after: string | undefined;
      try {
        do {
          const page = await api.chatSessions(scope,after);
          if (closed) return;
          for (const item of page.items) found.set(item.id,item);
          setItems([...found.values()]);
          after = page.next;
        } while (scanOlder && after);
      } catch (cause) {
        if (!closed) setError(errorMessage(cause));
      } finally {
        if (!closed) setLoading(false);
      }
    }
    void load();
    return () => { closed = true; };
  }, [scope,scanOlder,retry]);
  const matches = items.filter(item => (item.title || 'New chat').toLocaleLowerCase().includes(term));
  return <Modal title="Search conversations" keyboard onClose={onClose}>
    <label className="chat-search-field">Session title
      <input type="search" autoFocus placeholder="Search by title" value={query} onChange={event=>setQuery(event.target.value)}/>
    </label>
    <div className="chat-list-tabs" aria-label="Search scope">
      <button type="button" aria-pressed={!scope} onClick={()=>setScope(false)}>Recents</button>
      <button type="button" aria-pressed={scope} onClick={()=>setScope(true)}>Archived</button>
    </div>
    {error && <><ErrorBox error={error}/><Btn onClick={()=>setRetry(retry+1)}>Retry search</Btn></>}
    {loading && <p role="status">Searching conversations…</p>}
    {!loading && !error && !matches.length && <p role="status">No matching conversations.</p>}
    <ul className="chat-search-results">{matches.map(item=><li key={item.id}>
      <button type="button" onClick={()=>onSelect(item.id,scope)}>{item.title || 'New chat'}</button>
    </li>)}</ul>
  </Modal>;
}
