import {useRef,useState} from 'react';
import {api,errorMessage} from '../api/client';
import type {Graph} from '../api/types';
import {Btn,ErrorBox} from './common';

export function syntheticWorkflow(id:string):Graph {
  return {apiVersion:'weft.io/v1',kind:'Graph',metadata:{id,name:'Synthetic task example',description:'Echo-only example. No real model, repository or account access.'},spec:{nodes:[{id:'example',name:'Synthetic example',runtime:{kind:'echo'},loop:{mode:'once'},tools:[]}]}};
}
export function SyntheticTaskExample() {
  const [expanded,setExpanded]=useState(false);
  const [confirmed,setConfirmed]=useState(false);
  const [busy,setBusy]=useState(false);
  const [attempted,setAttempted]=useState(false);
  const [error,setError]=useState<string>();
  const [workflowId,setWorkflowId]=useState<string>();
  const started=useRef(false);
  const prepare=async()=>{
    if(!confirmed||started.current)return;
    started.current=true;setAttempted(true);setBusy(true);
    try{
      if(!globalThis.crypto?.randomUUID)throw new Error('Preparing an example requires HTTPS or a localhost browser connection.');
      const id=`example-${crypto.randomUUID()}`;setWorkflowId(id);
      const saved=await api.createGraph(syntheticWorkflow(id));
      const task=await api.saveTask({title:'Synthetic first task',request:'Echo this sample request to demonstrate task review and results. This is not repository analysis or AI output.',outcome:'freeform',graphId:saved.graph.metadata.id,graphVersion:saved.graph.metadata.version??saved.record.version,nodeId:'example',resultNodeId:'example'});
      location.hash=`#/tasks/${encodeURIComponent(task.id)}`;
    }catch(e){setError(errorMessage(e));}finally{setBusy(false);}
  };
  return <section className="card" aria-label="Synthetic task example">
    <h2>Try the task flow</h2><p>Explore review, execution and results with a synthetic echo example. No model, repository or connected account is used.</p>
    {!expanded?<Btn onClick={()=>setExpanded(true)}>Preview example</Btn>:<>
      <p>This creates a saved example workflow and a task draft. Nothing runs until you review and start the task. The result echoes sample text; it does not analyze code.</p>
      <label><input type="checkbox" checked={confirmed} disabled={attempted} onChange={e=>setConfirmed(e.target.checked)}/>Create the synthetic workflow and task draft.</label>
      <Btn tone="primary" disabled={!confirmed||attempted||busy} onClick={()=>void prepare()}>Prepare example</Btn>
      <ErrorBox error={error}/>
      {error&&workflowId&&<p>Preparation may have saved some data. Check <a href="#/tasks">Tasks</a> and <a href={`#/g/${encodeURIComponent(workflowId??'')}/canvas`}>the example workflow</a> before trying again. No execution was requested.</p>}
    </>}
  </section>;
}
