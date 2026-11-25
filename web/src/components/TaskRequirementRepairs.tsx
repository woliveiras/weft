import type {TaskReview} from '../api/tasks';
import {taskRepairLink} from './taskEntryContext';

export function requiredConnections(agents:NonNullable<TaskReview['agents']>) {
  return [...new Set(agents.flatMap(agent=>(agent.grants??[]).flatMap(grant=>{
    const match=/^mcp\.([a-z][a-z0-9_-]{0,63})\.[a-zA-Z0-9_-]{1,128}$/.exec(grant.name);
    return match?[match[1]]:[];
  })))].sort();
}

export function TaskRequirementRepairs({agents,taskId,canConfigure=true}:{agents:NonNullable<TaskReview['agents']>;taskId?:string;canConfigure?:boolean}) {
  const connections=requiredConnections(agents);
  const href=(destination:string)=>taskId?taskRepairLink(destination,taskId):destination;
  return <section aria-label="Repair task requirements"><h3>Configuration and repair</h3>
    <p>After repairing configuration, return to this saved task and review its requirements again. No permissions are granted automatically.</p>
    {canConfigure?<><a href={href('#/models')}>Configure model providers</a>
    {!!connections.length&&<ul>{connections.map(id=><li key={id}><a href={href(`#/connections/c/${encodeURIComponent(id)}/overview`)}>Review connection {id}</a></li>)}</ul>}</>:<p>An administrator must repair model or connection configuration.</p>}
  </section>;
}
