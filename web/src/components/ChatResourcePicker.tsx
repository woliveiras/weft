import {useEffect, useId, useLayoutEffect, useRef, useState} from 'react';
import {Badge} from './common';
import {statusLabel, statusTones} from './taskStatus';
import {api, errorMessage} from '../api/client';
import type {ChatCandidate, ChatCandidates, ChatPickerArea} from '../api/chat';
function compactTaskId(id:string, candidates:ChatCandidate[]) {
  let length = 6;
  while (length < id.length && candidates.some(other => other.id !== id && other.id.endsWith(id.slice(-length)))) length++;
  return length < id.length ? '…'+id.slice(-length) : id;
}

function ResourceIdentity({candidate, candidates}:{candidate:ChatCandidate; candidates?:ChatCandidate[]}) {
  const {id, label, task, catalog} = candidate;
  if (!task && !catalog) return <>{label}</>;
  const title = task?.title || catalog?.name || id;
  let badge;
  if (task) badge = <Badge tone={statusTones[task.status]}>{statusLabel(task.status)}</Badge>;
  else if (catalog?.version !== undefined) badge = <Badge>Version {catalog.version}</Badge>;
  else if (catalog?.revision !== undefined) badge = <Badge>Revision {catalog.revision}</Badge>;
  const reference = task && candidates ? compactTaskId(id, candidates) : id;
  return <span className="guide-resource-identity">
    <strong>{title}</strong>{' '}{badge}{' '}
    <small>{task?.projectId && <span>Project {task.projectId} ·</span>}{' '}<code>{reference}</code></small>
  </span>;
}

function searchStatus(result:ChatCandidates, area:ChatPickerArea, noun:string) {
  const count = result.items.length;
  if (result.partial) return count
    ? `Showing ${count} ${count === 1 ? noun : area}. Search by name or ID to narrow the list.`
    : `No matches in the ${area} checked. Try another name or the full ${noun} ID.`;
  if (!count) return 'No matches. Try another name or ID.';
  return count === 1 ? '1 match' : count+' matches';
}

function CandidateSearch({area, noun, query, onQuery, onPick, restoreId}: {
  area:ChatPickerArea; noun:string; query:string; restoreId?:string; onQuery:(query:string)=>void; onPick:(target:ChatCandidate)=>void;
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
    <label>Search {area}<input placeholder={noun+' name or ID'} ref={search} type="search" value={query} maxLength={160} data-guide-focus="search"
      onChange={event => onQuery(event.target.value)}/></label>
    <div role="status">
      {current?.result ? searchStatus(current.result, area, noun.toLowerCase()) : current?.error ? 'Search unavailable. ' : 'Searching…'}
      {current?.error && <><span>{current.error}</span> <button type="button" onClick={()=>setAttempt(n=>n+1)}>Retry search</button></>}
    </div>
    {items.length > 0 && <ul ref={list} className="guide-results" aria-label="Matching resources">
      {items.map(candidate=><li key={candidate.id}><button type="button" data-id={candidate.id}
        onClick={()=>onPick(candidate)}><ResourceIdentity candidate={candidate} candidates={items}/></button></li>)}
    </ul>}
  </>;
}

const resourceNames:Record<ChatPickerArea,string> = {
  tasks:'Task', agents:'Agent', workflows:'Workflow', projects:'Project', skills:'Skill',
};

interface PickerProps {
  area: ChatPickerArea;
  label: string;
  question?: string;
  query: string;
  target?: ChatCandidate;
  restoreId?: string;
  onBack: () => void;
  onQuery: (query:string) => void;
  onPick: (target:ChatCandidate) => void;
}

export function ChatResourcePicker({area, label, question, query, target, restoreId, onBack, onQuery, onPick}:PickerProps) {
  const summaryId = useId();
  const noun = resourceNames[area];
  const heading = target ? label : question ?? `Which ${noun.toLowerCase()} are you looking for?`;
  return <>
    <div className="guide-heading">
      <strong tabIndex={-1} data-guide-focus="heading" aria-describedby={target ? summaryId : undefined}>{heading}</strong>
      {!target && <button type="button" onClick={onBack}>Back</button>}
    </div>
    {target ? <>
      <div id={summaryId}><ResourceIdentity candidate={target}/></div>
      <button type="button" onClick={onBack}>Change {noun.toLowerCase()}</button>
    </> : <CandidateSearch area={area} noun={noun} query={query} restoreId={restoreId} onQuery={onQuery} onPick={onPick}/>}
  </>;
}
