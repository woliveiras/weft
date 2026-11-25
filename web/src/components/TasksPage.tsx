import {TaskReadiness} from './TaskReadiness';
import {applyTaskEntryContext,taskRepairLink,type TaskEntryContext} from './taskEntryContext';
import {TaskDraftDelete} from './TaskDraftDelete';
import {TaskRetryOrigin} from './TaskRetry';
import {TaskTracking} from './TaskDetails';
import { HelpField } from './FieldHelp';
import {TaskAuthorization} from "./TaskAuthorization";
import {RepositoryImport} from './RepositoryImport';
import {TaskIssueSource} from "./TaskIssueSource";
import {TaskRequirementRepairs} from './TaskRequirementRepairs';
import {useEffect, useRef, useState} from 'react';
import {api, errorMessage} from '../api/client';
import type {GraphSummary, Info} from '../api/types';
import type {TaskDraft, TaskInput, TaskReview} from '../api/tasks';
import {useAsync} from '../hooks/useData';
import {Btn, ErrorBox} from './common';
import {TasksList} from './TasksList';
import {isTaskEnvironmentPreparationNotice,defaultTaskWorkflowVersion,workflowTaskDefaults,updateTaskInput,outcomeRequest,promptTitle,missingTaskConfiguration,projectChoiceLabel,submissionDraftFeedback,taskConfigurationFieldErrors} from './taskDefaults';
import {budgetLabel,money} from './cost';

export const taskOutcomes = [
  {id:'understand',label:'Understand a repository',description:'Explain architecture, dependencies and areas that need attention.'},
  {id:'implement',label:'Implement a change',description:'Prepare code changes and test evidence for your review.'},
  {id:'investigate',label:'Investigate an issue',description:'Examine the supplied problem and report findings and possible fixes.'},
  {id:'review',label:'Review changes',description:'Inspect the supplied changes and explain risks and recommendations.'},
  {id:'freeform',label:'Something else',description:'Describe the work and the result you expect.'},
];
const empty: TaskInput = {title:'',request:'',outcome:'freeform',graphId:'',graphVersion:0,nodeId:''};

function editableInput(draft:TaskDraft):TaskInput {
  return {source:draft.source,executionMode:draft.executionMode,title:draft.title,request:draft.request,outcome:draft.outcome,graphId:draft.graphId,graphVersion:draft.graphVersion,nodeId:draft.nodeId,resultNodeId:draft.resultNodeId,scmConnection:draft.scmConnection,projectId:draft.projectId,baseCommit:draft.baseCommit,channelId:draft.channelId,messageType:draft.messageType,payloadJson:draft.payloadJson};
}

export function TasksPage({id,create,info,graphs,entry,graphsLoading=false,graphsError,onReloadGraphs,reviewOnLoad=false}: {id?:string;create?:boolean;info?:Info;graphs:GraphSummary[];entry?:TaskEntryContext;graphsLoading?:boolean;graphsError?:string;onReloadGraphs?:()=>void;reviewOnLoad?:boolean}) {
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
    <TaskEditor key={id??'new'} draft={detail.data} reviewOnLoad={reviewOnLoad} entry={entry} graphsLoading={graphsLoading} graphsError={graphsError} onReloadGraphs={onReloadGraphs} modelState={providers.loading?'loading':providers.error?'unavailable':noModel?'missing':'configured'} noModel={noModel} admin={info?.identity?.role==='admin'} graphs={graphs.filter(g=>!g.graph.metadata.id.startsWith('task-run-'))} nativeProfile={info?.nativeProfile===true} canStart={info?.identity?.role==='admin'} canEdit={info?.identity?.role==='admin'||info?.identity?.role==='operator'}/>
  </section>;
}
/** Workflows whose agents call a model (everything except echo agents). */
export function needsModel(graph?:GraphSummary['graph']):boolean {
  return !!graph?.spec.nodes.some(n=>(n.runtime?.kind??'native')!=='echo');
}

function NoModelProvider({admin,onRepair,busy}:{admin:boolean;onRepair?:()=>void;busy?:boolean}) {
  return <section className="integration-note tone-note" role="note" aria-label="No model provider">
    <h2>No model provider is ready</h2>
    <p>Workflows with model agents cannot run yet; echo-only workflows still can. {admin
      ? <>Enable an inference plugin (for example Ollama) and add a provider under {onRepair?<Btn disabled={busy} onClick={onRepair}>Configure models and return to task</Btn>:<a href="#/models">Models</a>}, then come back to create a task.</>
      : 'Ask an administrator to enable an inference plugin and add a model provider.'}</p>
  </section>;
}
/** Optional source control hand-off: the result agent pushes and opens a pull request through this connection. */
export function SCMHandOffField({value,onChange}:{value:string;onChange:(v:string)=>void}) {
  const list=useAsync(()=>api.scmConnections(),[]);
  const options=list.data??[];

  return <HelpField label={"Source control hand-off"} help={"Choose which source-control connection delivers the completed result. Review its repository, branch prefix and pull request approval requirement."}><select value={value} onChange={e=>onChange(e.target.value)}>
    <option value="">None — review and push manually</option>
    {value&&!options.some(c=>c.name===value)&&<option value={value}>{value} (saved selection unavailable)</option>}
    {options.map(c=><option key={c.name} value={c.name}>{c.name} ({c.repository})</option>)}
  </select>{list.loading&&<small role="status">Loading source control connections…</small>}{list.error&&<><ErrorBox error={list.error}/><Btn onClick={()=>void list.reload()}>Retry source control connections</Btn></>}{!list.loading&&!list.error&&!options.length&&<small>No source control connections configured. An administrator can add one in Connections.</small>}<span className="muted small">After the workflow's human gate, the result agent commits on a task branch, pushes it and opens a pull request{options.find(c=>c.name===value)?.requirePullRequestApproval===false?'':' once a person approves it'}.</span></HelpField>;
}

/** Budgets a new run of the selected workflow would be held to, for the caller. */
function TaskBudgetPreview({workflow,project}:{workflow:string;project?:string}) {
  const preview=useAsync(()=>workflow?api.budgetPreview(workflow,project):Promise.resolve(undefined),[workflow,project]);
  const data=preview.data;
  if(preview.loading)return <p role="status">Loading applicable budgets…</p>;
  if(preview.error)return <aside aria-label="Budget unavailable"><ErrorBox error={preview.error}/><Btn onClick={()=>void preview.reload()}>Retry budget preview</Btn></aside>;
  if(!data||(data.taskRunBudgetMicros===0&&data.budgets.filter(b=>b.scope!=='run').length===0))return null;
  return <section className="integration-note" aria-label="Budget">
    <h3>Budget</h3>
    {data.taskRunBudgetMicros>0&&<p>This run may spend up to {money(data.taskRunBudgetMicros,data.currency)} before it pauses for a raise.</p>}
    <ul>{data.budgets.filter(b=>b.scope!=='run').map(b=><li key={b.scope+b.id}>{budgetLabel(b)}: {money(b.remainingMicros,b.currency||data.currency)} left of {money(b.limitMicros,b.currency||data.currency)}{b.reached&&' — reached, new tasks are refused'}</li>)}</ul>
  </section>;
}

function TaskEditor({draft,graphs,canEdit,canStart,nativeProfile,noModel,admin,entry,graphsLoading,graphsError,onReloadGraphs,modelState,reviewOnLoad}:{draft?:TaskDraft;graphs:GraphSummary[];canEdit:boolean;canStart:boolean;nativeProfile:boolean;noModel:boolean;admin:boolean;entry?:TaskEntryContext;graphsLoading:boolean;graphsError?:string;onReloadGraphs?:()=>void;modelState:"loading"|"unavailable"|"missing"|"configured";reviewOnLoad:boolean}) {
  const [input,setInput] = useState<TaskInput>(()=>draft?editableInput(draft):empty);
  const contextApplied=useRef(!!draft);
  const contextChoices=useRef(new Set<keyof TaskInput>());
 const [importOpen,setImportOpen]=useState(false);
  const settingsRef=useRef<HTMLElement>(null);
  const environmentRef=useRef<HTMLElement>(null);
  const openEnvironmentReview=()=>{environmentRef.current?.scrollIntoView?.({block:'start',behavior:'smooth'});environmentRef.current?.focus();};
  const openSettings=()=>{settingsRef.current?.scrollIntoView?.({block:'start',behavior:'smooth'});settingsRef.current?.focus();};
  const [customTitle,setCustomTitle]=useState(!!draft);
  const projects=useAsync(()=>api.projects(),[]);
  const [saved,setSaved] = useState(draft);
  const [reviewResult,setReview] = useState<TaskReview>();
  const [error,setError] = useState<string>();
  const [busy,setBusy] = useState(false);
  const [submitting,setSubmitting] = useState(false);
  const [preparingEnvironment,setPreparingEnvironment] = useState(false);
  const [submissionFeedback,setSubmissionFeedback] = useState<ReturnType<typeof submissionDraftFeedback>>(()=>draft?.submitted?submissionDraftFeedback(draft):undefined);
  const [confirmed,setConfirmed] = useState(false);
  const [dirty,setDirty] = useState(false);
  const review=!dirty&&reviewResult?.task.revision===saved?.revision?reviewResult:undefined;
  const selected=graphs.find(g=>g.graph.metadata.id===input.graphId)?.graph;
  useEffect(()=>{
    if(!canEdit)return;
    const next=defaultTaskWorkflowVersion(input,selected);
    if(next===input)return;
    setInput(next);setDirty(true);setReview(undefined);setConfirmed(false);
    if(saved)setSubmissionFeedback(previous=>previous?submissionDraftFeedback({...saved,...next}):undefined);
  },[input,selected,canEdit,saved]);
  // Drafts can still be saved; running needs a model when the workflow does.
  const modelBlocked=noModel&&needsModel(selected);
  const missing=missingTaskConfiguration(input);
  const savedMissing=saved?missingTaskConfiguration(saved):[];
  const fieldErrors=saved?.submitted||saved?.resolution||reviewResult||submissionFeedback?taskConfigurationFieldErrors(input):{};

  useEffect(()=>{
    if(!dirty)return;
    const warn=(e:BeforeUnloadEvent)=>{e.preventDefault();e.returnValue='';};
    window.addEventListener('beforeunload',warn);return()=>window.removeEventListener('beforeunload',warn);
  },[dirty]);
  const change=(next:Partial<TaskInput>,explicit=true)=>{if(explicit)for(const key of Object.keys(next) as (keyof TaskInput)[])contextChoices.current.add(key);setSubmissionFeedback(undefined);setInput(old=>updateTaskInput(old,next));setDirty(true);setReview(undefined);setConfirmed(false);};
  useEffect(()=>{
    if(contextApplied.current||!entry||projects.loading||projects.error||graphsLoading||graphsError)return;
    contextApplied.current=true;
    const contextual=applyTaskEntryContext(input,{workflow:contextChoices.current.has('graphId')?undefined:entry.workflow,project:contextChoices.current.has('projectId')?undefined:entry.project},graphs.map(g=>g.graph),projects.data??[]);
    const next=contextual===input?input:{...contextual,...Object.fromEntries([...contextChoices.current].map(key=>[key,input[key]]))};
    if(next!==input){setInput(next);setDirty(true);}
  },[entry,projects.loading,projects.error,projects.data,graphsLoading,graphsError,graphs,input]);
  useEffect(()=>{
    if(draft||!input.projectId||input.baseCommit||!projects.data)return;
    const project=projects.data.find(p=>p.id===input.projectId);
    if(project)setInput(old=>({...old,baseCommit:project.head,executionMode:old.executionMode??project.executionMode}));
  },[draft,input.projectId,input.baseCommit,projects.data]);
  const repair=async(destination:string)=>{
    setBusy(true);setError(undefined);
    try {
      const task=dirty||!saved?await api.saveTask(input,saved?.id,saved?.revision):saved;
      setSaved(task);setDirty(false);setReview(undefined);setConfirmed(false);
      location.hash=taskRepairLink(destination,task.id);
    }catch(e){setError(`Could not save the task before opening configuration: ${errorMessage(e)}`);}
    finally{setBusy(false);}
  };
  const editRequest=(request:string)=>change({request,...(!customTitle?{title:promptTitle(request)}:{})});
  const save=async()=>{
    setBusy(true);setSubmissionFeedback(undefined);setError(undefined);
    try {const result=await api.saveTask(input,saved?.id,saved?.revision);setSaved(result);setDirty(false);setReview(undefined);if(!saved)location.hash=`#/tasks/${encodeURIComponent(result.id)}`;}
    catch(e){setError(errorMessage(e));}finally{setBusy(false);}
  };
  const resolve=async()=>{
    setBusy(true);setSubmissionFeedback(undefined);setError(undefined);
    try {
      const result=await api.resolveTaskPrompt(input,saved?.id,saved?.revision);
      setSaved(result);setInput({source:result.source,title:result.title,request:result.request,outcome:result.outcome,graphId:result.graphId,graphVersion:result.graphVersion,nodeId:result.nodeId,resultNodeId:result.resultNodeId,scmConnection:result.scmConnection,channelId:result.channelId,messageType:result.messageType,payloadJson:result.payloadJson,projectId:result.projectId,baseCommit:result.baseCommit,executionMode:result.executionMode});setDirty(false);setReview(undefined);setConfirmed(false);
      if(!saved)location.hash=`#/tasks/${encodeURIComponent(result.id)}`;
    }catch(e){setError(errorMessage(e));}finally{setBusy(false);}
  };
  useEffect(()=>{if(!draft||(!draft.submitted&&!reviewOnLoad)||draft.status!=='draft')return;let disposed=false;void api.reviewTask(draft.id).then(value=>{if(!disposed)setReview(value);}).catch(e=>{if(!disposed)setError(errorMessage(e));});return()=>{disposed=true;};},[draft?.id,draft?.submitted,reviewOnLoad]);
  const submit=async()=>{
    setBusy(true);setSubmitting(true);setSubmissionFeedback(undefined);setError(undefined);
    try{
      // Review is the same explicit, non-executing operation for new and saved tasks.
      setReview(undefined);setConfirmed(false);
      if(missing.length>0){
        setSubmissionFeedback(submissionDraftFeedback({...saved,...input,status:'draft'} as TaskDraft));
        return;
      }
      const task=dirty||!saved?await api.saveTask(input,saved?.id,saved?.revision):saved;
      setSaved(task);setDirty(false);
      const checked=await api.reviewTask(task.id);
      setReview(checked);setSubmissionFeedback(submissionDraftFeedback(task,checked));
      if(!saved)location.hash=`#/tasks/${encodeURIComponent(task.id)}?review=1`;
    }catch(e){setError(errorMessage(e));}finally{setBusy(false);setSubmitting(false);}
  };
  const inspect=async()=>{
    if(!saved)return;setBusy(true);setSubmissionFeedback(undefined);setError(undefined);setConfirmed(false);
    try{setReview(await api.reviewTask(saved.id));}catch(e){setError(errorMessage(e));}finally{setBusy(false);}
  };
  const prepareEnvironment=async()=>{
    if(!saved||!review||!confirmed||!review.canPrepareEnvironment)return;
    setBusy(true);setPreparingEnvironment(true);setSubmissionFeedback(undefined);setError(undefined);
    try{setSaved(await api.prepareTaskEnvironment(saved.id,review.task.revision));setReview(undefined);}
    catch(e){setError(errorMessage(e));try{setSaved(await api.task(saved.id));}catch(readError){setError(`${errorMessage(e)}; ${errorMessage(readError)}`);}}finally{setBusy(false);setPreparingEnvironment(false);}
  };
  const start=async()=>{
    if(!saved||!review||!confirmed)return;
    setBusy(true);setSubmissionFeedback(undefined);setError(undefined);
    try{setSaved(await api.startTask(saved.id,review.task.revision));setReview(undefined);}
    catch(e){setError(errorMessage(e));}finally{setBusy(false);}
  };
  if(saved&&saved.status!=='draft')return <TaskTracking task={saved} canControl={canStart} canDecide={canEdit} canRetry={canEdit} nativeProfile={nativeProfile}/>;
  return <>
    <a className="task-back-link" href="#/tasks">Back to tasks</a>
    {saved && <TaskRetryOrigin task={saved}/>}
    {saved?.environmentApprovalId&&<p>Environment preparation requires a decision in <a href="#/approvals">Approvals</a>. The issue remains in progress.</p>}
    {saved?.launchError&&<p role="alert">{saved.launchError}</p>}
    {modelBlocked&&<NoModelProvider admin={admin} busy={busy} onRepair={()=>void repair('#/models')}/>}
    <form className="task-request-form" aria-label="Task request" onSubmit={e=>{e.preventDefault();void save();}}>
      <fieldset disabled={!canEdit||busy}>
        <section className="card form task-editor-section" aria-labelledby="task-request-heading">
          <header><h2 id="task-request-heading">1. Describe the work</h2><p className="muted">Give the agents a clear request and the result you expect.</p></header>
        <HelpField label={"Request"} help={"Describe what work should be done and how to judge the result. Include file names or constraints that the agent cannot infer from the repository."}><textarea required rows={8} placeholder="Describe the work you want the agents to do and the result you expect." maxLength={16384} value={input.request} onChange={e=>editRequest(e.target.value)}/></HelpField>
          <div className="task-field-grid">
        <HelpField label={"Goal"} help={"Select the expected delivery format. Use it to make clear whether the task should present findings, make changes or hand off a result."}><select value={input.outcome} onChange={e=>change({outcome:e.target.value,request:outcomeRequest(input,e.target.value),...(!customTitle?{title:promptTitle(outcomeRequest(input,e.target.value))}:{})})}>{taskOutcomes.map(o=><option key={o.id} value={o.id}>{o.label}</option>)}</select><small className="muted">{taskOutcomes.find(o=>o.id===input.outcome)?.description}</small></HelpField>
        <HelpField label="Title" help="A short name used to identify this task in lists and its history. It starts from the first line of your request; editing it keeps your chosen title."><input maxLength={200} required value={input.title} onChange={e=>{setCustomTitle(true);change({title:e.target.value});}}/></HelpField>
          </div>
          <div className="task-interpretation">
            <Btn disabled={!input.request.trim()||busy||!canEdit} onClick={()=>void resolve()}>Resolve request</Btn>
            <p className="muted small">Optional: ask the default model to suggest task settings and save the request. Your title, project and environment choices are retained; input bindings are rebuilt. No agents start.</p>
          </div>
        {saved?.resolution&&<aside aria-label="Request interpretation"><p>{saved.resolution.message}</p>{saved.resolution.repository&&<p>Repository: {saved.resolution.repository}</p>}{['needs_model','model_error'].includes(saved.resolution.state)&&<Btn disabled={busy||!admin} onClick={()=>void repair('#/models')}>Configure models</Btn>}<p>Your original request is preserved. Resolving never grants tool access.</p>{savedMissing.length>0&&<><p>Task setup is incomplete: {savedMissing.join(', ')}. Retry Resolve request, or configure it manually without using the model.</p><Btn onClick={openSettings}>Configure task manually</Btn></>}</aside>}
        </section>
        <section ref={settingsRef} tabIndex={-1} className="card form task-editor-section" aria-labelledby="task-settings-heading">
          <header><h2 id="task-settings-heading">2. Workflow &amp; repository</h2><p className="muted">Review where the work will run and which repository it will use.</p></header>
          <div className="task-field-grid">
        <HelpField label={"Workflow"} error={fieldErrors.graphId} help={"Choose the saved workflow to run this task. Review the agents, their versions and the permissions used by the task."}><select required value={input.graphId} onChange={e=>{
          const g=graphs.find(g=>g.graph.metadata.id===e.target.value)?.graph;
          const defaults=workflowTaskDefaults(g,input.title,input.request);
          // Selecting a workflow accepts its routing suggestions, not its repository over an explicit entry choice.
          const projectId=contextChoices.current.has('projectId')?input.projectId:entry?.project||input.projectId||defaults.projectId;
          const project=projects.data?.find(p=>p.id===projectId);
          contextChoices.current.add('graphId');
          change({...defaults,graphId:e.target.value,graphVersion:g?.metadata.version??0,projectId,
            baseCommit:projectId===input.projectId?input.baseCommit:project?.head,
            executionMode:input.executionMode??project?.executionMode,
            ...(contextChoices.current.has('scmConnection')?{scmConnection:input.scmConnection}:{})},false);
        }}>
          <option value="">Select workflow</option>{!selected&&input.graphId&&<option value={input.graphId}>{input.graphId} (unavailable)</option>}
          {graphs.map(({graph:g})=><option key={g.metadata.id} value={g.metadata.id}>{g.metadata.name||g.metadata.id}</option>)}
        </select></HelpField>
            <HelpField label="Workflow version" error={fieldErrors.graphVersion} help="The current saved workflow version is selected by default. Saving pins that version to this task. Existing pins do not change when a workflow is published; select the current version and review again to update a draft.">
              <select required disabled={!selected?.metadata.version} value={input.graphVersion||''} onChange={e=>change({graphVersion:Number(e.target.value)})}>
                {!input.graphVersion&&!selected?.metadata.version&&<option value="">Select a workflow</option>}
                {selected?.metadata.version&&<option value={selected.metadata.version}>Current workflow version (Version {selected.metadata.version})</option>}
                {!!input.graphVersion&&input.graphVersion!==selected?.metadata.version&&<option value={input.graphVersion}>Version {input.graphVersion} (saved)</option>}
              </select>
            </HelpField>
        <HelpField label={"Project"} help={"Links the task to a registered repository and its preferences. Choose the project whose files the task should use."}><select value={input.projectId??''} onChange={e=>{const p=projects.data?.find(p=>p.id===e.target.value);change({projectId:p?.id,baseCommit:p?.head,executionMode:p?.executionMode??'workflow'});}}>
          <option value="">Use workflow repository or empty managed workspace</option>{input.projectId&&!projects.data?.some(p=>p.id===input.projectId)&&<option value={input.projectId}>{input.projectId} (saved selection)</option>}{projects.data?.map(p=><option key={p.id} value={p.id}>{projectChoiceLabel(p)}</option>)}
        </select>
          {input.projectId&&<small className="muted">Base commit: <code>{input.baseCommit}</code>. A separate worktree will be created on the Weft server. Uncommitted source files are not copied.</small>}
          <small className="muted">Choose the repository whose files this task should use, or import one below. Without a project, a configured workflow repository is used; otherwise the workspace starts empty.</small>
        </HelpField>
        <HelpField label={"Execution environment"} help={"Use the workflow environment or an eligible project preference when available. The choice controls where task commands run."}><select value={input.executionMode??'workflow'} onChange={e=>change({executionMode:e.target.value as TaskInput['executionMode']})}>
          <option value="workflow">Use workflow environment</option><option value="local" disabled={!nativeProfile}>Local worktree{!nativeProfile?' (native Weft required)':''}</option><option value="sandbox">Sandbox</option>
        </select></HelpField>
          </div>
          {graphsLoading&&<p role="status">Loading workflows…</p>}
          <ErrorBox error={graphsError}/>
          {graphsError&&onReloadGraphs&&<Btn onClick={onReloadGraphs}>Retry workflows</Btn>}
          {projects.loading&&<p role="status">Loading registered projects…</p>}
          <ErrorBox error={projects.error}/>
          {projects.error&&<Btn onClick={()=>void projects.reload()}>Retry projects</Btn>}
          <div className="task-import-entry">
            <Btn aria-expanded={importOpen} onClick={()=>setImportOpen(!importOpen)}>{importOpen?'Close repository import':'Import repository from URL'}</Btn>
            <p className="muted small">Import a Git snapshot when the repository is not registered yet. This requires administrator approval and does not start agents.</p>
          </div>
          {importOpen&&<RepositoryImport nativeProfile={nativeProfile} canEdit={canStart&&!busy} canSelect={canEdit&&!busy} initialURL={saved?.resolution?.repository} onProject={project=>{change({projectId:project.id,baseCommit:project.head});void projects.reload();}}/>}
          <p className="muted small">Workflow environment inherits agent isolation. Sandbox requires Docker for every enabled agent; local worktrees require native Weft and do not isolate processes or network access. <a href="https://woliveiras.com/posts/why-git-worktrees-matter-coding-agents/" target="_blank" rel="noreferrer">About worktrees</a></p>
          {input.executionMode==='sandbox'&&<p>Sandbox requires all enabled agents to use Docker isolation and the configured Docker/Colima daemon to be available. Weft never falls back to local execution.</p>}
        </section>
        <section className="card form task-editor-section" aria-labelledby="task-routing-heading">
          <header><h2 id="task-routing-heading">3. Input &amp; delivery</h2><p className="muted">Confirm the receiving agent, message format and final delivery. Suggestions do not change workflow permissions.</p></header>
          <div className="task-field-grid">
        <HelpField label={"Entry agent"} error={fieldErrors.nodeId} help={"The workflow node that starts or receives the task. Pick the agent responsible for interpreting the request."}><select required value={input.nodeId} onChange={e=>change({nodeId:e.target.value})}>
          <option value="">Select entry agent</option>{input.nodeId&&!selected?.spec.nodes.some(n=>n.id===input.nodeId)&&<option value={input.nodeId}>{input.nodeId} (unavailable)</option>}
          {selected?.spec.nodes.filter(n=>!n.disabled).map(n=><option key={n.id} value={n.id}>{n.name||n.id}</option>)}
        </select></HelpField>
        <HelpField label={"Result agent"} help={"The workflow node expected to produce the final task result. Use a distinct node when the workflow has a separate finishing stage."}><select value={input.resultNodeId??''} onChange={e=>change({resultNodeId:e.target.value})}>
          <option value="">Same as entry</option>
          {selected?.spec.nodes.filter(n=>!n.disabled).map(n=><option key={n.id} value={n.id}>{n.name||n.id}</option>)}
        </select></HelpField>
          </div>
        <HelpField label={"Input delivery"} help={"Choose how to send the request to the workflow. A channel delivers a structured message to eligible receivers; direct entry targets the selected agent."}><select value={input.channelId??''} onChange={e=>{const c=selected?.spec.channels?.find(c=>c.id===e.target.value);change({channelId:c?.id,messageType:c?(c.types?.length===1&&!/[?*]/.test(c.types[0])?c.types[0]:''):undefined,payloadJson:c?JSON.stringify({title:input.title,description:input.request},null,2):undefined,...(c?{nodeId:c.to?.[0]??''}:{})});}}>
          <option value="">Direct agent instruction</option>{selected?.spec.channels?.filter(c=>!c.from&&c.to?.length===1&&(!c.type||c.type==='queue')&&!c.when).map(c=><option key={c.id} value={c.id}>{c.id} → {c.to?.[0]}</option>)}
        </select></HelpField>
        {input.channelId&&<><HelpField label={"Message type"} error={fieldErrors.messageType} help={"Use the message type expected by the workflow\u2019s channel or routing conditions. For example, issue.opened must match the type consumed by the workflow."}><input required maxLength={128} value={input.messageType??''} onChange={e=>change({messageType:e.target.value})}/></HelpField><HelpField label={"Input payload (JSON)"} error={fieldErrors.payloadJson} help={"Additional structured data delivered with the request. Use valid JSON and fields the receiving workflow understands."}><textarea required rows={6} maxLength={16384} value={input.payloadJson??''} onChange={e=>change({payloadJson:e.target.value})}/></HelpField><p>The selected channel receives this JSON. Request text is retained for context; it is not sent separately. Check the workflow's expected message type and fields.</p></>}
        <TaskIssueSource source={input.source} grants={selected?.spec.nodes.find(n=>n.id===input.nodeId)?.tools??selected?.spec.defaults?.tools??[]} onChange={source=>change({source})}/>
        <SCMHandOffField value={input.scmConnection??''} onChange={scmConnection=>change({scmConnection:scmConnection||undefined})}/>
        </section>
      </fieldset>
      <TaskReadiness input={input} graph={selected} project={projects.data?.find(p=>p.id===input.projectId)} projectsLoading={projects.loading} projectsError={projects.error} modelState={modelState} review={review} admin={admin} onRepair={destination=>void repair(destination)} busy={busy}/>
      <section className="card task-editor-actions" aria-label="Save and review task">
        {input.graphId&&<TaskBudgetPreview workflow={input.graphId} project={input.projectId}/>}
        <p>Save keeps an editable draft. Review checks requirements before you authorize execution; mandatory approvals and installation restrictions still apply.</p>
        <ErrorBox error={error}/>
        <div className="task-editor-action-row">
          <div className="row"><Btn type="submit" disabled={!canEdit||busy}>Save draft</Btn>
          <Btn tone="primary" disabled={!input.request.trim()||busy||!canEdit} aria-busy={submitting} onClick={()=>void submit()}>{submitting?'Reviewing task…':'Review task'}</Btn>
          <Btn disabled={!saved||dirty||busy} onClick={()=>void inspect()}>Review requirements</Btn></div>
          {saved&&<TaskDraftDelete task={saved} canEdit={canEdit} disabled={busy} onDeleted={()=>{setDirty(false);location.hash='#/tasks';}}/>}
        </div>
        <p className="task-request-status" role="status" aria-atomic="true">{submitting?'Reviewing the saved task and execution requirements. No agents are starting…':submissionFeedback?submissionFeedback.message:dirty?'Unsaved changes. Save draft before reviewing authorization.':saved?'Draft saved. No agents have been started.':'No agents have been started.'}</p>
        {submissionFeedback?.configure&&<Btn onClick={openSettings}>Complete task setup</Btn>}
        {review?.canPrepareEnvironment&&<aside className="task-environment-next-step" aria-label="Environment preparation required">
          <h3>Prepare the task environment</h3>
          <p>This task needs its Docker environment before agents can run. You can prepare it from this page.</p>
          <ol><li>Review the build definition and confirm preparation.</li><li>Approve the build in Approvals if requested.</li><li>After the build, review the prepared environment and start the task.</li></ol>
          <Btn onClick={openEnvironmentReview}>Review environment preparation</Btn>
        </aside>}
      </section>
    </form>
    {saved&&savedMissing.length===0&&<TaskAuthorization key={`${saved.id}:${saved.revision}`} taskId={saved.id} canEdit={canStart} configurationIssue={dirty?'Save your task changes before reviewing authorization.':missing.length>0?`Task setup is incomplete: ${missing.join(', ')}.`:undefined} onConfigure={canEdit?openSettings:undefined} onChanged={()=>{setReview(undefined);setConfirmed(false);}}/>}
    {review&&savedMissing.length>0&&<article className="card" aria-label="Task requirements"><h2>Requirements</h2>
      {review.blockers.map(b=><p key={b}>{b}</p>)}
      <p>Complete and save the task configuration before reviewing execution requirements. The execution environment and agent permissions have not been reviewed yet.</p>
      <Btn onClick={openSettings}>Review task settings</Btn>
    </article>}
    {review&&savedMissing.length===0&&<article className="card" aria-label="Task requirements"><h2>Requirements</h2>
      <p>Current workflow version: {review.currentGraphVersion} · Requested environment: {review.task.executionMode||'workflow'}</p>
      {review.canPrepareEnvironment&&<section ref={environmentRef} tabIndex={-1} className="task-environment-review" aria-label="Review task environment preparation">
        <h3>Review the environment build</h3>
        <p>Weft will prepare a Docker environment on its sandbox daemon. Building can download images and dependencies and execute the setup commands shown below.</p>
        <p><strong>Preparation does not start agents.</strong> The task stays waiting for any required approval. After the build, you will review the prepared environment before starting.</p>
        {review.environmentDefinition&&<section aria-label="Task environment definition">
          <p>{review.environmentDefinition.image?`Image: ${review.environmentDefinition.image}`:'Build from the workflow Dockerfile.'}</p>
          <details><summary>Build definition and network settings</summary><pre>{JSON.stringify(review.environmentDefinition,null,2)}</pre></details>
        </section>}
        {!canStart&&<p>An administrator must prepare this task environment.</p>}
        <label className="check"><input type="checkbox" checked={confirmed} onChange={e=>setConfirmed(e.target.checked)}/>I reviewed the workflow and authorize a separate execution copy.</label>
        <Btn tone="primary" disabled={!canStart||!confirmed||busy||modelBlocked} aria-busy={preparingEnvironment} onClick={()=>void prepareEnvironment()}>{preparingEnvironment?'Preparing environment…':'Prepare task environment'}</Btn>
        {preparingEnvironment&&<p role="status">Requesting environment preparation. Any required build approval will appear in Approvals. No agents are starting.</p>}
      </section>}
      {review.blockers.filter(b=>!review.canPrepareEnvironment||!isTaskEnvironmentPreparationNotice(b)).map(b=><p key={b}>{b}</p>)}
      {[...(review.validation.errors??[]),...(review.validation.warnings??[])].filter(v=>!review.canPrepareEnvironment||v.path!=='spec.environment'||!isTaskEnvironmentPreparationNotice(v.message)).map((v,i)=><p key={i}>{v.path}: {v.message}</p>)}
      {review.canPrepareEnvironment&&<details><summary>Technical validation details</summary>{review.blockers.map((b,i)=><p key={i}>{b}</p>)}{[...(review.validation.errors??[]),...(review.validation.warnings??[])].map((v,i)=><p key={`validation-${i}`}>{v.path}: {v.message}</p>)}</details>}
      {review.project&&<p>Project: {review.project.name} · Source: {review.project.path} · Base: {input.baseCommit} · Task worktree: {review.workspacePath}. Commands use the workflow's configured isolation and permissions.</p>}
      <p>Execution copy: {review.executionGraphId}. Starting runs the whole workflow with a fresh managed workspace. Input goes through the selected channel or directly to the entry agent; the selected result agent supplies the result.</p>
      <p>Entry agent model: {review.model?.provider||'runtime default'} / {review.model?.name||'runtime default'}. Isolation: {review.isolation||'process'}. Process execution uses the server machine and is not a sandbox.</p>
      <TaskRequirementRepairs agents={review.agents??[]} taskId={saved?.id} canConfigure={admin}/>
      {review.environmentDefinition&&!review.canPrepareEnvironment&&<section aria-label="Task environment definition"><h3>Environment to prepare</h3><p>Building may execute setup commands under the installation sandbox policies and limits. Build completion does not start agents.</p><pre>{JSON.stringify(review.environmentDefinition,null,2)}</pre></section>}
      <p>Tool permissions remain unchanged. Every call still passes gateway policies and approvals.</p>
      <details><summary>All workflow agents and permissions</summary>{review.agents?.map(agent=><section key={agent.id}><h3>{agent.id}</h3><p>{agent.model?.provider} / {agent.model?.name} · {agent.isolation}</p><pre>{JSON.stringify(agent.grants??[],null,2)}</pre></section>)}</details>
      {!review.canPrepareEnvironment&&<label className="check"><input type="checkbox" checked={confirmed} onChange={e=>setConfirmed(e.target.checked)}/>I reviewed the workflow and authorize a separate execution copy.</label>}
      {!canStart&&<p>An administrator must start this task, because it creates an execution copy.</p>}
      <Btn tone="primary" disabled={!canStart||!review.canStart||!confirmed||busy||modelBlocked} onClick={()=>void start()}>Start task</Btn>
      <a href={taskRepairLink(`#/g/${encodeURIComponent(input.graphId)}/agent?agent=${encodeURIComponent(input.nodeId)}&section=general`,review.task.id)}>Review agent configuration</a>
    </article>}
  </>;
}
