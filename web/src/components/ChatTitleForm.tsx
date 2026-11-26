import { useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { ChatSession } from '../api/chat';
import { Btn, ErrorBox } from './common';

export function ChatTitleForm({id,previous,onSaved,onCancel}:{
  id:string; previous:string; onSaved:(session:ChatSession)=>void; onCancel:()=>void;
}) {
  const [title,setTitle] = useState(previous === 'Untitled chat' ? '' : previous);
  const [busy,setBusy] = useState(false);
  const [error,setError] = useState('');
  const trimmed = title.trim();
  async function save() {
    if (busy || !trimmed || trimmed===previous) return;
    setBusy(true);
    setError('');
    try {
      onSaved(await api.renameChat(id,previous,trimmed));
    } catch (cause) {
      setError(errorMessage(cause));
    } finally { setBusy(false); }
  }
  return <form className="chat-title-form" onSubmit={event=>{event.preventDefault();void save();}}>
    <label>Conversation title<input autoFocus required maxLength={120} value={title} onChange={event=>setTitle(event.target.value)}/></label>
    {error && <ErrorBox error={error}/>}
    <div className="btn-row"><Btn type="submit" tone="primary" disabled={busy || !trimmed || trimmed===previous}>Save title</Btn>
      <Btn onClick={onCancel} disabled={busy}>Cancel</Btn></div>
  </form>;
}
