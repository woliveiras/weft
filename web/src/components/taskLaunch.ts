import type {TaskAuthorization,TaskInput,TaskReview} from '../api/tasks';
import type {BudgetView,Graph} from '../api/types';

export interface TaskLaunchReview {
 review:TaskReview;
 authorization:TaskAuthorization;
 budget:{taskRunBudgetMicros:number;currency:string;budgets:BudgetView[]};
 workspace?:Graph['spec']['workspace'];
}
/** Compare effective editable input independently from server revision and authorization hashes. */
export function taskInputIdentity(input:TaskInput):string {
 return JSON.stringify({
  title:input.title,request:input.request,outcome:input.outcome,graphId:input.graphId,graphVersion:input.graphVersion,
  nodeId:input.nodeId,resultNodeId:input.resultNodeId||input.nodeId,projectId:input.projectId||'',baseCommit:input.baseCommit||'',
  executionMode:input.executionMode||'workflow',scmConnection:input.scmConnection||'',channelId:input.channelId||'',
  messageType:input.messageType||'',payloadJson:input.payloadJson||'',
  source:input.source?{kind:input.source.kind,connectionId:input.source.connectionId,reference:input.source.reference,cloudId:input.source.cloudId||''}:undefined,
 });
}
/** Compare only acknowledged, visible facts; volatile inspection timestamps do not grant consent. */
export function taskLaunchIdentity(value:TaskLaunchReview):string|undefined {
 const {review,authorization,budget}=value;
 if(budget.budgets.some(item=>item.reached||item.paused||item.invalid)||!authorization.scope||!review.task.id||!review.task.revision||authorization.record&&(authorization.record.scope!==authorization.scope||authorization.record.taskId!==review.task.id))return undefined;
 return JSON.stringify({
  task:review.task.id,revision:review.task.revision,input:taskInputIdentity(review.task),scope:authorization.scope,
  mode:authorization.record?.mode??'ask',recordScope:authorization.record?.scope,
  workflow:review.currentGraphVersion,environment:review.environmentDefinition,
  project:review.project?.id,workspace:review.workspacePath,workflowWorkspace:value.workspace,agents:review.agents,
  grants:review.grants,model:review.model,isolation:review.isolation,
  budget:{taskRunBudgetMicros:budget.taskRunBudgetMicros,currency:budget.currency,budgets:budget.budgets.map(item=>({scope:item.scope,id:item.id,currency:item.currency,limitMicros:item.limitMicros,reached:item.reached,paused:item.paused,invalid:item.invalid}))},
 });
}
