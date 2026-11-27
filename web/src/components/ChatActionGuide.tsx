import {useEffect, useId, useLayoutEffect, useRef, useState} from 'react';
import {api, errorMessage} from '../api/client';
import type {ChatCandidate, ChatCandidates, ChatPickerArea} from '../api/chat';
import {guideAction, guideActions, moreCategories, primaryCategories, type ChatCategory, type GuideDraft} from './chatGuide';

function CandidateSearch({area, query, onQuery, onPick, restoreId}: {
  area:ChatPickerArea; query:string; restoreId?:string; onQuery:(query:string)=>void; onPick:(target:ChatCandidate)=>void;
}) {
  const list = useRef<HTMLUListElement>(null);
  const search = useRef<HTMLInputElement>(null);
  const returnTo = useRef(restoreId);
  const [attempt, setAttempt] = useState(0);
  const [lookup, setLookup] = useState<{query:string; attempt:number; result?:ChatCandidates; error?:string}>();
  useEffect(() => {
    const controller = new AbortController();
    const timer = window.setTimeout(async () => {
      try {
        const result = await api.chatCandidates(area, query, controller.signal);
        if (!controller.signal.aborted) setLookup({query,attempt,result});
      } catch (error) {
        if (!controller.signal.aborted) setLookup({query,attempt,error:errorMessage(error)});
      }
    }, query ? 180 : 0);
    return () => {clearTimeout(timer); controller.abort();};
  }, [area, query, attempt]);
  const current = lookup?.query === query && lookup.attempt === attempt ? lookup : undefined;
  useLayoutEffect(() => {
    if (current && returnTo.current) {
      if (document.activeElement === search.current) {
        Array.from(list.current?.querySelectorAll('button') ?? [])
          .find(button => button.dataset.id === returnTo.current)?.focus();
      }
      returnTo.current = undefined;
    }
  }, [current]);
  return <>
    <label>Search {area}<input ref={search} type="search" value={query} maxLength={160} data-guide-focus="search"
      onChange={event => onQuery(event.target.value)}/></label>
    <div role="status">
      {!current ? 'Searching…' : current.error ? 'Search unavailable. ' : current.result?.partial
        ? 'Search incomplete. Refine your search or choose a displayed result.'
        : !current.result?.items.length ? 'No matches. Try another name or ID.' : ''}
      {current?.error && <><span>{current.error}</span> <button type="button" onClick={()=>setAttempt(n=>n+1)}>Retry search</button></>}
    </div>
    {!!current?.result?.items.length && <ul ref={list} className="guide-results" aria-label="Matching resources">
      {current.result.items.map(candidate=><li key={candidate.id}><button type="button" data-id={candidate.id}
        onClick={()=>onPick(candidate)}>{candidate.label}</button></li>)}
    </ul>}
  </>;
}

export function ChatActionGuide({draft, admin, onChange}: {
  draft:GuideDraft; admin:boolean; onChange:(draft:GuideDraft)=>void;
}) {
  const groupId = useId();
  const root = useRef<HTMLElement>(null);
  const focusAfter = useRef<string | null>(null);
  const lastCandidate = useRef<string | undefined>(undefined);
  const returnLink = useRef<HTMLAnchorElement | null>(null);
  const action = guideAction(draft,admin);
  const category = draft.category;
  const target = draft.target;
  const picking = action?.kind === 'pick' || action?.kind === 'project';

  // Chat stays mounted in an Activity while a manual editor is open.
  useLayoutEffect(() => {
    returnLink.current?.focus();
    returnLink.current = null;
  }, []);
  useLayoutEffect(() => {
    if (!focusAfter.current) return;
    const key = focusAfter.current;
    focusAfter.current = null;
    Array.from(root.current?.querySelectorAll<HTMLElement>('[data-guide-focus]') ?? [])
      .find(el => el.dataset.guideFocus === key)?.focus();
  }, [draft]);
  function change(next:GuideDraft, focus:string) {
    focusAfter.current = focus;
    onChange(next);
  }
  function back() {
    if (target && picking) {
      lastCandidate.current = target.id;
      change({...draft,target:undefined}, 'search');
    } else if (action && category) {
      change({category,more:draft.more}, action.label);
    } else if (category) {
      change({more:draft.more}, category);
    } else {
      change({}, 'Mais');
    }
  }
  function categoryButton(item: ChatCategory) {
    return <button type="button" key={item} data-guide-focus={item}
      onClick={()=>change({category:item,more:moreCategories.some(c=>c===item)}, 'heading')}>{item}</button>;
  }
  return <section ref={root} className="chat-guide" aria-label="Chat actions"
    onKeyDown={event=>{
      if (event.key === 'Escape' && (category || draft.more)) {event.preventDefault();back();}
    }}>
    {!category ? <>
      <div className="guide-pills">{primaryCategories.map(categoryButton)}
        <button type="button" data-guide-focus="Mais" aria-expanded={!!draft.more} aria-controls={groupId}
          onClick={()=>change({more:!draft.more},'Mais')}>Mais</button>
      </div>
      <div id={groupId} hidden={!draft.more}><div className="guide-pills">{moreCategories.map(categoryButton)}</div></div>
    </> : <>
      <div className="guide-heading">
        <strong tabIndex={-1} data-guide-focus="heading" aria-describedby={target ? groupId+'-target' : undefined}>{action ? action.label : category}</strong>
        <button type="button" onClick={back}>Back</button>
      </div>
      {!action && <div className="guide-pills">
        {guideActions(category,admin).map(item=>item.kind==='link'
          ? <a key={item.label} href={item.href} onClick={event=>{returnLink.current=event.currentTarget;}}>{item.label}</a>
          : <button type="button" key={item.label} data-guide-focus={item.label}
              onClick={()=>{
                lastCandidate.current = undefined;
                change({...draft,action:item.label},item.kind==='prompt'?'heading':'search');
              }}>{item.label}</button>)}
      </div>}
      {picking && !target && <CandidateSearch key={action.area} area={action.area} restoreId={lastCandidate.current} query={draft.query ?? ''}
        onQuery={query=>onChange({...draft,query})} onPick={target=>change({...draft,target},'heading')}/>}
      {target && picking && <p><span id={groupId+'-target'}>{target.label}</span> <button type="button" onClick={()=>{
        lastCandidate.current = undefined;
        change({...draft,target:undefined},'search');
      }}>Change selection</button></p>}
      {action?.kind==='project' && target && <a href={'#/projects?project='+encodeURIComponent(target.id)}
        onClick={event=>{returnLink.current=event.currentTarget;}}>Open project settings</a>}
      {(target || action?.kind==='prompt') && <button type="button" onClick={()=>change({}, 'Tasks')}>Clear selection</button>}
    </>}
  </section>;
}
