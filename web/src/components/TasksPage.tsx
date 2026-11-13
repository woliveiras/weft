import {TaskJourney} from "./TaskJourney";
import {TaskAuthorization} from "./TaskAuthorization";
import {RepositoryImport} from './RepositoryImport';
import {TaskIssueSource} from "./TaskIssueSource";
import {TaskEnvironment} from './TaskEnvironment';
import {TaskRequirementRepairs} from './TaskRequirementRepairs';
import {TaskDecisions} from './TaskDecisions';
import {TaskActivity} from './TaskActivity';
import {useEffect, useState} from 'react';
import {api, errorMessage} from '../api/client';
import type {GraphSummary, Info} from '../api/types';
import type {TaskDraft, TaskInput, TaskReview, TaskProgress} from '../api/tasks';
import {useAsync,useThrottled} from '../hooks/useData';
import {useOptionalEventStream} from '../hooks/useEventStream';
import {Btn, ErrorBox} from './common';
import {TaskCleanup} from './TaskCleanup';
import {WorkspaceHandoff} from './WorkspaceHandoff';
import {SyntheticTaskExample} from './SyntheticTaskExample';
import {TaskEvidence} from './TaskEvidence';
import {TaskPlan} from './TaskPlan';
import {TaskCheckpoints} from './TaskCheckpoints';
import {workflowTaskDefaults,updateTaskInput,outcomeRequest,promptTitle} from './taskDefaults';
import {BudgetBar} from './CostsSection';
import {budgetLabel,money} from './cost';

export const taskOutcomes = [
  {id:'understand',label:'Understand a repository',description:'Explain architecture, dependencies and areas that need attention.'},
  {id:'implement',label:'Implement a change',description:'Prepare code changes and test evidence for your review.'},
  {id:'investigate',label:'Investigate an issue',description:'Examine the supplied problem and report findings and possible fixes.'},
  {id:'review',label:'Review changes',description:'Inspect the supplied changes and explain risks and recommendations.'},
  {id:'freeform',label:'Something else',description:'Describe the work and the result you expect.'},
];
const empty: TaskInput = {title:'',request:'',outcome:'freeform',graphId:'',graphVersion:0,nodeId:''};

export function TasksPage({id,create,info,graphs}: {id?:string;create?:boolean;info?:Info;graphs:GraphSummary[]}) {
  const [after,setAfter] = useState<string>();
  const list = useAsync(()=>api.tasks(after),[after]);
  const detail = useAsync(()=>id?api.task(id):Promise.resolve(undefined),[id]);
  const providers = useAsync(api.providers,[]);
  // Only a loaded list with no ready provider blocks; a failed load does not.
  const noModel = !!providers.data && !providers.data.some(p=>p.status==='ready');
  if (id && detail.loading) return <p className="page">Loading task…</p>;
  if (id && detail.error) return <section className="page"><a href="#/tasks">Back to tasks</a><ErrorBox error={detail.error}/><Btn onClick={()=>void detail.reload()}>Retry</Btn></section>;
  return <section className="page installation-page" aria-label="Tasks">
    <header className="page-head"><div><h1>{create?'New task':id?'Task':'Tasks'}</h1><p className="muted">Prepare a request and review the workflow before execution.</p></div>
      {!create&&!id&&info?.identity?.role!=='viewer'&&info?.identity?.role&&<a className="btn primary" href="#/tasks/new">New task</a>}
    </header>
    {noModel&&!id&&!create&&<NoModelProvider admin={info?.identity?.role==='admin'}/>}
    {id||create ? <TaskEditor key={id??'new'} draft={detail.data} noModel={noModel} admin={info?.identity?.role==='admin'} graphs={graphs.filter(g=>!g.graph.metadata.id.startsWith('task-run-'))} nativeProfile={info?.nativeProfile===true} canStart={info?.identity?.role==='admin'} canEdit={info?.identity?.role==='admin'||info?.identity?.role==='operator'}/> : <>
      {info?.identity?.role==='admin'&&<SyntheticTaskExample/>}
      <ErrorBox error={list.error}/>
      <Btn onClick={()=>void list.reload()} disabled={list.loading}>Refresh tasks</Btn>
      {!list.loading&&!list.error&&list.data?.items.length===0&&<p>No task drafts yet. Create a request using one of your existing workflows.</p>}
      <ul className="task-list">{list.data?.items.map(task=><li key={task.id}><a href={`#/tasks/${encodeURIComponent(task.id)}`}>{task.title}</a><p>{task.resolution&&task.resolution.state!=='resolved'?task.resolution.message:(task.observedState??task.status)}{task.graphId&&<> · Workflow {task.graphId} · Version {task.graphVersion}</>}</p></li>)}</ul>
      {after&&<Btn onClick={()=>setAfter(undefined)}>First page</Btn>}
      {list.data?.next&&<Btn onClick={()=>setAfter(list.data?.next)}>Next page</Btn>}
    </>}
  </section>;
}
/** Workflows whose agents call a model (everything except echo agents). */
export function needsModel(graph?:GraphSummary['graph']):boolean {
  return !!graph?.spec.nodes.some(n=>(n.runtime?.kind??'native')!=='echo');
}

function NoModelProvider({admin}:{admin:boolean}) {
  return <section className="integration-note tone-note" role="note" aria-label="No model provider">
    <h2>No model provider is ready</h2>
    <p>Workflows with model agents cannot run yet; echo-only workflows still can. {admin
      ? <>Enable an inference plugin (for example Ollama) and add a provider under <a href="#/models">Models</a>, then come back to create a task.</>
      : 'Ask an administrator to enable an inference plugin and add a model provider.'}</p>
  </section>;
}
/** Optional source control hand-off: the result agent pushes and opens a pull request through this connection. */
function SCMHandOffField({value,onChange}:{value:string;onChange:(v:string)=>void}) {
  const list=useAsync(()=>api.scmConnections().catch(()=>[]),[]);
  const options=list.data??[];
  if(options.length===0&&!value)return null;
  return <label>Source control hand-off<select value={value} onChange={e=>onChange(e.target.value)}>
    <option value="">None — review and push manually</option>
    {options.map(c=><option key={c.name} value={c.name}>{c.name} ({c.repository})</option>)}
  </select><span className="muted small">After the workflow's human gate, the result agent commits on a task branch, pushes it and opens a pull request{options.find(c=>c.name===value)?.requirePullRequestApproval===false?'':' once a person approves it'}.</span></label>;
}

/** What the SCM hand-off did: pushed branch, pull request, failures. */
function TaskDeliveryView({delivery}:{delivery:import('../api/tasks').TaskDelivery}) {
  return <section aria-label="Source control delivery">
    <h3>Source control</h3>
    <p>{delivery.branch?<>Pushed <code>{delivery.branch}</code> to {delivery.repository} ({delivery.connection}).</>:`Nothing pushed to ${delivery.connection} yet.`}</p>
    {delivery.pullRequestUrl&&<p>Pull request <a href={delivery.pullRequestUrl} target="_blank" rel="noreferrer">#{delivery.pullRequestNumber}</a> is open.</p>}
    {(delivery.failures??[]).length>0&&<ul role="alert" aria-label="Delivery failures">{delivery.failures!.map((f,i)=><li key={i}>{f}</li>)}</ul>}
  </section>;
}

/** Budgets a new run of the selected workflow would be held to, for the caller. */
function TaskBudgetPreview({workflow,project}:{workflow:string;project?:string}) {
  const preview=useAsync(()=>workflow?api.budgetPreview(workflow,project):Promise.resolve(undefined),[workflow,project]);
  const data=preview.data;
  if(!data||(data.taskRunBudgetMicros===0&&data.budgets.filter(b=>b.scope!=='run').length===0))return null;
  return <section className="integration-note" aria-label="Budget">
    <h3>Budget</h3>
    {data.taskRunBudgetMicros>0&&<p>This run may spend up to {money(data.taskRunBudgetMicros,data.currency)} before it pauses for a raise.</p>}
    <ul>{data.budgets.filter(b=>b.scope!=='run').map(b=><li key={b.scope+b.id}>{budgetLabel(b)}: {money(b.remainingMicros,b.currency||data.currency)} left of {money(b.limitMicros,b.currency||data.currency)}{b.reached&&' — reached, new tasks are refused'}</li>)}</ul>
  </section>;
}

/** The run's own budget, with a raise when it paused the run. */
function TaskRunBudget({graph,canRaise,active}:{graph:string;canRaise:boolean;active:boolean}) {
  const budget=useAsync(()=>api.budget('run',graph).catch(()=>undefined),[graph]);
  const reload=useThrottled(()=>void budget.reload(),300);
  useOptionalEventStream({graph,types:['budget.raised','budget.graph_transition','budget.ledger_invalid']},reload);
  if(!budget.data)return null;
  return <section aria-label="Task run budget">
    {active&&budget.data.paused&&!budget.data.notResumed?.length&&<p role="alert">This run is paused because it reached its budget. {canRaise?'Raise the budget to continue, or stop the task.':'Ask an administrator to raise the budget, or stop the task.'}</p>}
    <ul className="integration-grid"><BudgetBar budget={budget.data} currency={budget.data.currency||'USD'} canRaise={canRaise} showPause={active} onRaised={()=>budget.reload()}/></ul>
  </section>;
}

function TaskEditor({draft,graphs,canEdit,canStart,nativeProfile,noModel,admin}:{draft?:TaskDraft;graphs:GraphSummary[];canEdit:boolean;canStart:boolean;nativeProfile:boolean;noModel:boolean;admin:boolean}) {
  const [input,setInput] = useState<TaskInput>(()=>draft?{source:draft.source,executionMode:draft.executionMode,title:draft.title,request:draft.request,outcome:draft.outcome,graphId:draft.graphId,graphVersion:draft.graphVersion,nodeId:draft.nodeId,resultNodeId:draft.resultNodeId,scmConnection:draft.scmConnection,projectId:draft.projectId,baseCommit:draft.baseCommit,channelId:draft.channelId,messageType:draft.messageType,payloadJson:draft.payloadJson}:empty);
  const [submissionKey,setSubmissionKey]=useState(()=>crypto.randomUUID());
 const [importOpen,setImportOpen]=useState(false);
  const [settingsOpen,setSettingsOpen]=useState(false);
  const [customTitle,setCustomTitle]=useState(!!draft);
  const projects=useAsync(()=>api.projects(),[]);
  const [saved,setSaved] = useState(draft);
  const [review,setReview] = useState<TaskReview>();
  const [error,setError] = useState<string>();
  const [busy,setBusy] = useState(false);
  const [confirmed,setConfirmed] = useState(false);
  const [dirty,setDirty] = useState(false);
  const selected=graphs.find(g=>g.graph.metadata.id===input.graphId)?.graph;
  // Drafts can still be saved; running needs a model when the workflow does.
  const modelBlocked=noModel&&needsModel(selected);
  useEffect(()=>{
    if(!dirty)return;
    const warn=(e:BeforeUnloadEvent)=>{e.preventDefault();e.returnValue='';};
    window.addEventListener('beforeunload',warn);return()=>window.removeEventListener('beforeunload',warn);
  },[dirty]);
  const change=(next:Partial<TaskInput>)=>{setSubmissionKey(crypto.randomUUID());setInput(old=>updateTaskInput(old,next));setDirty(true);setReview(undefined);setConfirmed(false);};
  const editRequest=(request:string)=>change({request,...(!customTitle?{title:promptTitle(request)}:{})});
  const save=async()=>{
    setBusy(true);setError(undefined);
    try {const result=await api.saveTask(input,saved?.id,saved?.revision);setSaved(result);setDirty(false);setReview(undefined);if(!saved)location.hash=`#/tasks/${encodeURIComponent(result.id)}`;}
    catch(e){setError(errorMessage(e));}finally{setBusy(false);}
  };
  const resolve=async()=>{
    setBusy(true);setError(undefined);
    try {
      const result=await api.resolveTaskPrompt(input,saved?.id,saved?.revision);
      setSaved(result);setInput({source:result.source,title:result.title,request:result.request,outcome:result.outcome,graphId:result.graphId,graphVersion:result.graphVersion,nodeId:result.nodeId,resultNodeId:result.resultNodeId,channelId:result.channelId,messageType:result.messageType,payloadJson:result.payloadJson,projectId:result.projectId,baseCommit:result.baseCommit,executionMode:result.executionMode});setDirty(false);setReview(undefined);setConfirmed(false);
      if(!saved)location.hash=`#/tasks/${encodeURIComponent(result.id)}`;
    }catch(e){setError(errorMessage(e));}finally{setBusy(false);}
  };
  useEffect(()=>{if(!draft?.submitted||draft.status!=='draft')return;let disposed=false;void api.reviewTask(draft.id).then(value=>{if(!disposed)setReview(value);}).catch(e=>{if(!disposed)setError(errorMessage(e));});return()=>{disposed=true;};},[draft?.id,draft?.submitted]);
  const submit=async()=>{
    setBusy(true);setError(undefined);
    try{const result=await api.submitTaskPrompt(input,submissionKey,saved?.id,saved?.revision);const task=result.task;
      setSaved(task);setInput(task);setDirty(false);setReview(result.review);setConfirmed(false);setSubmissionKey(crypto.randomUUID());
      if(!saved)location.hash=`#/tasks/${encodeURIComponent(task.id)}`;
    }catch(e){setError(errorMessage(e));}finally{setBusy(false);}
  };
  const inspect=async()=>{
    if(!saved)return;setBusy(true);setError(undefined);setConfirmed(false);
    try{setReview(await api.reviewTask(saved.id));}catch(e){setError(errorMessage(e));}finally{setBusy(false);}
  };
  const prepareEnvironment=async()=>{
    if(!saved||!review||!confirmed)return;
    setBusy(true);setError(undefined);
    try{setSaved(await api.prepareTaskEnvironment(saved.id,review.task.revision));setReview(undefined);}
    catch(e){setError(errorMessage(e));try{setSaved(await api.task(saved.id));}catch(readError){setError(`${errorMessage(e)}; ${errorMessage(readError)}`);}}finally{setBusy(false);}
  };
  const start=async()=>{
    if(!saved||!review||!confirmed)return;
    setBusy(true);setError(undefined);
    try{setSaved(await api.startTask(saved.id,review.task.revision));setReview(undefined);}
    catch(e){setError(errorMessage(e));}finally{setBusy(false);}
  };
  if(saved&&saved.status!=='draft')return <TaskTracking task={saved} canControl={canStart} canDecide={canEdit} nativeProfile={nativeProfile}/>;
  return <>
    <a href="#/tasks">Back to tasks</a>
    {modelBlocked&&<NoModelProvider admin={admin}/>}
    {input.graphId&&<TaskBudgetPreview workflow={input.graphId} project={input.projectId}/>}
    <form className="card form" aria-label="Task request" onInvalidCapture={e=>{
      const details=(e.target as HTMLElement).closest('details');
      if(details){details.open=true;setSettingsOpen(true);}
    }} onSubmit={e=>{e.preventDefault();void save();}}>
      <fieldset disabled={!canEdit||busy}>
        <label>Request<textarea required rows={8} placeholder="Describe the work you want the agents to do and the result you expect." maxLength={16384} value={input.request} onChange={e=>editRequest(e.target.value)}/></label>
        <p>Run task authorizes a separate execution of the selected workflow with its existing permissions. Weft checks requirements first; missing configuration or required approvals keep the task waiting. Saving alone keeps a draft.</p>
        <div className="row"><Btn tone="primary" disabled={!input.request.trim()||busy||!canStart||modelBlocked} onClick={()=>void submit()}>{saved?.submitted?'Continue task':'Run task'}</Btn>
        <Btn disabled={!input.request.trim()||busy||!canEdit} onClick={()=>void resolve()}>Resolve request</Btn></div>
        <p>Uses the configured default model to interpret and save your request. Your workflow selection takes priority, then a named workflow, then Software Factory. Title, project and environment choices are retained; input bindings are rebuilt. No agents start.</p>
        {saved?.resolution&&<aside aria-label="Request interpretation"><p>{saved.resolution.message}</p>{saved.resolution.repository&&<p>Repository: {saved.resolution.repository}</p>}{['needs_model','model_error'].includes(saved.resolution.state)&&<a href="#/models">Configure models</a>}<p>Your original request is preserved. Resolving never grants tool access.</p></aside>}
        <label>Workflow<select required value={input.graphId} onChange={e=>{const g=graphs.find(g=>g.graph.metadata.id===e.target.value)?.graph;change({graphId:e.target.value,graphVersion:g?.metadata.version??0,...workflowTaskDefaults(g,input.title,input.request)});}}>
          <option value="">Select workflow</option>{!selected&&input.graphId&&<option value={input.graphId}>{input.graphId} (unavailable)</option>}
          {graphs.map(({graph:g})=><option key={g.metadata.id} value={g.metadata.id}>{g.metadata.name||g.metadata.id}</option>)}
        </select></label>
        <label>Execution environment<select value={input.executionMode??'workflow'} onChange={e=>change({executionMode:e.target.value as TaskInput['executionMode']})}>
          <option value="workflow">Use workflow environment</option><option value="local" disabled={!nativeProfile}>Local worktree{!nativeProfile?' (native Weft required)':''}</option><option value="sandbox">Sandbox</option>
        </select></label>
        <p>Environment selection is checked against the workflow; it does not change agent isolation or permissions. Local worktrees use the native Weft host and do not isolate processes or network access. <a href="https://woliveiras.com/posts/why-git-worktrees-matter-coding-agents/" target="_blank" rel="noreferrer">About worktrees</a></p>
        {input.executionMode==='sandbox'&&<p>Sandbox requires all enabled agents to use Docker isolation and the configured Docker/Colima daemon to be available. Weft never falls back to local execution.</p>}
        <p>New tasks ask for tool approval by default. Save or resolve your request to review and confirm Workspace access or Full access. Mandatory workflow approvals and installation restrictions always apply.</p>
        <ErrorBox error={projects.error}/>
        <details open={settingsOpen} onToggle={e=>setSettingsOpen(e.currentTarget.open)}>
          <summary>Review settings</summary>
        <label>Goal<select value={input.outcome} onChange={e=>change({outcome:e.target.value,request:outcomeRequest(input,e.target.value),...(!customTitle?{title:promptTitle(outcomeRequest(input,e.target.value))}:{})})}>{taskOutcomes.map(o=><option key={o.id} value={o.id}>{o.label}</option>)}</select></label>
        <p>{taskOutcomes.find(o=>o.id===input.outcome)?.description}</p>
        <label>Title<input maxLength={200} required value={input.title} onChange={e=>{setCustomTitle(true);change({title:e.target.value});}}/></label>
        <label>Project<select value={input.projectId??''} onChange={e=>{const p=projects.data?.find(p=>p.id===e.target.value);change({projectId:p?.id,baseCommit:p?.head,executionMode:p?.executionMode??'workflow'});}}>
          <option value="">No repository (managed workspace)</option>{projects.data?.map(p=><option key={p.id} value={p.id}>{p.name}</option>)}
        </select></label>
        {input.projectId&&<p>Base commit: <code>{input.baseCommit}</code>. A separate worktree will be created on the Weft server. Uncommitted source files are not copied.</p>}
        <p>Use a registered project or import a public repository below. The source checkout is retained; each task gets a separate worktree at the selected commit.</p>
        <p className="muted">Unambiguous workflow inputs and result agents are suggested below. Review them before saving. Models and permissions are unchanged.</p>
        <label>Input delivery<select value={input.channelId??''} onChange={e=>{const c=selected?.spec.channels?.find(c=>c.id===e.target.value);change({channelId:c?.id,messageType:c?(c.types?.length===1&&!/[?*]/.test(c.types[0])?c.types[0]:''):undefined,payloadJson:c?JSON.stringify({title:input.title,description:input.request},null,2):undefined,...(c?{nodeId:c.to?.[0]??''}:{})});}}>
          <option value="">Direct agent instruction</option>{selected?.spec.channels?.filter(c=>!c.from&&c.to?.length===1&&(!c.type||c.type==='queue')&&!c.when).map(c=><option key={c.id} value={c.id}>{c.id} → {c.to?.[0]}</option>)}
        </select></label>
        {input.channelId&&<><label>Message type<input required maxLength={128} value={input.messageType??''} onChange={e=>change({messageType:e.target.value})}/></label><label>Input payload (JSON)<textarea required rows={6} maxLength={16384} value={input.payloadJson??''} onChange={e=>change({payloadJson:e.target.value})}/></label><p>The selected channel receives this JSON. Request text is retained for context; it is not sent separately. Check the workflow's expected message type and fields.</p></>}
        <label>Entry agent<select required value={input.nodeId} onChange={e=>change({nodeId:e.target.value})}>
          <option value="">Select entry agent</option>{input.nodeId&&!selected?.spec.nodes.some(n=>n.id===input.nodeId)&&<option value={input.nodeId}>{input.nodeId} (unavailable)</option>}
          {selected?.spec.nodes.filter(n=>!n.disabled).map(n=><option key={n.id} value={n.id}>{n.name||n.id}</option>)}
        </select></label>
        <TaskIssueSource source={input.source} grants={selected?.spec.nodes.find(n=>n.id===input.nodeId)?.tools??selected?.spec.defaults?.tools??[]} onChange={source=>change({source})}/>
        <label>Result agent<select value={input.resultNodeId??''} onChange={e=>change({resultNodeId:e.target.value})}>
          <option value="">Select result agent before starting</option>
          {selected?.spec.nodes.filter(n=>!n.disabled).map(n=><option key={n.id} value={n.id}>{n.name||n.id}</option>)}
        </select></label>
        <SCMHandOffField value={input.scmConnection??''} onChange={scmConnection=>change({scmConnection:scmConnection||undefined})}/>
        <p className="muted">Version {input.graphVersion||'not selected'}. Saving a request does not change the workflow or grant tools.</p>
        {selected&&selected.metadata.version!==input.graphVersion&&<Btn onClick={()=>change({graphVersion:selected.metadata.version??0})}>Use current workflow version</Btn>}
        </details>
      </fieldset>
      <details open={importOpen} onToggle={e=>setImportOpen(e.currentTarget.open)}><summary>Import repository from URL</summary>{importOpen&&<RepositoryImport nativeProfile={nativeProfile} canEdit={canStart} canSelect={canEdit} initialURL={saved?.resolution?.repository} onProject={project=>{change({projectId:project.id,baseCommit:project.head});void projects.reload();setSettingsOpen(true);}}/>}</details>
      {saved?.resolution?.state==='needs_repository'&&!importOpen&&<Btn onClick={()=>setImportOpen(true)}>Prepare repository</Btn>}
      <ErrorBox error={error}/>
      <div className="row"><Btn type="submit" tone="primary" disabled={!canEdit||busy}>Save draft</Btn><Btn disabled={!saved||dirty||busy} onClick={()=>void inspect()}>Review requirements</Btn></div>
      <p role="status">{dirty?'Unsaved changes':saved?'Draft saved. No agents have been started.':'Save this request to resume it later.'}</p>
    </form>
    {saved&&<TaskAuthorization key={`${saved.id}:${saved.revision}`} taskId={saved.id} canEdit={canStart} onChanged={()=>{setReview(undefined);setConfirmed(false);}}/>}
    {review&&<article className="card" aria-label="Task requirements"><h2>Requirements</h2>
      <p>Current workflow version: {review.currentGraphVersion} · Requested environment: {review.task.executionMode||'workflow'}</p>
      {review.blockers.map(b=><p key={b}>{b}</p>)}
      {[...(review.validation.errors??[]),...(review.validation.warnings??[])].map((v,i)=><p key={i}>{v.path}: {v.message}</p>)}
      {review.project&&<p>Project: {review.project.name} · Source: {review.project.path} · Base: {input.baseCommit} · Task worktree: {review.workspacePath}. Commands use the workflow's configured isolation and permissions.</p>}
      <p>Execution copy: {review.executionGraphId}. Starting runs the whole workflow with a fresh managed workspace. Input goes through the selected channel or directly to the entry agent; the selected result agent supplies the result.</p>
      <p>Entry agent model: {review.model?.provider||'runtime default'} / {review.model?.name||'runtime default'}. Isolation: {review.isolation||'process'}. Process execution uses the server machine and is not a sandbox.</p>
      <TaskRequirementRepairs agents={review.agents??[]}/>
      {review.environmentDefinition&&<section aria-label="Task environment definition"><h3>Environment to prepare</h3><p>Building may execute setup commands under the installation sandbox policies and limits. Build completion does not start agents.</p><pre>{JSON.stringify(review.environmentDefinition,null,2)}</pre></section>}
      <p>Tool permissions remain unchanged. Every call still passes gateway policies and approvals.</p>
      <details><summary>All workflow agents and permissions</summary>{review.agents?.map(agent=><section key={agent.id}><h3>{agent.id}</h3><p>{agent.model?.provider} / {agent.model?.name} · {agent.isolation}</p><pre>{JSON.stringify(agent.grants??[],null,2)}</pre></section>)}</details>
      <label><input type="checkbox" checked={confirmed} onChange={e=>setConfirmed(e.target.checked)}/>I reviewed the workflow and authorize a separate execution copy.</label>
      {!canStart&&<p>An administrator must start this task, because it creates an execution copy.</p>}
      {review.canPrepareEnvironment&&<Btn tone="primary" disabled={!canStart||!confirmed||busy||modelBlocked} onClick={()=>void prepareEnvironment()}>Prepare task environment</Btn>}
      <Btn tone="primary" disabled={!canStart||!review.canStart||!confirmed||busy||modelBlocked} onClick={()=>void start()}>Start task</Btn>
      <a href={`#/g/${encodeURIComponent(input.graphId)}/agent?agent=${encodeURIComponent(input.nodeId)}&section=general`}>Review agent configuration</a>
    </article>}
  </>;
}

function TaskTracking({task,canControl,canDecide,nativeProfile}:{task:TaskDraft;canControl:boolean;canDecide:boolean;nativeProfile:boolean}) {
  const [progress,setProgress]=useState<TaskProgress>();
  const [showDiagnostics,setShowDiagnostics]=useState(false);
  const [error,setError]=useState<string>();
  const [busy,setBusy]=useState(false);
  const [confirmed,setConfirmed]=useState(false);
  const [refresh,setRefresh]=useState(0);
  useEffect(()=>{
    let disposed=false;let timer:ReturnType<typeof setTimeout>;
    const load=async()=>{
      try{const result=await api.taskProgress(task.id);if(!disposed){setProgress(result);setError(undefined);}}
      catch(e){if(!disposed)setError(errorMessage(e));}
      if(!disposed)timer=setTimeout(()=>void load(),3000);
    };
    void load();return()=>{disposed=true;clearTimeout(timer);};
  },[task.id,refresh]);
  const cancel=async()=>{setBusy(true);try{await api.cancelTask(task.id);setRefresh(x=>x+1);setConfirmed(false);}catch(e){setError(errorMessage(e));}finally{setBusy(false);}};
  const current=progress?.task??task;
  const graph=current.executionGraphId;
  return <>
    <a href="#/tasks">Back to tasks</a>
    <article className="card" aria-label="Task progress">
      <h2>{task.title}</h2><p>{task.request}</p>
      <p role="status">{progress?`Task status: ${progress.state}`:'Loading progress…'}</p>
      <TaskJourney progress={progress} error={error}/>
      <ErrorBox error={error}/><Btn onClick={()=>setRefresh(x=>x+1)}>Refresh task</Btn>
      <TaskAuthorization taskId={task.id} canEdit={canControl&&["running","starting","environment_pending"].includes(current.status)} running={current.status!=="environment_pending"} onChanged={()=>setRefresh(x=>x+1)}/>
      {current.launchError&&<p>{current.launchError}</p>}
      {progress?.blockers.map(b=><p key={b}>{b}</p>)}
      {graph&&<TaskRunBudget graph={graph} canRaise={canControl} active={progress?.state==='running'}/>}
      {progress?.delivery&&<TaskDeliveryView delivery={progress.delivery}/>}
      {graph&&<TaskDecisions graphId={graph} approvals={error?undefined:progress?.pendingApprovals} canDecide={canDecide} canAdmin={canControl} onChanged={()=>setRefresh(x=>x+1)}/>}
      {progress?.workspacePath&&<WorkspaceHandoff path={progress.workspacePath} nativeProfile={nativeProfile}/>}
      {canControl&&progress?.state==='cancelled'&&current.projectId&&<TaskCleanup taskId={task.id} workspaces={progress.workspaces??[]} onChanged={()=>setRefresh(x=>x+1)}/>}
      {progress?.environment&&<TaskEnvironment key={task.id} task={current} environment={progress.environment} canControl={canControl} onChanged={()=>setRefresh(x=>x+1)}/>}
      {graph&&<TaskPlan key={`plan-${graph}`} graphId={graph}/>}
      <h3>Result</h3><p>{progress?.result?.summary||'No result recorded yet.'}</p>
      <p>Agent output is generated content. Inspect tool outcomes and test evidence before accepting changes.</p>
      {graph&&<TaskEvidence key={graph} graphId={graph} workspaces={progress?.workspaces??[]} nativeProfile={nativeProfile} executions={progress?.executions??[]} role={canControl?'admin':canDecide?'operator':'viewer'}/>}
      {graph&&<TaskCheckpoints key={`checkpoints:${graph}`} graphId={graph} canRestore={canControl}/>}
      {graph&&<nav aria-label="Task evidence"><a href={`#/g/${encodeURIComponent(graph)}/runs`}>Execution details</a> · <a href={`#/g/${encodeURIComponent(graph)}/files`}>Workspace files</a> · <a href="#/approvals">Approvals</a></nav>}
      <details><summary>Workflow and executions</summary><p>Source: {task.graphId} · Version {task.graphVersion} · Execution copy: {graph||'not prepared'}</p>
        <ul>{progress?.executions.map(x=><li key={x.id}><a href={`#/g/${encodeURIComponent(x.graphId)}/runs?execution=${encodeURIComponent(x.id)}&section=result`}>{x.nodeId}: {x.status}</a></li>)}</ul>
      </details>
      {graph&&<details onToggle={e=>setShowDiagnostics(e.currentTarget.open)}><summary>Activity and diagnostics</summary>{showDiagnostics&&<TaskActivity graphId={graph} role={canControl?'admin':canDecide?'operator':'viewer'}/>}</details>}
      {canControl&&progress?.state!=='cancelled'&&<><label><input type="checkbox" checked={confirmed} onChange={e=>setConfirmed(e.target.checked)}/>Stop this task's execution copy and retain its data.</label><Btn disabled={busy||!confirmed} onClick={()=>void cancel()}>Stop task</Btn></>}
      <p>Task completion does not itself commit, push or delete the workspace. Agent actions still depend on the workflow grants and approvals.</p>
    </article>
  </>;
}
