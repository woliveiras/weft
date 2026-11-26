import {RepositoryPicker,RepositoryRows,repositoryBindings} from './RepositoryPicker';
import {taskInputIdentity,taskLaunchIdentity,type TaskLaunchReview} from './taskLaunch';
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
import type {Project, TaskDraft, TaskInput, TaskReview} from '../api/tasks';
import {useAsync} from '../hooks/useData';
import {Btn, ErrorBox} from './common';
import {TasksList} from './TasksList';
import {isTaskEnvironmentPreparationNotice,defaultTaskWorkflowVersion,workflowTaskDefaults,updateTaskInput,outcomeRequest,promptTitle,missingTaskConfiguration,submissionDraftFeedback,taskConfigurationFieldErrors} from './taskDefaults';
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
  return {repositories:draft.repositories,source:draft.source,executionMode:draft.executionMode,title:draft.title,request:draft.request,outcome:draft.outcome,graphId:draft.graphId,graphVersion:draft.graphVersion,nodeId:draft.nodeId,resultNodeId:draft.resultNodeId,scmConnection:draft.scmConnection,projectId:draft.projectId,baseCommit:draft.baseCommit,channelId:draft.channelId,messageType:draft.messageType,payloadJson:draft.payloadJson};
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
    <header className="page-head"><div><h1>{create?'New task':editing?'Task draft':detail.data?.title || 'Task'}</h1>{editing&&<p className="muted">Describe the work, choose a workflow and repository, then run your task.</p>}</div></header>
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
function TaskBudgetPreview({preview}:{preview:ReturnType<typeof useAsync<Awaited<ReturnType<typeof api.budgetPreview>>|undefined>>}) {
  const data=preview.data;
  if(preview.loading)return <p role="status">Loading applicable budgets…</p>;
  if(preview.error)return <aside aria-label="Budget unavailable"><ErrorBox error={preview.error}/><Btn onClick={()=>void preview.reload()}>Retry budget preview</Btn></aside>;
  if(!data)return null;
  return <section className="integration-note" aria-label="Budget">
    <h3>Budget</h3>
    {data.taskRunBudgetMicros===0&&data.budgets.length===0&&<p>No task or monthly spending limit is configured.</p>}
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
  const openSettings=()=>{setAdvancedOpen(true);settingsRef.current?.scrollIntoView?.({block:'start',behavior:'smooth'});settingsRef.current?.focus();};
  const [customTitle,setCustomTitle]=useState(!!draft);
  const projects=useAsync(()=>api.projects(),[]);
  const [saved,setSaved] = useState(draft);
  const [reviewResult,setReview] = useState<TaskReview>();
  const [launchReview,setLaunchReview] = useState<TaskLaunchReview>();
  const [advancedOpen,setAdvancedOpen]=useState(false);
  const [accessOpen,setAccessOpen]=useState(false);
  const launchLock=useRef(false);
  const editEpoch=useRef(0);
  const launchRef=useRef<HTMLElement>(null);
  const [error,setError] = useState<string>();
  const [busy,setBusy] = useState(false);
  const [submitting,setSubmitting] = useState(false);
  const [preparingEnvironment,setPreparingEnvironment] = useState(false);
  const [submissionFeedback,setSubmissionFeedback] = useState<ReturnType<typeof submissionDraftFeedback>>(()=>draft?.submitted?submissionDraftFeedback(draft):undefined);
  const [confirmed,setConfirmed] = useState(false);
  const repoConnections=useAsync(()=>input.repositories?.length?api.scmConnections():Promise.resolve([]),[!!input.repositories?.length]);
  const [dirty,setDirty] = useState(false);
  const budgets=useAsync(()=>input.graphId?api.budgetPreview(input.graphId,input.projectId,...(input.repositories?.length?[input.repositories]:[])):Promise.resolve(undefined),[input.graphId,input.projectId,input.repositories]);
  const review=!dirty&&reviewResult?.task.revision===saved?.revision?reviewResult:undefined;
  const selected=graphs.find(g=>g.graph.metadata.id===input.graphId)?.graph;
  useEffect(()=>{
    if(!canEdit)return;
    const next=defaultTaskWorkflowVersion(input,selected);
    if(next===input)return;
    editEpoch.current++;setLaunchReview(undefined);setInput(next);setDirty(true);setReview(undefined);setConfirmed(false);
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
  const change = (next: Partial<TaskInput>, explicit: boolean | (keyof TaskInput)[] = true) => {
    editEpoch.current++;
    setLaunchReview(undefined);
    if (explicit) {
      const keys = Array.isArray(explicit) ? explicit : Object.keys(next) as (keyof TaskInput)[];
      for (const key of keys) contextChoices.current.add(key);
    }
    setSubmissionFeedback(undefined);
    setInput(old => updateTaskInput(old, next));
    setDirty(true);
    setReview(undefined);
    setConfirmed(false);
  };
  const entryProject = !contextApplied.current && !contextChoices.current.has('projectId') ? entry?.project : undefined;
  const singularId = input.projectId || entryProject;
  const selectedIds = input.repositories?.map(row => row.projectId) ?? (singularId ? [singularId] : []);
  function selectRepositories(ids: string[], catalog: Project[] = projects.data ?? []) {
    // Keep the single-checkout layout for existing workflows, including process execution.
    // Once a draft has named checkouts, retain its aliases, check pins and delivery bindings.
    const keepEnvironment = contextChoices.current.has('executionMode') || !!saved?.executionMode;
    if (input.repositories === undefined && ids.length <= 1) {
      const project = catalog.find(p => p.id === ids[0]);
      change({projectId: ids[0], baseCommit: project?.head, repositories: undefined,
        ...(!keepEnvironment && {executionMode: project?.executionMode})}, ['projectId', 'baseCommit', 'repositories']);
      return;
    }
    const previous = input.repositories ?? repositoryBindings(singularId ? [singularId] : [], catalog)
      .map(row => ({...row, baseCommit: input.baseCommit || row.baseCommit}));
    change({repositories: repositoryBindings(ids, catalog, previous), projectId: undefined, baseCommit: undefined,
      scmConnection: undefined, ...(singularId && !keepEnvironment ? {executionMode: undefined} : {})},
      ['repositories', 'projectId', 'baseCommit', 'scmConnection']);
  }
  useEffect(()=>{
    if(contextApplied.current||!entry||projects.loading||projects.error||graphsLoading||graphsError)return;
    contextApplied.current=true;
    const contextual=applyTaskEntryContext(input,{workflow:contextChoices.current.has('graphId')?undefined:entry.workflow,project:contextChoices.current.has('projectId')?undefined:entry.project},graphs.map(g=>g.graph),projects.data??[]);
    const next=contextual===input?input:{...contextual,...Object.fromEntries([...contextChoices.current].map(key=>[key,input[key]]))};
    if(next!==input){editEpoch.current++;setInput(next);setDirty(true);}
  },[entry,projects.loading,projects.error,projects.data,graphsLoading,graphsError,graphs,input]);
  useEffect(()=>{
    if(draft||!input.projectId||input.baseCommit||!projects.data)return;
    const project=projects.data.find(p=>p.id===input.projectId);
    if(project){editEpoch.current++;setInput(old=>({...old,baseCommit:project.head,executionMode:old.executionMode??project.executionMode}));}
  },[draft,input.projectId,input.baseCommit,projects.data]);
  useEffect(()=>{
    if(draft||saved||!projects.data||input.projectId||entry?.project)return;
    if(input.repositories?.some(r=>!r.baseCommit && projects.data?.some(p=>p.id===r.projectId)) && !contextChoices.current.has('repositories')){
      setInput(old=>({...old,repositories:repositoryBindings(old.repositories!.map(r=>r.projectId),projects.data!,old.repositories)}));
      return;
    }
    const ids=selected?.spec.task?.projectIds;
    if(!input.repositories && ids?.length && !contextChoices.current.has('repositories')){
      setInput(old=>({...old,repositories:repositoryBindings(ids,projects.data!),scmConnection:undefined}));
    }
  },[draft,saved,projects.data,input.projectId,input.repositories,entry?.project,selected]);
  const repair=async(destination:string)=>{
    setBusy(true);setError(undefined);
    try {
      const task=dirty||!saved?await api.saveTask(input,saved?.id,saved?.revision):saved;
      setSaved(task);setDirty(false);setReview(undefined);setLaunchReview(undefined);setConfirmed(false);
      location.hash=taskRepairLink(destination,task.id);
    }catch(e){setError(`Could not save the task before opening configuration: ${errorMessage(e)}`);}
    finally{setBusy(false);}
  };
  const editRequest=(request:string)=>change({request,...(!customTitle?{title:promptTitle(request)}:{})});
  const save=async()=>{
    setBusy(true);setSubmissionFeedback(undefined);setError(undefined);
    try {const result=await api.saveTask(input,saved?.id,saved?.revision);setSaved(result);setDirty(false);setReview(undefined);setLaunchReview(undefined);if(!saved)location.hash=`#/tasks/${encodeURIComponent(result.id)}`;}
    catch(e){setError(errorMessage(e));}finally{setBusy(false);}
  };
  const resolve=async()=>{
    setBusy(true);setSubmissionFeedback(undefined);setError(undefined);
    try {
      const result=await api.resolveTaskPrompt(input,saved?.id,saved?.revision);
      setLaunchReview(undefined);editEpoch.current++;setSaved(result);setInput({repositories:result.repositories,source:result.source,title:result.title,request:result.request,outcome:result.outcome,graphId:result.graphId,graphVersion:result.graphVersion,nodeId:result.nodeId,resultNodeId:result.resultNodeId,scmConnection:result.scmConnection,channelId:result.channelId,messageType:result.messageType,payloadJson:result.payloadJson,projectId:result.projectId,baseCommit:result.baseCommit,executionMode:result.executionMode});setDirty(false);setReview(undefined);setConfirmed(false);
      if(!saved)location.hash=`#/tasks/${encodeURIComponent(result.id)}`;
    }catch(e){setError(errorMessage(e));}finally{setBusy(false);}
  };
  const inspectLaunch=async(task:TaskDraft):Promise<TaskLaunchReview>=>{
    const checked=await api.reviewTask(task.id);
    // Keep blockers visible even when authorization or budget inspection fails.
    if(checked.task.id!==task.id||checked.task.revision!==task.revision||taskInputIdentity(checked.task)!==taskInputIdentity(task))throw new Error('This task changed during inspection. Reload it before running.');
    setReview(checked);
    const [authorization,budget]=await Promise.all([
      api.taskAuthorization(task.id),api.budgetPreview(task.graphId,task.projectId,...(task.repositories?.length?[task.repositories]:[])),
    ]);
    if(!authorization.scope)throw new Error('Authorization review is incomplete. Retry Run task before starting.');
    return {review:checked,authorization,budget};
  };
  useEffect(()=>{
    if(launchLock.current||!saved||dirty||saved.status!=='draft'||missingTaskConfiguration(saved).length)return;
    let disposed=false;const epoch=editEpoch.current;
    void (async()=>{
      try{
        const checked=await api.reviewTask(saved.id);
        if(disposed||editEpoch.current!==epoch)return;
        if(checked.task.id!==saved.id||checked.task.revision!==saved.revision||taskInputIdentity(checked.task)!==taskInputIdentity(saved))throw new Error('This task changed during inspection. Reload it before running.');
        setReview(checked);
        const [authorization,budget]=await Promise.all([api.taskAuthorization(saved.id),api.budgetPreview(saved.graphId,saved.projectId,...(saved.repositories?.length?[saved.repositories]:[]))]);
        if(!authorization.scope)throw new Error('Authorization review is incomplete. Retry Run task before starting.');
        if(!disposed&&editEpoch.current===epoch)setLaunchReview({review:checked,authorization,budget});
      }catch(cause){if(!disposed&&editEpoch.current===epoch)setError(`Could not check task readiness: ${errorMessage(cause)}`);}
    })();
    return()=>{disposed=true;};
  },[saved?.id,saved?.revision,dirty,reviewOnLoad]);
  const submit=async()=>{
    if(launchLock.current)return;
    launchLock.current=true;setBusy(true);setSubmitting(true);setSubmissionFeedback(undefined);setError(undefined);
    const displayed=!dirty&&review?launchReview:undefined;
    const launchEpoch=editEpoch.current;
    try{
      if(missing.length>0){
        setAdvancedOpen(true);
        setSubmissionFeedback(submissionDraftFeedback({...saved,...input,status:'draft'} as TaskDraft));
        openSettings();return;
      }
      const task=dirty||!saved?await api.saveTask(input,saved?.id,saved?.revision):saved;
      setSaved(task);
      if(editEpoch.current!==launchEpoch){setDirty(true);throw new Error('Task settings changed while saving. Your edits are retained; run again to review them.');}
      // Registration may resolve an omitted project base; surface that concrete commit before consent.
      const acknowledgedInput=!input.baseCommit&&input.projectId&&task.projectId===input.projectId&&task.baseCommit?{...input,baseCommit:task.baseCommit}:input;
      if(acknowledgedInput!==input)setInput(acknowledgedInput);
      if(taskInputIdentity(task)!==taskInputIdentity(acknowledgedInput)){
        setDirty(true);throw new Error('The saved task settings differ from your request. Your edits are retained; review and save them again before running.');
      }
      setDirty(false);
      const checked=await inspectLaunch(task);
      if(editEpoch.current!==launchEpoch){setDirty(true);throw new Error('Task settings changed during inspection. Your edits are retained; run again to review them.');}
      setLaunchReview(checked);
      const displayedIdentity=displayed&&taskLaunchIdentity(displayed);
      const sameScope=displayedIdentity&&displayed.review.canStart&&displayedIdentity===taskLaunchIdentity(checked);
      if(sameScope&&checked.review.canStart&&canStart&&!modelBlocked){
        setSubmissionFeedback({message:'Starting the task with the displayed settings…',configure:false});
        const started=await api.startTask(task.id,checked.review.task.revision);
        setSaved(started);setReview(undefined);setLaunchReview(undefined);
        if(!saved)location.hash=`#/tasks/${encodeURIComponent(started.id)}`;
        return;
      }
      setConfirmed(false);
      const message=checked.review.canPrepareEnvironment?'Review the environment build below. Preparing it does not start agents.':checked.review.canStart?'Task settings are ready below. Click Run task to start with these permissions and budget.':'Task has not started. Resolve the requirements below, then click Run task.';
      setSubmissionFeedback({message,configure:false});
      launchRef.current?.scrollIntoView?.({block:'nearest',behavior:'smooth'});
      if(!saved)location.hash=`#/tasks/${encodeURIComponent(task.id)}`;
    }catch(cause){setLaunchReview(undefined);setError(errorMessage(cause));}
    finally{launchLock.current=false;setBusy(false);setSubmitting(false);}
  };
  const prepareEnvironment=async()=>{
    if(launchLock.current||!saved||!review||!confirmed||!review.canPrepareEnvironment)return;
    launchLock.current=true;setBusy(true);setPreparingEnvironment(true);setSubmissionFeedback(undefined);setError(undefined);
    try{setSaved(await api.prepareTaskEnvironment(saved.id,review.task.revision));setReview(undefined);}
    catch(e){setError(errorMessage(e));try{setSaved(await api.task(saved.id));}catch(readError){setError(`${errorMessage(e)}; ${errorMessage(readError)}`);}}finally{launchLock.current=false;setBusy(false);setPreparingEnvironment(false);}
  };
  if(saved&&saved.status!=='draft')return <TaskTracking task={saved} canControl={canStart} canDecide={canEdit} canRetry={canEdit} nativeProfile={nativeProfile}/>;
  return <>
    <a className="task-back-link" href="#/tasks">Back to tasks</a>
    {saved && <TaskRetryOrigin task={saved}/>}
    {saved?.environmentApprovalId&&<p>Environment preparation requires a decision in <a href="#/approvals">Approvals</a>. The issue remains in progress.</p>}
    {saved?.launchError&&<p role="alert">{saved.launchError}</p>}
    {modelBlocked&&<NoModelProvider admin={admin} busy={busy} onRepair={()=>void repair('#/models')}/>}
    <form noValidate className="task-request-form" aria-label="Task request" onSubmit={e=>{e.preventDefault();void submit();}}>
      <section className="card task-editor-actions task-launch-header" aria-label="Run task">
        <ErrorBox error={error}/>
        <div className="task-editor-action-row">
          <div className="row">
            <Btn tone="primary" disabled={!input.request.trim()||busy||!canEdit} aria-busy={submitting} onClick={()=>void submit()}>{submitting?'Checking task…':input.repositories?.length && (!launchReview || dirty || !review?.canStart)?'Review task':'Run task'}</Btn>
            <Btn disabled={!canEdit||busy} onClick={()=>void save()}>Save draft</Btn>
          </div>
          {saved&&<TaskDraftDelete task={saved} canEdit={canEdit} disabled={busy} onDeleted={()=>{setDirty(false);location.hash='#/tasks';}}/>}
        </div>
        <p className="task-request-status" role="status" aria-atomic="true">{submitting?'Saving changes and checking execution requirements…':submissionFeedback?submissionFeedback.message:dirty?'Run task saves these changes and checks what is needed to start.':saved?'Draft saved. Run task checks requirements and starts with the displayed permissions when ready.':'Run task saves your request and checks requirements. No agents have started.'}</p>
        {submissionFeedback?.configure&&<Btn onClick={openSettings}>Complete task setup</Btn>}
        {!canStart&&<p>An administrator must start or prepare a task. You can save this draft for them.</p>}
      </section>
      <fieldset disabled={!canEdit||busy}>
        <section className="card form task-editor-section" aria-labelledby="task-request-heading">
          <header><h2 id="task-request-heading">Describe the work</h2><p className="muted">Give the agents a clear request and the result you expect.</p></header>
        <HelpField label={"Request"} help={"Describe what work should be done and how to judge the result. Include file names or constraints that the agent cannot infer from the repository."}><textarea required rows={8} placeholder="Describe the work you want the agents to do and the result you expect." maxLength={16384} value={input.request} onChange={e=>editRequest(e.target.value)}/></HelpField>
        </section>
        <section ref={settingsRef} tabIndex={-1} className="card form task-editor-section" aria-labelledby="task-settings-heading">
          <header><h2 id="task-settings-heading">Workflow &amp; repositories</h2><p className="muted">Choose the workflow and files this task will use.</p></header>
          <div className="task-field-grid">
        <HelpField label={"Workflow"} error={fieldErrors.graphId} help={"Choose the saved workflow to run this task. Review the agents, their versions and the permissions used by the task."}><select required value={input.graphId} onChange={e=>{
          const g=graphs.find(g=>g.graph.metadata.id===e.target.value)?.graph;
          const defaults=workflowTaskDefaults(g,input.title,input.request);
          // Selecting a workflow accepts its routing suggestions, not its repository over an explicit entry choice.
          const keepRepositories=input.repositories!==undefined && (contextChoices.current.has('repositories')||draft?.repositories!==undefined);
          const keepProject=contextChoices.current.has('projectId')||!!draft;
          const projectId=keepRepositories?undefined:keepProject?input.projectId:entry?.project||defaults.projectId;
          const suggested=g?.spec.task?.projectIds;
          let repositories=keepRepositories?input.repositories:undefined;
          if(!keepRepositories&&!projectId&&suggested)repositories=repositoryBindings(suggested,projects.data??[]);
          const project=projects.data?.find(p=>p.id===projectId);
          contextChoices.current.add('graphId');
          change({...defaults,graphId:e.target.value,graphVersion:g?.metadata.version??0,projectId,
            repositories,
            baseCommit:projectId===input.projectId?input.baseCommit:project?.head,
            executionMode:input.executionMode??project?.executionMode,
            scmConnection:repositories?undefined:contextChoices.current.has('scmConnection')?input.scmConnection:defaults.scmConnection},false);
        }}>
          <option value="">Select workflow</option>{!selected&&input.graphId&&<option value={input.graphId}>{input.graphId} (unavailable)</option>}
          {graphs.map(({graph:g})=><option key={g.metadata.id} value={g.metadata.id}>{g.metadata.name||g.metadata.id}</option>)}
        </select></HelpField>
        <RepositoryPicker label="Repositories" help="Choose the registered repositories this task will use. An empty selection uses the workflow repository or an empty workspace."
          ids={selectedIds} projects={projects.data ?? []} loading={projects.loading} disabled={!canEdit || busy}
          onChange={selectRepositories}/>
          </div>
          {input.projectId && <small className="muted">Base commit: <code>{input.baseCommit}</code></small>}
          {input.repositories && <><RepositoryRows repos={input.repositories} projects={projects.data ?? []}
            connections={repoConnections.data} onChange={repositories => change({repositories})}/><ErrorBox error={repoConnections.error}/></>}
          {graphsLoading&&<p role="status">Loading workflows…</p>}
          <ErrorBox error={graphsError}/>
          {graphsError&&onReloadGraphs&&<Btn onClick={onReloadGraphs}>Retry workflows</Btn>}
          {projects.loading&&<p role="status">Loading registered repositories…</p>}
          <ErrorBox error={projects.error}/>
          {projects.error&&<Btn onClick={()=>void projects.reload()}>Retry repositories</Btn>}
          <div className="task-import-entry">
            <Btn aria-expanded={importOpen} onClick={()=>setImportOpen(!importOpen)}>{importOpen?'Close repository import':'Import repository from URL'}</Btn>
            <p className="muted small">Import a Git snapshot when the repository is not registered yet. This requires administrator approval and does not start agents.</p>
          </div>
          {importOpen&&<RepositoryImport nativeProfile={nativeProfile} canEdit={canStart&&!busy} canSelect={canEdit&&!busy} initialURL={saved?.resolution?.repository} onProject={project=>{
            selectRepositories([...new Set([...selectedIds, project.id])], [project, ...(projects.data ?? [])]);
            void projects.reload();}}/>}
        </section>
        <details className="card task-advanced" open={advancedOpen||!!input.graphId&&missing.length>0} onToggle={e=>setAdvancedOpen(e.currentTarget.open)}>
          <summary>Advanced settings <span className="muted small">Version, environment, routing and delivery</span></summary>
          <section className="form task-editor-section" aria-label="Task options">
            <div className="task-field-grid">
        <HelpField label={"Goal"} help={"Select the expected delivery format. Use it to make clear whether the task should present findings, make changes or hand off a result."}><select value={input.outcome} onChange={e=>change({outcome:e.target.value,request:outcomeRequest(input,e.target.value),...(!customTitle?{title:promptTitle(outcomeRequest(input,e.target.value))}:{})})}>{taskOutcomes.map(o=><option key={o.id} value={o.id}>{o.label}</option>)}</select><small className="muted">{taskOutcomes.find(o=>o.id===input.outcome)?.description}</small></HelpField>
        <HelpField label="Title" help="A short name used to identify this task in lists and its history. It starts from the first line of your request; editing it keeps your chosen title."><input maxLength={200} required value={input.title} onChange={e=>{setCustomTitle(true);change({title:e.target.value});}}/></HelpField>
            <HelpField label="Workflow version" error={fieldErrors.graphVersion} help="The current saved workflow version is selected by default. Saving pins that version to this task. Existing pins do not change when a workflow is published; select the current version and review again to update a draft.">
              <select required disabled={!selected?.metadata.version} value={input.graphVersion||''} onChange={e=>change({graphVersion:Number(e.target.value)})}>
                {!input.graphVersion&&!selected?.metadata.version&&<option value="">Select a workflow</option>}
                {selected?.metadata.version&&<option value={selected.metadata.version}>Current workflow version (Version {selected.metadata.version})</option>}
                {!!input.graphVersion&&input.graphVersion!==selected?.metadata.version&&<option value={input.graphVersion}>Version {input.graphVersion} (saved)</option>}
              </select>
            </HelpField>
        <HelpField label={"Execution environment"} help={"Use the workflow environment or an eligible project preference when available. The choice controls where task commands run."}><select value={input.executionMode??'workflow'} onChange={e=>change({executionMode:e.target.value as TaskInput['executionMode']})}>
          <option value="workflow">Use workflow environment</option><option value="local" disabled={!nativeProfile}>Local worktree{!nativeProfile?' (native Weft required)':''}</option><option value="sandbox">Sandbox</option>
        </select></HelpField>
            </div>
          <p className="muted small">Workflow environment inherits agent isolation. Sandbox requires Docker for every enabled agent; local worktrees require native Weft and do not isolate processes or network access. <a href="https://woliveiras.com/posts/why-git-worktrees-matter-coding-agents/" target="_blank" rel="noreferrer">About worktrees</a></p>
          {input.executionMode==='sandbox'&&<p>Sandbox requires all enabled agents to use Docker isolation and the configured Docker/Colima daemon to be available. Weft never falls back to local execution.</p>}
          <div className="task-interpretation">
            <Btn disabled={!input.request.trim()||busy||!canEdit} onClick={()=>void resolve()}>Resolve request</Btn>
            <p className="muted small">Optional: ask the default model to suggest task settings and save the request. Your title, repositories and environment choices are retained; input bindings are rebuilt. No agents start.</p>
          </div>
        {saved?.resolution&&<aside aria-label="Request interpretation"><p>{saved.resolution.message}</p>{saved.resolution.repository&&<p>Repository: {saved.resolution.repository}</p>}{['needs_model','model_error'].includes(saved.resolution.state)&&<Btn disabled={busy||!admin} onClick={()=>void repair('#/models')}>Configure models</Btn>}<p>Your original request is preserved. Resolving never grants tool access.</p>{savedMissing.length>0&&<><p>Task setup is incomplete: {savedMissing.join(', ')}. Retry Resolve request, or configure it manually without using the model.</p><Btn onClick={openSettings}>Configure task manually</Btn></>}</aside>}
          </section>
        <section className="card form task-editor-section" aria-labelledby="task-routing-heading">
          <header><h2 id="task-routing-heading">Input &amp; delivery</h2><p className="muted">Confirm the receiving agent, message format and final delivery. Suggestions do not change workflow permissions.</p></header>
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
        {!input.repositories&&<SCMHandOffField value={input.scmConnection??''} onChange={scmConnection=>change({scmConnection:scmConnection||undefined})}/>}
        </section>
        </details>
      </fieldset>
      <TaskReadiness input={input} graph={selected} project={projects.data?.find(p=>p.id===input.projectId)} projectsLoading={projects.loading} projectsError={projects.error} modelState={modelState} review={review} admin={admin} onRepair={destination=>void repair(destination)} busy={busy}/>
      <section ref={launchRef} className="card task-editor-actions" aria-label="Launch details">
        {input.graphId&&<TaskBudgetPreview preview={launchReview&&review?{...budgets,data:launchReview.budget,error:undefined,loading:false}:budgets}/>}
        {launchReview&&review&&<section className="task-launch-review" aria-label="Launch review">
          <h3>{review.canStart&&taskLaunchIdentity(launchReview)?'Ready to run':'Before this task can run'}</h3>
          <p>Access: <strong>{launchReview.authorization.record?.mode==='full'?'Full access':launchReview.authorization.record?.mode==='workspace'?'Workspace access':'Ask for approval'}</strong>{!launchReview.authorization.record&&' — Ask for approval (default)'}. Mandatory approvals and installation restrictions still apply.</p>
          <p>Workflow: <strong>{review.task.graphId} · Version {review.task.graphVersion}</strong>. {review.task.repositories?.length?'Repositories: '+review.task.repositories.map(r=>r.alias).join(', '):'Repository: '+(review.project?.name||review.task.projectId||selected?.spec.workspace?.repo||'Empty managed workspace')}{review.task.baseCommit&&<> · Commit <code>{review.task.baseCommit}</code></>}.</p>
          {review.task.repositories&&<RepositoryRows repos={review.task.repositories} projects={projects.data??[]}/>}
          <p>Run task authorizes a separate execution with the request, repository and workflow version shown above. It does not grant additional tools.</p>
          <ul className="task-launch-grants" aria-label="Effective agent permissions">{(review.agents??[]).map(agent=><li key={agent.id}><strong>{agent.id}</strong> · {agent.isolation||'process'} · {agent.model?.provider||'default provider'} / {agent.model?.name||'default model'}
            <ul>{(agent.grants??[]).map((grant,index)=><li key={index}><code>{grant.name}</code>{Object.entries(grant).filter(([key])=>key!=='name').map(([key,value])=><span key={key}> · {key}: {typeof value==='object'?JSON.stringify(value):String(value)}</span>)}</li>)}</ul>
          </li>)}</ul>
          {launchReview.authorization.record&&(launchReview.authorization.record.scope!==launchReview.authorization.scope||launchReview.authorization.record.taskId!==review.task.id)&&<p role="alert">Saved authorization is stale. Open Access settings and confirm the current scope before running.</p>}
          {launchReview.budget.budgets.some(item=>item.reached||item.paused||item.invalid)&&<p role="alert">A spending limit prevents this task from starting. Review the budget limits before running.</p>}
          <Btn small disabled={busy} onClick={()=>setAccessOpen(true)}>Change access mode</Btn>
        </section>}
      </section>
    </form>
    {review&&<article className="card task-launch-blockers" aria-label="Task requirements"><h2>{review.canPrepareEnvironment?'Prepare the environment':review.canStart?'Execution details':'Action required'}</h2>
      {review.canPrepareEnvironment&&<section ref={environmentRef} tabIndex={-1} className="task-environment-review" aria-label="Review task environment preparation">
        <p>Building can download images and dependencies and execute the setup commands below on the Weft sandbox daemon. <strong>Preparation does not start agents.</strong></p>
        {review.environmentDefinition&&<section aria-label="Task environment definition">
          <p>{review.environmentDefinition.image?`Image: ${review.environmentDefinition.image}`:'Build from the workflow Dockerfile.'}</p>
          <details><summary>Build definition and network settings</summary><pre>{JSON.stringify(review.environmentDefinition,null,2)}</pre></details>
        </section>}
        {!canStart&&<p>An administrator must prepare this task environment.</p>}
        <label className="check"><input type="checkbox" checked={confirmed} onChange={e=>setConfirmed(e.target.checked)}/>I reviewed the environment definition and authorize preparing it.</label>
        <Btn tone="primary" disabled={!canStart||!confirmed||busy||modelBlocked||!launchReview} aria-busy={preparingEnvironment} onClick={()=>void prepareEnvironment()}>{preparingEnvironment?'Preparing environment…':'Prepare task environment'}</Btn>
        {preparingEnvironment&&<p role="status">Requesting environment preparation. Any required build approval will appear in Approvals. No agents are starting.</p>}
      </section>}
      {review.blockers.filter(b=>!review.canPrepareEnvironment||!isTaskEnvironmentPreparationNotice(b)).map(b=><p key={b}>{b}</p>)}
      {[...(review.validation.errors??[]),...(review.validation.warnings??[])].filter(v=>!review.canPrepareEnvironment||v.path!=='spec.environment'||!isTaskEnvironmentPreparationNotice(v.message)).map((v,i)=><p key={i}>{v.path}: {v.message}</p>)}
      <TaskRequirementRepairs agents={review.agents??[]} taskId={saved?.id} canConfigure={admin}/>
      <details><summary>Technical execution details</summary>
        <p>Current workflow version: {review.currentGraphVersion} · Requested environment: {review.task.executionMode||'workflow'}</p>
        <p>Execution copy: {review.executionGraphId}. Input: {input.nodeId}{input.channelId?` via ${input.channelId}`:' · direct instruction'}. Result: {input.resultNodeId||input.nodeId}.</p>
        {review.project&&<p>Project: {review.project.name} · Base: {input.baseCommit} · Task worktree: {review.workspacePath}</p>}
        {review.blockers.map((b,i)=><p key={i}>{b}</p>)}
        {[...(review.validation.errors??[]),...(review.validation.warnings??[])].map((v,i)=><p key={`validation-${i}`}>{v.path}: {v.message}</p>)}
      </details>
    </article>}
    {saved&&savedMissing.length===0&&<details className="card task-access-settings" open={accessOpen} onToggle={e=>setAccessOpen(e.currentTarget.open)}>
      <summary>Access settings <span className="muted small">Ask by default; change authorization explicitly</span></summary>
        {accessOpen&&<TaskAuthorization key={`${saved.id}:${saved.revision}`} taskId={saved.id} canEdit={canStart&&!busy} configurationIssue={dirty?'Save your task changes before changing authorization.':undefined} onConfigure={canEdit?openSettings:undefined} onChanged={()=>{setReview(undefined);setLaunchReview(undefined);setConfirmed(false);}}/>}
    </details>}
  </>;
}
