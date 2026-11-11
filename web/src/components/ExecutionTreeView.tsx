import {useEffect,useRef,useState} from 'react';
import {api,errorMessage} from '../api/client';
import {activeExecution,treeDepths,type CancelScope} from '../api/executionTree';
import {useAsync} from '../hooks/useData';
import {Badge,Btn,ErrorBox,fmtDateTime} from './common';
import {runLink} from './runPresentation';
export function ExecutionTreeView({graphId,executionId,role}:{graphId:string;executionId:string;role?:string}) {
 const tree=useAsync(()=>api.executionTree(executionId),[executionId]);
 const [review,setReview]=useState<{id:string;scope:CancelScope}>();
 const [busy,setBusy]=useState(false),[error,setError]=useState<string>(),[notice,setNotice]=useState<string>();
 const lock=useRef(false);
 useEffect(()=>{if(tree.loading)return;const timer=setTimeout(()=>void tree.reload(),2000);return()=>clearTimeout(timer);},[tree.loading,tree.reload]);
 let depths:Map<string,number>|undefined,invalid:string|undefined;
 if(tree.data&&!tree.error){try{depths=treeDepths(tree.data,graphId,executionId);}catch(e){invalid=errorMessage(e);}}
 const canControl=(role==='admin'||role==='operator')&&!!depths&&!tree.error;
 const rows=depths?tree.data!.nodes:[];
 const root=rows[0];
 async function confirm(){
  if(!review||!canControl||lock.current)return;
  lock.current=true;setBusy(true);setError(undefined);
  try{const result=await api.cancelExecution(review.id,review.scope);setNotice(result.alreadyTerminal?'The execution has already finished.':'Interruption requested. Waiting for the worker to report its outcome.');setReview(undefined);await tree.reload();}
  catch(e){setError(errorMessage(e));}
  finally{lock.current=false;setBusy(false);}
 }
 return <section className="card" aria-label="Execution tree"><div className="page-head"><h2>Execution tree</h2><Btn disabled={tree.loading||busy} onClick={()=>void tree.reload()}>Refresh tree</Btn></div>
 <p>Parent and delegated executions linked by saved records. Child token usage is included in the parent total; do not add both totals.</p>
 {depths&&tree.data?.budget&&<section aria-label="Tree token budget"><h3>Tree token budget</h3><p>Limit: {tree.data.budget.limit} · Charged: {tree.data.budget.charged} · Reserved: {tree.data.budget.reserved} · Estimated charges: {tree.data.budget.estimated}</p><p>Parent and child calls are counted once. Reservations use an input estimate plus the output allowance. Unknown consumption after dispatch is charged conservatively; this is not a billing guarantee.</p></section>}
 <ErrorBox error={tree.error||invalid}/><ErrorBox error={error}/>{notice&&<p role="status">{notice}</p>}
 {canControl&&root&&<Btn disabled={busy||!!root.cancellation||!rows.some(n=>activeExecution(n.execution))} onClick={()=>setReview({id:root.execution.id,scope:'tree'})}>Stop entire tree</Btn>}
 {review&&canControl&&<section className="card" role="region" aria-label="Confirm interruption"><h3>{review.scope==='tree'?'Stop entire execution tree?':'Stop this child execution?'}</h3><p>Target: {review.id}</p><p>{review.scope==='tree'?'The root execution and its active children will be interrupted.':'This child and its active descendants will be interrupted. Its parent receives an error and may continue.'} Completed effects cannot be undone. Future runs and the graph configuration are unchanged.</p><Btn disabled={busy} onClick={()=>void confirm()}>Confirm interruption</Btn><Btn disabled={busy} onClick={()=>setReview(undefined)}>Keep running</Btn></section>}
 {!!depths&&<ol aria-label="Execution relationships">{rows.map(({execution:x,cancellation})=><li key={x.id} style={{marginInlineStart:`${depths.get(x.id)!*1.5}rem`}}><article className="card"><h3><a href={runLink(graphId,x.id,'tree')}>{x.nodeId} · {x.id}</a></h3><p>{x.parentExecutionId?`Child of ${x.parentExecutionId} · Profile: ${x.delegationTemplate}`:'Root execution'} · <Badge>{x.status}</Badge></p><p>Started {fmtDateTime(x.startedAt)}{x.completedAt?` · Finished ${fmtDateTime(x.completedAt)}`:''} · Duration: {Math.max(0,Math.round(((x.completedAt?Date.parse(x.completedAt):Date.now())-Date.parse(x.startedAt))/1000))}s</p><p>{x.usage?.inputTokens??0} input / {x.usage?.outputTokens??0} output tokens</p><ErrorBox error={x.error}/>{cancellation&&<p>Interruption requested by {cancellation.actor} at {fmtDateTime(cancellation.requestedAt)}{activeExecution(x)?' · Waiting for worker acknowledgement.':''}</p>}{canControl&&x.parentExecutionId&&<Btn disabled={busy||!activeExecution(x)||!!cancellation} onClick={()=>setReview({id:x.id,scope:'child'})}>Stop child</Btn>}</article></li>)}</ol>}
 </section>;
}
