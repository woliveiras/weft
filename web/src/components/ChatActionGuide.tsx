import {useEffect, useId, useLayoutEffect, useRef, useState} from 'react';
import {Badge} from './common';
import {statusLabel, statusTones} from './taskStatus';
import {api, errorMessage} from '../api/client';
import type {ChatCandidate, ChatCandidates, ChatPickerArea} from '../api/chat';
import {guideAction, guideActions, moreCategories, primaryCategories, type ChatCategory, type GuideDraft} from './chatGuide';

function compactTaskId(id:string, candidates:ChatCandidate[]) {
  let length = 6;
  while (length < id.length && candidates.some(other => other.id !== id && other.id.endsWith(id.slice(-length)))) length++;
  return length < id.length ? '…'+id.slice(-length) : id;
}

function TaskIdentity({candidate, reference}:{candidate:ChatCandidate; reference:string}) {
  const task = candidate.task;
  if (!task) return <>{candidate.label}</>;
  return <span className="guide-task-identity">
    <strong>{task.title}</strong>{' '}
    <Badge tone={statusTones[task.status]}>{statusLabel(task.status)}</Badge>{' '}
    <small>{task.projectId && <span>Project {task.projectId} ·</span>}{' '}<code>{reference}</code></small>
  </span>;
}

function searchStatus(result:ChatCandidates, tasks:boolean) {
  const count = result.items.length;
  if (tasks && result.partial) return count
    ? `Showing ${count} ${count === 1 ? 'task' : 'tasks'}. Search by name or ID to narrow the list.`
    : 'No matches in the tasks checked. Try another name or the full task ID.';
  if (result.partial) return 'Search incomplete. Refine your search or choose a displayed result.';
  if (!count) return 'No matches. Try another name or ID.';
  return count === 1 ? '1 match' : count+' matches';
}

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
        const result = await api.chatCandidates(area, query.trim(), controller.signal);
        if (!controller.signal.aborted) setLookup({query,attempt,result});
      } catch (error) {
        if (!controller.signal.aborted) setLookup({query,attempt,error:errorMessage(error)});
      }
    }, query ? 180 : 0);
    return () => {clearTimeout(timer); controller.abort();};
  }, [area, query, attempt]);
  const current = lookup?.query === query && lookup.attempt === attempt ? lookup : undefined;
  const items = current?.result?.items ?? [];
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
    <label>Search {area}<input placeholder={area === 'tasks' ? 'Task name or ID' : undefined} ref={search} type="search" value={query} maxLength={160} data-guide-focus="search"
      onChange={event => onQuery(event.target.value)}/></label>
    <div role="status">
      {current?.result ? searchStatus(current.result, area === 'tasks') : current?.error ? 'Search unavailable. ' : 'Searching…'}
      {current?.error && <><span>{current.error}</span> <button type="button" onClick={()=>setAttempt(n=>n+1)}>Retry search</button></>}
    </div>
    {items.length > 0 && <ul ref={list} className="guide-results" aria-label="Matching resources">
      {items.map(candidate=><li key={candidate.id}><button type="button" data-id={candidate.id}
        onClick={()=>onPick(candidate)}>{area === 'tasks' && candidate.task
          ? <TaskIdentity candidate={candidate} reference={compactTaskId(candidate.id, items)}/> : candidate.label}</button></li>)}
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

  const taskPicker = action?.kind === 'pick' && action.area === 'tasks';
  const reviewingTask = taskPicker && !!target;

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
  return <section ref={root} className={'chat-guide'+(taskPicker ? reviewingTask ? ' guide-task-review' : ' guide-task-search' : '')} aria-label="Chat actions"
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
        <strong tabIndex={-1} data-guide-focus="heading" aria-describedby={target ? groupId+'-target' : undefined}>{taskPicker && !target ? action.follow ? 'Which task do you want to follow?' : 'Which task are you looking for?' : action ? action.label : category}</strong>
        {!reviewingTask && <button type="button" onClick={back}>Back</button>}
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
      {reviewingTask && <div id={groupId+'-target'}><TaskIdentity candidate={target} reference={target.id}/></div>}
      {reviewingTask && <button type="button" onClick={back}>Change task</button>}
      {target && picking && !reviewingTask && <p><span id={groupId+'-target'}>{target.label}</span> <button type="button" onClick={()=>{
        lastCandidate.current = undefined;
        change({...draft,target:undefined},'search');
      }}>Change selection</button></p>}
      {action?.kind==='project' && target && <a href={'#/projects?project='+encodeURIComponent(target.id)}
        onClick={event=>{returnLink.current=event.currentTarget;}}>Open project settings</a>}
      {(target || action?.kind==='prompt') && <button type="button" onClick={()=>change({}, 'Tasks')}>Clear selection</button>}
    </>}
  </section>;
}
