import {useCallback,useEffect,useState} from 'react';
import {api,errorMessage} from '../api/client';
import type {DeclaredCheck,WeftEvent} from '../api/types';
import {Btn} from './common';

/** One check.run result recorded by the gateway as event check.completed. */
export interface CheckRunEvidence {
  check:string;command:string;passed:boolean;exitCode:number;timedOut?:boolean;durationMs:number;format?:string;infraError?:boolean;
  failures?:{test?:string;file?:string;line?:number;message?:string}[];failureCount?:number;truncated?:boolean;
  workspace?:{head?:string;state?:string;partial?:boolean;base?:string;checkFilesChanged?:string[]};
}

/** The latest run of one check by one agent replica, judged against later workspace activity. */
export interface CheckRow extends CheckRunEvidence {time:string;nodeId?:string;replica?:number;seq:number;stale:boolean}

/** Workspace scope to judge: one agent replica's worktree. Unset means every agent. */
export interface CheckScope {nodeId:string;replica:number}

const REFRESH_MS=15000;

function isCheckRun(x:unknown):x is CheckRunEvidence {
  const r=x as CheckRunEvidence;
  return !!r&&typeof r.check==='string'&&typeof r.passed==='boolean'&&typeof r.exitCode==='number';
}

function sameScope(ev:WeftEvent,nodeId?:string,replica?:number) {
  return ev.nodeId===nodeId&&(ev.replica??0)===(replica??0);
}

/** A change observed after a run, by any tool but a check itself, or from outside the agent. */
function changedWorkspace(ev:WeftEvent,row:CheckRow) {
  if (ev.type!=='workspace.observed'||ev.seq<=row.seq) return false;
  const data=ev.data as {tool?:string;changes?:unknown[]}|undefined;
  if (data?.tool==='check.run'||!data?.changes?.length) return false;
  return !ev.nodeId||sameScope(ev,row.nodeId,row.replica);
}

/** A later check run in the same worktree that saw a different tree. */
function differentTree(ev:WeftEvent,row:CheckRow) {
  if (ev.type!=='check.completed'||ev.seq<=row.seq||!sameScope(ev,row.nodeId,row.replica)||!isCheckRun(ev.data)) return false;
  const a=row.workspace,b=ev.data.workspace;
  return !!a?.state&&!!b?.state&&(a.head!==b.head||a.state!==b.state);
}

/** latestChecks keeps the latest run of each check per agent replica and marks it stale when the workspace changed after it. */
export function latestChecks(events:WeftEvent[],scope?:CheckScope):CheckRow[] {
  const sorted=[...events].sort((a,b)=>a.seq-b.seq);
  const rows=new Map<string,CheckRow>();
  for (const ev of sorted) {
    if (ev.type!=='check.completed'||!isCheckRun(ev.data)) continue;
    if (scope&&!sameScope(ev,scope.nodeId,scope.replica)) continue;
    rows.set(JSON.stringify([ev.data.check,ev.nodeId??'',ev.replica??0]),{...ev.data,time:ev.time,nodeId:ev.nodeId,replica:ev.replica,seq:ev.seq,stale:false});
  }
  return [...rows.values()].map(row=>({...row,stale:sorted.some(ev=>changedWorkspace(ev,row)||differentTree(ev,row))}));
}

function status(r:CheckRow) {
  if (r.infraError) return 'could not run (sandbox error)';
  if (!r.passed) return r.timedOut?'timed out':'failed';
  return r.stale?'passed, but the workspace changed after this run':'passed';
}

/** CheckEvidence lists every declared check with its recorded outcome; it never infers a result from agent output. */
export function CheckEvidence({graphId,scope}:{graphId:string;scope?:CheckScope}) {
  const [declared,setDeclared]=useState<DeclaredCheck[]>();
  const [events,setEvents]=useState<WeftEvent[]>();
  const [error,setError]=useState<string>();
  const [revision,setRevision]=useState(0);
  const refresh=useCallback(()=>setRevision(r=>r+1),[]);
  useEffect(()=>{
    let cancelled=false;
    setError(undefined);
    Promise.all([api.getGraph(graphId),api.events(graphId,{types:'check.completed,workspace.observed',limit:1000})])
      .then(([summary,evs])=>{if(!cancelled){setDeclared(summary.graph.spec.checks??[]);setEvents(evs.filter(ev=>ev.graphId===graphId));}})
      .catch(cause=>{if(!cancelled)setError(errorMessage(cause));});
    const timer=setTimeout(refresh,REFRESH_MS);
    return ()=>{cancelled=true;clearTimeout(timer);};
  },[graphId,revision,refresh]);
  if (error&&!events) return <>Check results unavailable: {error}. Review recorded commands and their outcomes below. <Btn onClick={refresh}>Retry</Btn></>;
  if (!events||!declared) return <>Loading declared checks…</>;
  const rows=latestChecks(events,scope);
  const names=[...declared.map(c=>c.name),...rows.map(r=>r.check).filter(n=>!declared.some(c=>c.name===n))].filter((n,i,all)=>all.indexOf(n)===i);
  if (!names.length) return <>Not independently verified. This task declares no checks; review recorded commands and their outcomes below.</>;
  const verdict=(name:string)=>{
    const runs=rows.filter(r=>r.check===name);
    if (!runs.length) return 'not run';
    if (runs.some(r=>!r.passed)) return 'failed';
    return runs.some(r=>r.stale)?'stale':'passed';
  };
  const counts={passed:0,failed:0,stale:0,'not run':0} as Record<string,number>;
  names.forEach(n=>counts[verdict(n)]++);
  const changedTests=[...new Set(rows.filter(r=>r.passed).flatMap(r=>r.workspace?.checkFilesChanged??[]))];
  const summary=counts.passed===names.length
    ?`All ${names.length} declared checks passed on the current workspace.`
    :`${counts.passed} of ${names.length} declared checks passed on the current workspace`+[['failed','failed'],['stale','need a rerun after later changes'],['not run','were not run']].filter(([k])=>counts[k]).map(([k,label])=>`; ${counts[k]} ${label}`).join('')+'.';
  return <>
    {summary}{' '}<Btn onClick={refresh}>Refresh checks</Btn>
    {error&&<> Refresh failed: {error}.</>}
    {!!changedTests.length&&<p role="note">Test or check-definition files changed in this task: {changedTests.join(', ')}. A pass does not show that these changes are right; review them.</p>}
    <ul aria-label="Declared checks">{names.map(name=>{
      const runs=rows.filter(r=>r.check===name);
      const command=declared.find(c=>c.name===name)?.command??runs[0]?.command;
      return <li key={name}><strong>{name}</strong> {command&&<code>{command}</code>}
        {!runs.length?<> · not run</>:<ul aria-label={`${name} runs`}>{runs.map(r=><li key={`${r.nodeId}:${r.replica}`}>
          {status(r)} · exit {r.exitCode}{r.nodeId?` · ${r.nodeId}#${r.replica??0}`:''}{r.workspace?.head?` · at ${r.workspace.head.slice(0,10)}${r.workspace.state?`+${r.workspace.state.slice(0,8)}`:''}`:' · workspace state unknown'}
          {!r.passed&&!!r.failures?.length&&<ul aria-label={`${name} failures`}>{r.failures.map((f,i)=><li key={i}>{[f.test,f.file&&(f.line?`${f.file}:${f.line}`:f.file)].filter(Boolean).join(' ')}{f.message?`: ${f.message}`:''}</li>)}{(r.failureCount??0)>r.failures.length&&<li>{(r.failureCount??0)-r.failures.length} more not shown</li>}</ul>}
        </li>)}</ul>}
      </li>;})}</ul>
  </>;
}
