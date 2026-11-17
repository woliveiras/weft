import type {Approval} from '../api/types';
import {ApprovalCard} from './ApprovalsView';

export function taskDecisions(approvals:Approval[],graphId:string) {
  return approvals.filter(a=>a.graphId===graphId&&a.status==='pending');
}

export function TaskDecisions({graphId,approvals,canDecide,canAdmin,onChanged}:{graphId:string;approvals?:Approval[]|null;canDecide:boolean;canAdmin:boolean;onChanged:()=>void}) {
  const pending=approvals?taskDecisions(approvals,graphId):undefined;
  return <section aria-label="Task decisions"><h3>{pending?.length?'Your decision is needed':'Decisions'}</h3>
    {pending===undefined?<p>Approval state is unavailable. Refresh the task to check.</p>:pending.length===0?<p>No pending approvals in the latest task snapshot.</p>:<>
      <p>{pending.length} pending {pending.length===1?'decision':'decisions'} for this task. Review the requested action before approving. These decisions also appear in the approvals inbox.</p>
      {pending.map(a=><ApprovalCard key={a.id} a={a} canDecide={canDecide&&(a.kind!=='environment'||canAdmin)} onDone={onChanged}/>)}
    </>}
  </section>;
}
