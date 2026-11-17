import type {TaskWorkspace} from '../api/tasks';
import {WorkspaceHandoff} from './WorkspaceHandoff';
import {RunEvents} from './RunsView';
import {ExecutionTreeView} from './ExecutionTreeView';
import {useState} from 'react';
import type {Execution} from '../api/types';
import {Btn} from './common';
import {GitView} from './GitView';
import {CheckEvidence} from './CheckEvidence';

export function TaskEvidence({graphId,executions,role,workspaces=[],nativeProfile=false}:{graphId:string;executions:Execution[];role?:string;workspaces?:TaskWorkspace[];nativeProfile?:boolean}) {
  const [workspaceKey,setWorkspaceKey]=useState('');
  const scope=workspaces.find(x=>JSON.stringify([x.nodeId,x.replica])===workspaceKey);
  const [inspect,setInspect]=useState(false);
  const [selected,setSelected]=useState('');
  const [showTree,setShowTree]=useState(false);
  const execution=executions.find(x=>x.id===selected&&x.graphId===graphId);
  return <section aria-label="Review task evidence">
    <h3>Changes and checks</h3>
    <p>Inspect the current working tree, diff and history. A completed agent run is not proof that tests passed or that changes were committed or pushed; only recorded declared checks are.</p>
    <dl aria-label="Delivery verification">
      <dt>Checks</dt><dd><CheckEvidence graphId={graphId} scope={scope?{nodeId:scope.nodeId,replica:scope.replica}:undefined}/></dd>
      <dt>Commits</dt><dd>Not inferred from task completion. Inspect local Git history for the selected workspace.</dd>
      <dt>Push</dt><dd>Remote state not verified. No automatic push is performed on task completion.</dd>
    </dl>
    <label>Workspace to review<select value={workspaceKey} onChange={e=>setWorkspaceKey(e.target.value)}><option value="">Shared task workspace</option>{workspaces.map(x=><option key={JSON.stringify([x.nodeId,x.replica])} value={JSON.stringify([x.nodeId,x.replica])}>{x.nodeId} · replica {x.replica}{x.branch?` · ${x.branch}`:''}</option>)}</select></label>
    {workspaceKey&&!scope&&<p>The selected workspace is absent from this snapshot. Select an available workspace before inspecting.</p>}
    {scope&&<WorkspaceHandoff key={scope.path} path={scope.path} nativeProfile={nativeProfile}/>}
    <Btn disabled={!!workspaceKey&&!scope} onClick={()=>setInspect(value=>!value)}>{inspect?'Hide changes':'Inspect changes'}</Btn>
    {inspect&&(!workspaceKey||scope)&&<GitView key={`${graphId}:${workspaceKey}`} graphId={graphId} node={scope?.inspectionNode??""} replica={scope?.replica??0} path={scope?.subdir??""}/>}
    <h4>Check execution evidence</h4>
    <p>Open recorded tool calls to review commands and their outcomes. No test result is inferred from the agent summary.</p>
    {!!executions.length&&<label>Inspect execution evidence<select value={selected} onChange={e=>{setSelected(e.target.value);setShowTree(false);}}><option value="">Select an execution</option>{executions.filter(x=>x.graphId===graphId).map(x=><option key={x.id} value={x.id}>{x.nodeId} · {x.status} · {x.id}</option>)}</select></label>}
    {selected&&!execution&&<p>The selected execution is no longer in this snapshot. Select another execution to inspect.</p>}
    {execution&&<section aria-label="Selected execution evidence"><p>Execution: {execution.status}. Individual tool outcomes may differ.</p><p>{execution.usage?.inputTokens??0} input / {execution.usage?.outputTokens??0} output tokens recorded for this execution. Child usage is included in its parent; do not add both totals.</p><RunEvents key={execution.id} execution={execution} toolsOnly/><details onToggle={e=>setShowTree(e.currentTarget.open)}><summary>Execution tree and interruption</summary>{showTree&&<ExecutionTreeView key={execution.id} graphId={graphId} executionId={execution.id} role={role}/>}</details></section>}
    {executions.length===0?<p>No execution evidence recorded yet.</p>:<ul>{executions.map(run=><li key={run.id}><a href={`#/g/${encodeURIComponent(graphId)}/runs?execution=${encodeURIComponent(run.id)}&section=tools`}>{run.nodeId} · {run.status} · Tool calls</a></li>)}</ul>}
  </section>;
}
