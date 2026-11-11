import {useState} from 'react';
import {api,errorMessage} from '../api/client';
import type {TaskDraft,TaskReview} from '../api/tasks';
import type {EnvironmentView} from '../api/types';
import {Btn,ErrorBox} from './common';
export function TaskEnvironment({task,environment,canControl,onChanged}:{task:TaskDraft;environment:EnvironmentView;canControl:boolean;onChanged:()=>void}) {
 const [review,setReview]=useState<TaskReview>();const [confirmed,setConfirmed]=useState(false);const [busy,setBusy]=useState(false);const [error,setError]=useState<string>();
 const inspect=async()=>{setBusy(true);setError(undefined);setReview(undefined);setConfirmed(false);try{setReview(await api.reviewTask(task.id));}catch(e){setError(errorMessage(e));}finally{setBusy(false);}};
 const act=async(build:boolean)=>{if(!confirmed)return;setBusy(true);setError(undefined);try{if(build){await api.prepareTaskEnvironment(task.id,task.revision);}else if(review){await api.startTask(task.id,review.task.revision);}setReview(undefined);onChanged();}catch(e){setError(errorMessage(e));onChanged();}finally{setConfirmed(false);setBusy(false);}};
 return <section aria-label="Task environment preparation"><h3>Environment preparation</h3><p>Environment: {environment.state}{environment.phase?` · ${environment.phase}`:''}. Agents do not start automatically.</p><ErrorBox error={error}/><ErrorBox error={environment.error}/><details><summary>Build diagnostics</summary><pre>{environment.log||'No build log recorded.'}</pre></details>
 {task.status==='environment_pending'&&<><Btn disabled={busy} onClick={()=>void inspect()}>Review prepared task requirements</Btn>{review&&<><ul>{review.blockers.map(b=><li key={b}>{b}</li>)}</ul><p>Workflow version {review.currentGraphVersion}. Agent permissions remain unchanged.</p></>}
 {canControl&&<><label><input type="checkbox" checked={confirmed} onChange={e=>setConfirmed(e.target.checked)}/>I reviewed this task environment and authorize the selected build or start action.</label>{!environment.upToDate&&<Btn disabled={busy||!confirmed||environment.state==='building'} onClick={()=>void act(true)}>Retry environment build</Btn>}<Btn disabled={busy||!confirmed||!review?.canStart||!environment.upToDate} onClick={()=>void act(false)}>Start prepared task</Btn></>}
 </>}
 </section>;
}
