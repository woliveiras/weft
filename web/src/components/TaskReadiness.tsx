import {useState} from 'react';
import type {Graph} from '../api/types';
import type {TaskInput,Project,TaskReview} from '../api/tasks';
import {missingTaskConfiguration} from './taskDefaults';
import {Btn} from './common';
import './taskReadiness.css';

export function TaskReadiness({input,graph,project,projectsLoading,projectsError,modelState,review,admin,onRepair,busy}:{
 input:TaskInput;graph?:Graph;project?:Project;projectsLoading:boolean;projectsError?:string;
 modelState:'loading'|'unavailable'|'missing'|'configured';review?:TaskReview;admin:boolean;
 onRepair:(destination:string)=>void;busy:boolean;
}) {
 const [goal,setGoal]=useState('work');
 const nodes=graph?.spec.nodes.filter(n=>!n.disabled)??[];
 const isolation=[...new Set(nodes.map(n=>n.isolation?.mode??graph?.spec.defaults?.isolation?.mode??'process'))].join(', ');
 const repo=project?.repositoryUrl||project?.path||graph?.spec.workspace?.repo;
 const missing=missingTaskConfiguration(input);
 const rows=[
  ['Workflow & input',missing.length?`Needs configuration: ${missing.join(', ')}`:`${graph?.metadata.name||input.graphId} · Version ${input.graphVersion} · ${input.nodeId}${input.channelId?` via ${input.channelId} (${input.messageType})`:' · direct instruction'}`],
  ['Repository',input.projectId?(projectsLoading?'Loading project…':projectsError?`Unavailable: ${projectsError}`:!project?`Project ${input.projectId} is unavailable. Choose another project or retry discovery.`:`${project.name} · ${repo} · Commit ${input.baseCommit||'not selected'}`):repo?`Workflow repository: ${repo}`:'Empty managed workspace — no repository selected'],
  ['Model',nodes.length&&nodes.every(n=>n.runtime?.kind==='echo')?'Not required for echo agents':modelState==='loading'?'Loading providers…':modelState==='unavailable'?'Provider discovery unavailable — review or retry configuration':modelState==='missing'?'No model provider ready — administrator configuration required':'Provider configured — the task review checks each agent’s model'],
  ['Environment',`${input.executionMode||'workflow'} · ${isolation||'select a workflow'} isolation · ${review?.canPrepareEnvironment?'Build required':review?'See reviewed requirements':'Preparation and compatibility require task review'}`],
  ['Delivery',input.scmConnection?`Pull request via ${input.scmConnection} — task review checks repository and approvals`:'No task SCM hand-off selected; only the workflow’s existing grants can allow delivery'],
  ['Access',review?.canStart?'Checked — Run task starts with the displayed permissions':'Ask by default — Run task checks the saved permissions'],
 ];
 return <section className="card task-readiness" aria-label="Task setup summary">
  <header><div><h2>Task setup summary</h2><p className="muted">Your task uses these settings. Run task saves changes and checks requirements before starting.</p></div></header>
  <dl>{rows.map(([name,value])=><div key={name}><dt>{name}</dt><dd>{value}</dd></div>)}</dl>
  <details><summary>Prepare this task for a specific goal</summary>
   <label>Setup goal<select value={goal} onChange={e=>setGoal(e.target.value)}><option value="work">Run work in a repository</option><option value="pr">Deliver a pull request</option><option value="issue">Work from an issue</option><option value="review">Review a pull request</option></select></label>
   {goal==='work'&&<p>Select a registered project, a task-compatible workflow and its input agent. Review the model, environment, permissions and budget before starting.</p>}
   {goal==='pr'&&<p>Choose a repository and source control hand-off. SCM connections provide server-held Git and PR delivery credentials; MCP tools are granted separately. A required PR approval remains a separate decision.</p>}
   {goal==='issue'&&<p>Select the issue service and reference in Input &amp; delivery. Jira REST access, MCP tools and SCM delivery serve different purposes; review the chosen connection and project scope. Automated issue polling is configured in workflow triggers.</p>}
   {goal==='review'&&<p>Use a review workflow and include the pull request identity and revision in its expected input. The reviewer needs explicit read/comment grants. Reading a PR does not authorize merging it.</p>}
   {admin?<div className="row"><Btn disabled={busy} onClick={()=>onRepair('#/models')}>Configure models and return</Btn><Btn disabled={busy} onClick={()=>onRepair('#/connections')}>Configure connections and return</Btn></div>:<p>An administrator must repair model, connection or environment configuration. You can save this draft for them.</p>}
   <p className="muted small">Opening configuration saves your draft first. Return here to review the saved task; no agents start.</p>
  </details>
 </section>;
}
