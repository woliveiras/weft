import type {TaskProgress} from '../api/tasks';
export function taskStage(progress?:TaskProgress,unavailable=false) {
 if(unavailable)return {title:'Progress unavailable',next:'Refresh to check the current state. No action is inferred from a failed refresh.'};
 if(!progress)return {title:'Loading task',next:'Checking the latest recorded progress.'};
 if(progress.state==='cancel_requested')return {title:'Stopping work',next:'Waiting for agents to stop. Files and history are retained; external effects are not undone.'};
 if(progress.state==='cancelled')return {title:'Work stopped',next:'Review retained changes and history before starting a new task.'};
 if(progress.pendingApprovals?.length)return {title:'Needs your decision',next:'Review the requests below. These are the same decisions shown in Approvals.'};
 switch(progress.state){
 case 'succeeded':return {title:'Ready for review',next:'Inspect the result, code changes and test evidence below. Completion does not prove each tool succeeded.'};
 case 'failed':case 'blocked':case 'interrupted':return {title:'Work needs attention',next:'Inspect the recorded failure and configuration. A policy denial cannot be bypassed by approving the task.'};
 case 'preparing':case 'environment_pending':case 'environment_build_requested':return {title:'Preparing the environment',next:'Review environment progress below. Agents start only after requirements are satisfied.'};
 case 'starting':return {title:'Starting agents',next:'The workflow is being started with the reviewed permissions.'};
 case 'running':return {title:'Agents are working',next:'Follow execution progress below. You can review decisions or stop the task here.'};
 default:return {title:'Review task requirements',next:'Resolve the missing configuration before continuing this request.'};
 }
}
