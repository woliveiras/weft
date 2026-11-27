import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { Btn } from './common';
import './chatComposer.css';

const searchable = ['tasks','workflows','nodes','agents','projects','executions','skills','approvals','triggers','review-routes','mcp','scm','issue-connections','models','plugins','tools','budgets'];
const inspectable = ['tasks','workflows','agents','projects','skills','approvals','activity','triggers','review-routes','mcp','scm','issue-connections','models','chat-model','plugins','tools','delegation','network','retention','usage','prices','budgets','execution'];
type Suggestion = {value:string; label:string; detail?:string};

function suggestions(input: string, admin: boolean, choicePending: boolean): Suggestion[] {
  if (!input.startsWith('/') || /[\r\n]/.test(input)) return [];
  const [command, resource, ...args] = input.toLowerCase().split(/\s+/);
  if (resource === undefined) {
    return [
      {value:'/inspect ', label:'/inspect', detail:'Inspect installation state'},
      {value:'/find ', label:'/find', detail:'Find a resource by name'},
      ...(choicePending ? [{value:'/choose ',label:'/choose',detail:'Choose a displayed result'}] : []),
      ...(admin ? [{value:'/export',label:'/export',detail:'Download installation configuration'}] : []),
    ].filter(item => item.label.startsWith(command));
  }
  if (args.length || (command !== '/inspect' && command !== '/find')) return [];
  return (command === '/find' ? searchable : inspectable)
    .filter(area => area.startsWith(resource) && area !== resource)
    .map(area => ({label:command+' '+area, value:command+' '+area+(command === '/find' || area === 'execution' ? ' ' : '')}));
}

export function ChatComposer({draft, hasContext = false, preparedAction, detailsHint, onChange, onSend, disabled, sending, admin, choicePending}: {
  draft:string; hasContext?:boolean; preparedAction?:string; detailsHint?:string; onChange:(draft:string)=>void; onSend:()=>void; disabled:boolean; sending:boolean; admin:boolean; choicePending:boolean;
}) {
  const [focused,setFocused] = useState(false);
  const [dismissed,setDismissed] = useState(false);
  const [index,setIndex] = useState(0);
  const field = useRef<HTMLTextAreaElement>(null);
  const id = useId();
  const [editingDetailsFor,setEditingDetailsFor] = useState<string>();
  const compact = !!preparedAction && !draft && editingDetailsFor !== preparedAction;
  useLayoutEffect(() => {
    if (preparedAction && editingDetailsFor === preparedAction) field.current?.focus();
  }, [editingDetailsFor,preparedAction]);
  const options = focused && !dismissed && !hasContext ? suggestions(draft,admin,choicePending) : [];
  const active = Math.min(index, options.length - 1);
  const canSend = !disabled && (hasContext || !!draft.trim());
  useEffect(() => {
    if (active >= 0) document.getElementById(id+'-'+active)?.scrollIntoView?.({block:'nearest'});
  }, [id,active,draft]);

  function choose(option: Suggestion) {
    onChange(option.value);
    setIndex(0);
    setDismissed(option.value === '/export' || !option.value.endsWith(' '));
    field.current?.focus();
  }

  return <form className="chat-composer" aria-label="Compose message" onKeyDownCapture={event => {
    if (event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229) return;
    if (event.key === 'Enter' && (event.ctrlKey || event.metaKey)) {
      event.preventDefault();
      event.stopPropagation();
      if (canSend && !event.repeat) event.currentTarget.requestSubmit();
    }
  }} onSubmit={event => {
    event.preventDefault();
    if (canSend) { setDismissed(true); onSend(); }
  }}>
    {options.length > 0 && <ul id={id} role="listbox" aria-label="Command suggestions" className="chat-command-menu">
      {options.map((option,i) => <li key={option.label} id={id+'-'+i} role="option" aria-selected={active===i}
        onMouseDown={event => event.preventDefault()} onClick={() => choose(option)}>
        <strong>{option.label}</strong>{option.detail && <span>{option.detail}</span>}
      </li>)}
    </ul>}
    {detailsHint && <span className="small" id={id+'-hint'}>{detailsHint}</span>}
    <textarea hidden={compact} id={id+'-message'} aria-describedby={detailsHint ? id+'-hint' : undefined} disabled={sending} ref={field} aria-label="Message" aria-autocomplete="list" aria-controls={options.length ? id : undefined}
      aria-activedescendant={active >= 0 ? id+'-'+active : undefined} aria-keyshortcuts="Control+Enter Meta+Enter"
      placeholder={detailsHint ? undefined : hasContext ? "Add details (optional)" : "Ask Weft or type / for commands"} value={draft} rows={2} maxLength={4096}
      onChange={event => {onChange(event.target.value); setIndex(0); setDismissed(false);}}
      onFocus={() => setFocused(true)} onBlur={() => setFocused(false)}
      onKeyDown={event => {
        if (event.nativeEvent.isComposing || event.nativeEvent.keyCode===229) return;
        if (!options.length) return;
        if (event.key === 'Escape') {event.preventDefault(); setDismissed(true);}
        if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
          event.preventDefault();
          setIndex((active + (event.key==='ArrowDown' ? 1 : -1) + options.length) % options.length);
        }
        if ((event.key === 'Enter' || event.key === 'Tab') && !event.shiftKey) {
          event.preventDefault();
          choose(options[active]);
        }
      }}/>
    <div className="chat-composer-actions">
      {compact ? <button type="button" className="guide-add-details" aria-controls={id+'-message'}
        onClick={()=>setEditingDetailsFor(preparedAction)}>Add details</button> : <span className="muted">{/Mac|iPhone|iPad/.test(navigator.platform) ? '⌘' : 'Ctrl'} + Enter to send</span>}
      <Btn tone="primary" type="submit" disabled={!canSend}>{sending ? 'Sending…' : preparedAction ?? 'Send'}</Btn>
    </div>
  </form>;
}
