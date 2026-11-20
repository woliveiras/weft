import {workflowAPI,workflowConnectionLink} from './workflowEdition';
import type {WorkflowConnection, WorkflowEdition, TriggerLifecycleMappings} from '../api/types';
import {useAsync} from '../hooks/useData';
import {Btn,ErrorBox} from './common';
import {HelpField} from './FieldHelp';

export function LifecycleMappingPicker({connection,value,disabled,onChange,edition='jira-dc'}:{
 connection:WorkflowConnection;edition?:WorkflowEdition;value?:TriggerLifecycleMappings;disabled?:boolean;
 onChange:(value:TriggerLifecycleMappings|undefined)=>void;
}){
 const mappings=useAsync(()=>workflowAPI(edition).mappings(connection.name),[connection.name,connection.revision,edition]);
 const eligible=mappings.data?.filter(m=>m.enabled&&m.connection===connection.name&&m.connectionRevision===connection.revision&&m.siteUrl===connection.siteUrl&&m.projectId===connection.projectId)??[];
 return <fieldset disabled={disabled} className="form" aria-label="Optional Jira lifecycle mappings">
 <legend>Optional status synchronization</legend>
 <p className="muted small">Select explicit mapping revisions. Without a mapping, Weft updates comments and labels. Jira synchronization has its own outcome and does not change task or PR success.</p>
 {disabled&&<p role="note">Let the admitted task finish before changing its mappings.</p>}
 <ErrorBox error={mappings.error}/>{mappings.loading&&<p role="status">Loading workflow mappings…</p>}
 {mappings.error&&<Btn onClick={()=>void mappings.reload()}>Retry loading mappings</Btn>}
 {(['workStarted','prDelivered'] as const).map(stage=>{
  const pin=value?.[stage];const selected=pin?`${pin.name}@${pin.revision}`:'';
  return <HelpField key={stage} label={stage==='workStarted'?'When runtime starts':'When a pull request is delivered'} help={stage==='workStarted'?'Runs after durable runtime admission, following human approval and environment preparation. Task creation and waiting for approval do not count. Select a definition for the issue type and source status.':'Uses the persisted pull-request delivery event. It does not mean the pull request was merged. Each issue is validated against the pinned transition IDs before writing.'}>
  <select disabled={mappings.loading||!!mappings.error} value={selected} onChange={e=>{
   const found=eligible.find(m=>`${m.name}@${m.revision}`===e.target.value);
   const next={...value,[stage]:found?{name:found.name,revision:found.revision}:undefined};
   onChange(next.workStarted||next.prDelivered?next:undefined);
  }}><option value="">No automatic transition</option>
  {selected&&!eligible.some(m=>`${m.name}@${m.revision}`===selected)&&<option value={selected} disabled>{pin!.name} · Revision {pin!.revision} (review required)</option>}
  {eligible.map(m=><option key={m.name} value={`${m.name}@${m.revision}`}>{m.name} · Revision {m.revision} · Type {m.issueTypeId} · {m.fromStatusId} → {m.targetStatusId}</option>)}
  </select></HelpField>;
 })}
 <a href={`${workflowConnectionLink(edition,connection.name)}/mappings`}>Manage mappings and synchronization</a>
 </fieldset>;
}
