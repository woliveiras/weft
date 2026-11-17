import type {TaskReview} from '../api/tasks';

export function requiredConnections(agents:NonNullable<TaskReview['agents']>) {
  return [...new Set(agents.flatMap(agent=>(agent.grants??[]).flatMap(grant=>{
    const match=/^mcp\.([a-z][a-z0-9_-]{0,63})\.[a-zA-Z0-9_-]{1,128}$/.exec(grant.name);
    return match?[match[1]]:[];
  })))].sort();
}

export function TaskRequirementRepairs({agents}:{agents:NonNullable<TaskReview['agents']>}) {
  const connections=requiredConnections(agents);
  return <section aria-label="Repair task requirements"><h3>Configuration and repair</h3>
    <p>Open configuration in another tab to keep this saved request here. After repairing access or configuration, return and select Review requirements again. No permissions are granted automatically.</p>
    <a href="#/models" target="_blank" rel="noopener noreferrer">Configure model providers in another tab</a>
    {!!connections.length&&<ul>{connections.map(id=><li key={id}><a href={`#/connections/c/${encodeURIComponent(id)}/overview`} target="_blank" rel="noopener noreferrer">Review connection {id} in another tab</a></li>)}</ul>}
  </section>;
}
