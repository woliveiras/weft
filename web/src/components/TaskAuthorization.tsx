import {useState} from 'react';
import {api,errorMessage} from '../api/client';
import type {AuthorizationMode} from '../api/tasks';
import {useAsync} from '../hooks/useData';
import {Btn,ErrorBox} from './common';
export function TaskAuthorization({taskId,canEdit,onChanged,running=false}:{taskId:string;canEdit:boolean;onChanged:()=>void;running?:boolean}) {
 const state=useAsync(()=>api.taskAuthorization(taskId),[taskId]);
 const [mode,setMode]=useState<AuthorizationMode>('ask');
 const [remember,setRemember]=useState(false);
 const [confirmed,setConfirmed]=useState(false);
 const [busy,setBusy]=useState(false);
 const [error,setError]=useState<string>();
 const save=async()=>{if(!state.data||!confirmed)return;setBusy(true);setError(undefined);try{await api.saveTaskAuthorization(taskId,mode,state.data.scope,state.data.record?.revision??0,remember,state.data.preference?.revision??0);await state.reload();setConfirmed(false);onChanged();}catch(e){setError(errorMessage(e));}finally{setBusy(false);}};
 return <section className="card" aria-label="Task authorization"><h2>Authorization</h2>
 <p>Current mode: {state.error?'unavailable':state.data?.record?.mode??(running?'Existing workflow approvals (legacy task)':'Ask for approval (default)')}. Tool grants and mandatory approvals remain unchanged.</p>
 <details><summary>Reviewed agents, tools and execution boundaries</summary>{state.data?.capabilities?.map(agent=><section key={agent.id}><h3>{agent.id}</h3><p>Isolation: {agent.isolation}</p><pre>{JSON.stringify(agent.grants,null,2)}</pre></section>)}</details>
 <ErrorBox error={state.error||error}/>
 {state.data?.record&&state.data.record.scope!==state.data.scope&&<p role="alert">Saved authorization is stale. Review the current scope and confirm again before starting.</p>}
 <Btn disabled={busy} onClick={()=>{setConfirmed(false);void state.reload();}}>Refresh authorization</Btn>
 {canEdit&&(!running||state.data?.record)&&<><label>Authorization mode<select value={mode} onChange={e=>{setMode(e.target.value as AuthorizationMode);setConfirmed(false);}}><option value="ask">Ask for approval</option><option value="workspace" disabled={running&&state.data?.record?.mode!=="full"}>Workspace access</option><option value="full" disabled={running}>Full access</option></select></label>
 <p>{mode==='ask'?'Ask before each granted tool call.':mode==='workspace'?'Automatically allow workspace filesystem tools, Git reads and sandbox commands. Other calls ask. Native shell cannot be confined to a worktree and is refused.':'Run granted tools without repeated task prompts. Native commands can access files and network available to the Weft OS user, including outside the worktree. Existing grants may permit external actions, commit and push. Sandbox limits and mandatory gates still apply.'}</p>
 {state.data?.preference&&<p>Project suggestion: {state.data.preference.mode}. Select and confirm it explicitly; it is never applied automatically.</p>}
 {state.data?.projectId&&!running&&<label><input type="checkbox" checked={remember} onChange={e=>setRemember(e.target.checked)}/>Remember this mode as a project suggestion</label>}
 {running&&<p>Only reduction is allowed while running. Waiting calls recheck consent. Already running operations may finish; Stop task requests cancellation without undoing external effects.</p>}
 <p>Execution happens on the Weft server machine, using the reviewed workflow environment and permissions. Saving changes to the task or workflow requires renewed consent.</p>
 <label><input type="checkbox" checked={confirmed} onChange={e=>setConfirmed(e.target.checked)}/>I reviewed this task, its workflow permissions, execution location, file and network access, external actions and commit/push scope.</label>
 <Btn disabled={!confirmed||busy||!state.data||!!state.error} onClick={()=>void save()}>Confirm authorization</Btn></>}
 </section>;
}
