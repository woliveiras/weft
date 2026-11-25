import type {TaskDraft,TaskProgress} from '../api/tasks';
import {hasExpiredTaskApproval,taskStage} from './taskStage';
export function TaskJourney({progress,error,task}:{progress?:TaskProgress;error?:string;task?:TaskDraft}) {
 const stage=taskStage(progress,!!error,task??progress?.task);
 const executions=error?[]:progress?.executions??[];
 const decisions=error?[]:progress?.decisions??[];
 return <section aria-label="Task journey"><h3>{stage.title}</h3><p>{stage.next}</p>
 {!!executions.length&&<p>{executions.filter(x=>x.status==='succeeded').length} completed executions · {executions.filter(x=>x.status==='running').length} running · {executions.filter(x=>x.status==='failed'||x.status==='interrupted').length} need attention. Child executions are shown separately; this is not a token total.</p>}
 {!error&&!hasExpiredTaskApproval(progress)&&['failed','blocked','interrupted'].includes(progress?.state??'')&&<nav aria-label="Resolve task requirements"><a href="#/connections">Review connections</a> · <a href="#/models">Review models</a> · <a href="#/administration/network">Review installation policies</a></nav>}
 {decisions.some(a=>a.status!=='pending')&&<details><summary>Decision history</summary><ul>{decisions.filter(a=>a.status!=='pending').map(a=><li key={a.id}>{a.title} — {a.status} <a href="#/approvals">Approvals inbox</a></li>)}</ul></details>}
 </section>;
}
