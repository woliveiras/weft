import type {TaskInput,Project} from '../api/tasks';
import type {Graph} from '../api/types';
import {workflowTaskDefaults} from './taskDefaults';

export interface TaskEntryContext {project?:string;workflow?:string}
export function taskCreateLink(context:TaskEntryContext={}):string {
 const query=new URLSearchParams();
 if(context.project)query.set('project',context.project);
 if(context.workflow)query.set('workflow',context.workflow);
 return `#/tasks/new${query.size?`?${query}`:''}`;
}
/** Only new-task entry invokes this; asynchronous loading must not replace explicit edits. */
export function applyTaskEntryContext(input:TaskInput,context:TaskEntryContext,graphs:Graph[],projects:Project[]):TaskInput {
 let next=input;
 if(!input.graphId&&context.workflow){
  const graph=graphs.find(g=>g.metadata.id===context.workflow);
  next={...next,...workflowTaskDefaults(graph,input.title,input.request),...(input.scmConnection?{scmConnection:input.scmConnection}:{}),graphId:context.workflow,graphVersion:graph?.metadata.version??0};
 }
 const id=input.projectId||context.project||next.projectId;
 if(id&&!input.projectId){
  const project=projects.find(p=>p.id===id);
  next={...next,projectId:id,baseCommit:project?.head,executionMode:input.executionMode??project?.executionMode};
 }
 return next;
}
export function taskRepairLink(destination:string,taskId:string):string {
 return `${destination}${destination.includes('?')?'&':'?'}returnTo=${encodeURIComponent(`#/tasks/${encodeURIComponent(taskId)}`)}`;
}
/** Narrow local return destination, never an external redirect or an execution command. */
export function taskReturnLink(hash:string):string|undefined {
 const value=new URLSearchParams(hash.split('?')[1]).get('returnTo')??'';
 return /^#\/tasks\/tsk_[a-zA-Z0-9_-]+$/.test(value)?value:undefined;
}
