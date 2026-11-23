import {TaskTracking} from './TaskDetails';
import { HelpField } from './FieldHelp';
import {TaskAuthorization} from "./TaskAuthorization";
import {RepositoryImport} from './RepositoryImport';
import {TaskIssueSource} from "./TaskIssueSource";
import {TaskRequirementRepairs} from './TaskRequirementRepairs';
import {useEffect, useState} from 'react';
import {api, errorMessage} from '../api/client';
import type {GraphSummary, Info} from '../api/types';
import type {TaskDraft, TaskInput, TaskReview} from '../api/tasks';
import {useAsync} from '../hooks/useData';
import {Btn, ErrorBox} from './common';
import {TasksList} from './TasksList';
import {workflowTaskDefaults,updateTaskInput,outcomeRequest,promptTitle} from './taskDefaults';
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
  const detail = useAsync(()=>id?api.task(id):Promise.resolve(undefined),[id]);
  const providers = useAsync(api.providers,[]);
  // Only a loaded list with no ready provider blocks; a failed load does not.
  const noModel = !!providers.data && !providers.data.some(p=>p.status==='ready');
  if (!id && !create) return <TasksList graphs={graphs} role={info?.identity?.role}>
    {noModel&&<NoModelProvider admin={info?.identity?.role==='admin'}/>}
  </TasksList>;
  if (id && detail.loading) return <p className="page">Loading task…</p>;
  if (id && detail.error) return <section className="page"><a href="#/tasks">Back to tasks</a><ErrorBox error={detail.error}/><Btn onClick={()=>void detail.reload()}>Retry</Btn></section>;
  const editing = create || detail.data?.status === 'draft';
  return <section className={`page installation-page${editing ? ' task-request-page' : id ? ' task-detail-page' : ''}`} aria-label="Tasks">
    <header className="page-head"><div><h1>{create?'New task':editing?'Task draft':detail.data?.title || 'Task'}</h1>{editing&&<p className="muted">Prepare a request and review the workflow before execution.</p>}</div></header>
    <TaskEditor key={id??'new'} draft={detail.data} noModel={noModel} admin={info?.identity?.role==='admin'} graphs={graphs.filter(g=>!g.graph.metadata.id.startsWith('task-run-'))} nativeProfile={info?.nativeProfile===true} canStart={info?.identity?.role==='admin'} canEdit={info?.identity?.role==='admin'||info?.identity?.role==='operator'}/>
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
  return <HelpField label={"Source control hand-off"} help={"Choose which source-control connection delivers the completed result. Review its repository, branch prefix and pull request approval requirement."}><select value={value} onChange={e=>onChange(e.target.value)}>
    <option value="">None — review and push manually</option>
    {options.map(c=><option key={c.name} value={c.name}>{c.name} ({c.repository})</option>)}
  </select><span className="muted small">After the workflow's human gate, the result agent commits on a task branch, pushes it and opens a pull request{options.find(c=>c.name===value)?.requirePullRequestApproval===false?'':' once a person approves it'}.</span></HelpField>;
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
    <a className="task-back-link" href="#/tasks">Back to tasks</a>
    {saved?.environmentApprovalId&&<p>Environment preparation requires a decision in <a href="#/approvals">Approvals</a>. The issue remains in progress.</p>}
    {saved?.launchError&&<p role="alert">{saved.launchError}</p>}
    {modelBlocked&&<NoModelProvider admin={admin}/>}
    {input.graphId&&<TaskBudgetPreview workflow={input.graphId} project={input.projectId}/>}
    <form className="card form task-request-form" aria-label="Task request" onInvalidCapture={e=>{
      const details=(e.target as HTMLElement).closest('details');
      if(details){details.open=true;setSettingsOpen(true);}
    }} onSubmit={e=>{e.preventDefault();void save();}}>
      <fieldset disabled={!canEdit||busy}>
        <HelpField label={"Request"} help={"Describe what work should be done and how to judge the result. Include file names or constraints that the agent cannot infer from the repository."}><textarea required rows={8} placeholder="Describe the work you want the agents to do and the result you expect." maxLength={16384} value={input.request} onChange={e=>editRequest(e.target.value)}/></HelpField>
        <p>Run task authorizes a separate execution of the selected workflow with its existing permissions. Weft checks requirements first; missing configuration or required approvals keep the task waiting. Saving alone keeps a draft.</p>
        <div className="row"><Btn tone="primary" disabled={!input.request.trim()||busy||!canStart||modelBlocked} onClick={()=>void submit()}>{saved?.submitted?'Continue task':'Run task'}</Btn>
        <Btn disabled={!input.request.trim()||busy||!canEdit} onClick={()=>void resolve()}>Resolve request</Btn></div>
        <p>Uses the configured default model to interpret and save your request. Your workflow selection takes priority, then a named workflow, then Software Factory. Title, project and environment choices are retained; input bindings are rebuilt. No agents start.</p>
        {saved?.resolution&&<aside aria-label="Request interpretation"><p>{saved.resolution.message}</p>{saved.resolution.repository&&<p>Repository: {saved.resolution.repository}</p>}{['needs_model','model_error'].includes(saved.resolution.state)&&<a href="#/models">Configure models</a>}<p>Your original request is preserved. Resolving never grants tool access.</p></aside>}
        <HelpField label={"Workflow"} help={"Choose the saved workflow to run this task. Review the agents, their versions and the permissions used by the task."}><select required value={input.graphId} onChange={e=>{const g=graphs.find(g=>g.graph.metadata.id===e.target.value)?.graph;change({graphId:e.target.value,graphVersion:g?.metadata.version??0,...workflowTaskDefaults(g,input.title,input.request)});}}>
          <option value="">Select workflow</option>{!selected&&input.graphId&&<option value={input.graphId}>{input.graphId} (unavailable)</option>}
          {graphs.map(({graph:g})=><option key={g.metadata.id} value={g.metadata.id}>{g.metadata.name||g.metadata.id}</option>)}
        </select></HelpField>
        <HelpField label={"Execution environment"} help={"Use the workflow environment or an eligible project preference when available. The choice controls where task commands run."}><select value={input.executionMode??'workflow'} onChange={e=>change({executionMode:e.target.value as TaskInput['executionMode']})}>
          <option value="workflow">Use workflow environment</option><option value="local" disabled={!nativeProfile}>Local worktree{!nativeProfile?' (native Weft required)':''}</option><option value="sandbox">Sandbox</option>
        </select></HelpField>
        <p>Environment selection is checked against the workflow; it does not change agent isolation or permissions. Local worktrees use the native Weft host and do not isolate processes or network access. <a href="https://woliveiras.com/posts/why-git-worktrees-matter-coding-agents/" target="_blank" rel="noreferrer">About worktrees</a></p>
        {input.executionMode==='sandbox'&&<p>Sandbox requires all enabled agents to use Docker isolation and the configured Docker/Colima daemon to be available. Weft never falls back to local execution.</p>}
        <p>New tasks ask for tool approval by default. Save or resolve your request to review and confirm Workspace access or Full access. Mandatory workflow approvals and installation restrictions always apply.</p>
        <ErrorBox error={projects.error}/>
        <details open={settingsOpen} onToggle={e=>setSettingsOpen(e.currentTarget.open)}>
          <summary>Review settings</summary>
          <div className="task-review-fields">
        <HelpField label={"Goal"} help={"Select the expected delivery format. Use it to make clear whether the task should present findings, make changes or hand off a result."}><select value={input.outcome} onChange={e=>change({outcome:e.target.value,request:outcomeRequest(input,e.target.value),...(!customTitle?{title:promptTitle(outcomeRequest(input,e.target.value))}:{})})}>{taskOutcomes.map(o=><option key={o.id} value={o.id}>{o.label}</option>)}</select><small className="muted">{taskOutcomes.find(o=>o.id===input.outcome)?.description}</small></HelpField>
        <label>Title<input maxLength={200} required value={input.title} onChange={e=>{setCustomTitle(true);change({title:e.target.value});}}/></label>
        <HelpField label={"Project"} help={"Links the task to a registered repository and its preferences. Choose the project whose files the task should use."}><select value={input.projectId??''} onChange={e=>{const p=projects.data?.find(p=>p.id===e.target.value);change({projectId:p?.id,baseCommit:p?.head,executionMode:p?.executionMode??'workflow'});}}>
          <option value="">No repository (managed workspace)</option>{projects.data?.map(p=><option key={p.id} value={p.id}>{p.name}</option>)}
        </select>
          {input.projectId&&<small className="muted">Base commit: <code>{input.baseCommit}</code>. A separate worktree will be created on the Weft server. Uncommitted source files are not copied.</small>}
          <small className="muted">Use a registered project or import a public repository below. The source checkout is retained; each task gets a separate worktree at the selected commit.</small>
        </HelpField>
        <p className="muted">Unambiguous workflow inputs and result agents are suggested below. Review them before saving. Models and permissions are unchanged.</p>
        <HelpField label={"Input delivery"} help={"Choose how to send the request to the workflow. A channel delivers a structured message to eligible receivers; direct entry targets the selected agent."}><select value={input.channelId??''} onChange={e=>{const c=selected?.spec.channels?.find(c=>c.id===e.target.value);change({channelId:c?.id,messageType:c?(c.types?.length===1&&!/[?*]/.test(c.types[0])?c.types[0]:''):undefined,payloadJson:c?JSON.stringify({title:input.title,description:input.request},null,2):undefined,...(c?{nodeId:c.to?.[0]??''}:{})});}}>
          <option value="">Direct agent instruction</option>{selected?.spec.channels?.filter(c=>!c.from&&c.to?.length===1&&(!c.type||c.type==='queue')&&!c.when).map(c=><option key={c.id} value={c.id}>{c.id} → {c.to?.[0]}</option>)}
        </select></HelpField>
        {input.channelId&&<><HelpField label={"Message type"} help={"Use the message type expected by the workflow\u2019s channel or routing conditions. For example, issue.opened must match the type consumed by the workflow."}><input required maxLength={128} value={input.messageType??''} onChange={e=>change({messageType:e.target.value})}/></HelpField><HelpField label={"Input payload (JSON)"} help={"Additional structured data delivered with the request. Use valid JSON and fields the receiving workflow understands."}><textarea required rows={6} maxLength={16384} value={input.payloadJson??''} onChange={e=>change({payloadJson:e.target.value})}/></HelpField><p>The selected channel receives this JSON. Request text is retained for context; it is not sent separately. Check the workflow's expected message type and fields.</p></>}
        <HelpField label={"Entry agent"} help={"The workflow node that starts or receives the task. Pick the agent responsible for interpreting the request."}><select required value={input.nodeId} onChange={e=>change({nodeId:e.target.value})}>
          <option value="">Select entry agent</option>{input.nodeId&&!selected?.spec.nodes.some(n=>n.id===input.nodeId)&&<option value={input.nodeId}>{input.nodeId} (unavailable)</option>}
          {selected?.spec.nodes.filter(n=>!n.disabled).map(n=><option key={n.id} value={n.id}>{n.name||n.id}</option>)}
        </select></HelpField>
        <TaskIssueSource source={input.source} grants={selected?.spec.nodes.find(n=>n.id===input.nodeId)?.tools??selected?.spec.defaults?.tools??[]} onChange={source=>change({source})}/>
        <HelpField label={"Result agent"} help={"The workflow node expected to produce the final task result. Use a distinct node when the workflow has a separate finishing stage."}><select value={input.resultNodeId??''} onChange={e=>change({resultNodeId:e.target.value})}>
          <option value="">Same as entry</option>
          {selected?.spec.nodes.filter(n=>!n.disabled).map(n=><option key={n.id} value={n.id}>{n.name||n.id}</option>)}
        </select></HelpField>
        <SCMHandOffField value={input.scmConnection??''} onChange={scmConnection=>change({scmConnection:scmConnection||undefined})}/>
        <p className="muted">Version {input.graphVersion||'not selected'}. Saving a request does not change the workflow or grant tools.</p>
        {selected&&selected.metadata.version!==input.graphVersion&&<Btn onClick={()=>change({graphVersion:selected.metadata.version??0})}>Use current workflow version</Btn>}
          </div>
        </details>
      </fieldset>
      <details open={importOpen} onToggle={e=>setImportOpen(e.currentTarget.open)}><summary>Import repository from URL</summary>{importOpen&&<RepositoryImport nativeProfile={nativeProfile} canEdit={canStart} canSelect={canEdit} initialURL={saved?.resolution?.repository} onProject={project=>{change({projectId:project.id,baseCommit:project.head});void projects.reload();setSettingsOpen(true);}}/>}</details>
      {saved?.resolution?.state==='needs_repository'&&!importOpen&&<Btn onClick={()=>setImportOpen(true)}>Prepare repository</Btn>}
      <ErrorBox error={error}/>
      <div className="row"><Btn type="submit" tone="primary" disabled={!canEdit||busy}>Save draft</Btn><Btn disabled={!saved||dirty||busy} onClick={()=>void inspect()}>Review requirements</Btn></div>
      <p className="task-request-status" role="status">{dirty?'Unsaved changes':saved?'Draft saved. No agents have been started.':''}</p>
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
