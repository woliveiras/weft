import { HelpField } from './FieldHelp';
import {useState} from 'react';
import {api,ApiError,errorMessage} from '../api/client';
import type {AuthorizationMode} from '../api/tasks';
import {useAsync} from '../hooks/useData';
import {Btn,ErrorBox} from './common';
export function TaskAuthorization({taskId,canEdit,onChanged,running=false}:{taskId:string;canEdit:boolean;onChanged:()=>void;running?:boolean}) {
 const state=useAsync(async()=>{
  try{return await api.taskAuthorization(taskId);}
  catch(cause){
   if(cause instanceof ApiError && cause.status===409) {
    throw new Error('This task uses a different workflow version from the current one. Its authorization review is unavailable.');
   }
   throw cause;
  }
 },[taskId]);
 const [mode,setMode]=useState<AuthorizationMode>('ask');
 const [remember,setRemember]=useState(false);
 const [confirmed,setConfirmed]=useState(false);
 const [busy,setBusy]=useState(false);
 const [error,setError]=useState<string>();
 const save=async()=>{if(!state.data||!confirmed)return;setBusy(true);setError(undefined);try{await api.saveTaskAuthorization(taskId,mode,state.data.scope,state.data.record?.revision??0,remember,state.data.preference?.revision??0);await state.reload();setConfirmed(false);onChanged();}catch(e){setError(errorMessage(e));}finally{setBusy(false);}};
 const capabilities=state.data?.capabilities;
 return <section className="card task-authorization" aria-label="Task authorization"><h2>Authorization</h2>
 <p>Current mode: {state.error?'unavailable':state.data?.record?.mode??(running?'Existing workflow approvals (legacy task)':'Ask for approval (default)')}. Tool grants and mandatory approvals remain unchanged.</p>
 {capabilities?.length ? <details><summary>Reviewed agents, tools and execution boundaries</summary>{capabilities.map(agent=><section className="task-access-agent" key={agent.id}><h3>{agent.id}</h3><p>Isolation: {agent.isolation}</p><ul aria-label={`Tools granted to ${agent.id}`}>{(agent.grants??[]).map((grant,index)=><li key={index}><code>{grant.name}</code>{grant.commands?.length ? <span> · Commands: {grant.commands.join(', ')}</span> : null}</li>)}</ul><details><summary>View raw grant data</summary><pre>{JSON.stringify(agent.grants ?? [],null,2)}</pre></details></section>)}</details>
  : <p className="muted">{state.loading?'Loading authorization details…':state.error?'Could not load reviewed agent details.':'No reviewed agent details are available for this task.'}</p>}
 <ErrorBox error={state.error||error}/>
 {state.data?.record&&state.data.record.scope!==state.data.scope&&<p role="alert">Saved authorization is stale. Review the current scope and confirm again before starting.</p>}
 <Btn disabled={busy} onClick={()=>{setConfirmed(false);void state.reload();}}>Refresh authorization</Btn>
 {canEdit&&(!running||state.data?.record)&&<><HelpField label={"Authorization mode"} help={"Ask prompts for gated actions. Workspace access covers reviewed worktree operations. Full access preauthorizes only reviewed grants within task scope; mandatory approvals and installation rules still apply."}><select value={mode} onChange={e=>{setMode(e.target.value as AuthorizationMode);setConfirmed(false);}}><option value="ask">Ask for approval</option><option value="workspace" disabled={running&&state.data?.record?.mode!=="full"}>Workspace access</option><option value="full" disabled={running}>Full access</option></select></HelpField>
 <p>{mode==='ask'?'Ask before each granted tool call.':mode==='workspace'?'Automatically allow workspace filesystem tools, Git reads and Docker sandbox commands. Other calls ask. Process and Kubernetes isolation both use native shell. Workspace access refuses native shell because it can reach outside the worktree.':'Run granted tools without repeated task prompts. Native commands can access files and network available to the Weft OS user, including outside the worktree. Existing grants may permit external actions, commit and push. Sandbox limits and mandatory gates still apply.'}</p>
 {state.data?.preference&&<p>Project suggestion: {state.data.preference.mode}. Select and confirm it explicitly; it is never applied automatically.</p>}
 {state.data?.projectId&&!running&&<label><input type="checkbox" checked={remember} onChange={e=>setRemember(e.target.checked)}/>Remember this mode as a project suggestion</label>}
 {running&&<p>Only reduction is allowed while running. Waiting calls recheck consent. Already running operations may finish; Stop task requests cancellation without undoing external effects.</p>}
 <p>Execution happens on the Weft server machine, using the reviewed workflow environment and permissions. Saving changes to the task or workflow requires renewed consent.</p>
 <label><input type="checkbox" checked={confirmed} onChange={e=>setConfirmed(e.target.checked)}/>I reviewed this task, its workflow permissions, execution location, file and network access, external actions and commit/push scope.</label>
 <Btn disabled={!confirmed||busy||!state.data||!!state.error} onClick={()=>void save()}>Confirm authorization</Btn></>}
 </section>;
}
