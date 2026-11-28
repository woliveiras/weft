import {useEffect, useState} from 'react';
import {api} from '../api/client';
import {profileLabels, type AuthorizationProfile} from '../api/tasks';
import type {Project} from '../api/tasks';
import type {Graph, SCMConnection, WorkflowTaskDefaults as Defaults} from '../api/types';
import {Btn, ErrorBox} from './common';
import {HelpField} from './FieldHelp';
import {RepositoryPicker} from './RepositoryPicker';
import {taskCreateLink} from './taskEntryContext';

/** Suggestions join the complete workflow draft; this form never saves or starts it. */
export function WorkflowTaskDefaults({graph,onChange,editable,dirty=false}:{dirty?:boolean;graph:Graph;onChange:(update:(graph:Graph)=>Graph)=>void;editable:boolean}) {
 const value=graph.spec.task??{};
 const nodes=graph.spec.nodes.filter(node=>!node.disabled);
 const inputs=(graph.spec.channels??[]).filter(channel=>!channel.from&&!channel.when&&(!channel.type||channel.type==='queue')&&channel.to?.length===1&&nodes.some(node=>node.id===channel.to?.[0]));
 const [projects,setProjects]=useState<Project[]>();
 const [connections,setConnections]=useState<SCMConnection[]>();
 const [projectError,setProjectError]=useState('');
 const [connectionError,setConnectionError]=useState('');
 const [attempt,setAttempt]=useState(0);
 useEffect(()=>{
  let active=true;setProjects(undefined);setConnections(undefined);setProjectError('');setConnectionError('');
  void api.projects().then(result=>{if(active)setProjects(result);}).catch(error=>{if(active)setProjectError(String(error));});
  void api.scmConnections().then(result=>{if(active)setConnections(result);}).catch(error=>{if(active)setConnectionError(String(error));});
  return()=>{active=false;};
 },[attempt]);
 const change=(patch:Partial<Defaults>)=>onChange(current=>{
  const task={...current.spec.task,...patch};
  return {...current,spec:{...current.spec,task:Object.values(task).some(Boolean)?task:undefined}};
 });
 const missingNode=(id:string|undefined)=>id&&!nodes.some(node=>node.id===id)?<option value={id}>{id} (unavailable)</option>:null;
 const channel=inputs.find(input=>input.id===value.channelId);
 return <fieldset aria-label="Workflow task defaults">
  <p className="muted small">Configure recurring task choices once. New tasks suggest these values and still require their own saved version, repository base, requirements review and authorization.</p>
  <HelpField label="Default task authorization" help="Tasks inherit this approval profile and the grants on each workflow agent. A task administrator can override the profile before starting. Mandatory approvals still apply.">
   <select disabled={!editable} value={value.authorizationProfile??''} onChange={event=>change({authorizationProfile:event.target.value as AuthorizationProfile || undefined})}>
    <option value="">Ask for approval (default)</option>
    {Object.entries(profileLabels).map(([key,label])=><option key={key} value={key}>{label}</option>)}
   </select>
  </HelpField>
  <HelpField label="Default entry agent" help="The agent that receives new task requests. Leave unset to infer an entry only when topology is unambiguous. Choosing an agent directly clears a channel default.">
   <select disabled={!editable} value={value.nodeId??''} onChange={event=>change({nodeId:event.target.value||undefined,channelId:undefined,messageType:undefined})}>
    <option value="">Infer when unambiguous</option>{missingNode(value.nodeId)}{nodes.map(node=><option key={node.id} value={node.id}>{node.name||node.id}</option>)}
   </select>
  </HelpField>
  <HelpField label="Default result agent" help="The agent whose final result the task tracks. Leave unset to use unambiguous terminal inference or let the task choose.">
   <select disabled={!editable} value={value.resultNodeId??''} onChange={event=>change({resultNodeId:event.target.value||undefined})}>
    <option value="">Infer when unambiguous</option>{missingNode(value.resultNodeId)}{nodes.map(node=><option key={node.id} value={node.id}>{node.name||node.id}</option>)}
   </select>
  </HelpField>
  <HelpField label="Default input channel" help="Only unconditional queue ingress with one enabled receiver is eligible. Selecting it sets the entry agent; its concrete message type must match the channel's accepted types.">
   <select disabled={!editable} value={value.channelId??''} onChange={event=>{
    const selected=inputs.find(input=>input.id===event.target.value);
    change({channelId:selected?.id,nodeId:selected?.to?.[0]??value.nodeId,messageType:selected?.types?.length===1&&!/[?*]/.test(selected.types[0])?selected.types[0]:undefined});
   }}>
    <option value="">Direct entry or inferred ingress</option>{value.channelId&&!channel&&<option value={value.channelId}>{value.channelId} (unavailable)</option>}{inputs.map(input=><option key={input.id} value={input.id}>{input.id} → {input.to?.[0]}</option>)}
   </select>
  </HelpField>
  {value.channelId&&<HelpField label="Default message type" help="A concrete type for requests sent to this ingress, for example work.request. New tasks generate an editable title/description payload; no arbitrary payload is stored here.">
   <input disabled={!editable} value={value.messageType??''} maxLength={128} required onChange={event=>change({messageType:event.target.value||undefined})}/>
  </HelpField>}
  <RepositoryPicker label="Suggested repositories" help="These repositories are preselected in new tasks using this workflow version. Leave the field empty for no preference."
    projects={projects ?? []} ids={value.projectIds ?? (value.projectId ? [value.projectId] : [])}
    disabled={!editable || !projects} loading={!projects && !projectError} onChange={ids => {
      if (!value.projectIds && ids.length <= 1) change({projectId: ids[0]});
      else change({projectIds: ids, projectId: undefined, scmConnection: undefined});
    }}/>
  <button type="button" className="btn" disabled={dirty||!graph.metadata.version} onClick={()=>{location.hash=taskCreateLink({workflow:graph.metadata.id});}}>New task with these defaults</button>
  {!projects&&!projectError&&<p role="status">Loading repositories…</p>}<ErrorBox error={projectError&&`Could not load repositories. ${projectError}`}/>
  {!value.projectIds?.length&&<HelpField label="Default source control connection" help="Suggests the connection for PR delivery. Selecting it does not grant push/PR tools or waive approval. Admission rechecks the connection, repository and result agent's permissions.">
   <select disabled={!editable} value={value.scmConnection??''} onChange={event=>change({projectIds:undefined,scmConnection:event.target.value||undefined})}>
    <option value="">No delivery preference</option>{value.scmConnection&&!connections?.some(connection=>connection.name===value.scmConnection)&&<option value={value.scmConnection}>{value.scmConnection} (not available in loaded connections)</option>}{connections?.map(connection=><option key={connection.name} value={connection.name}>{connection.name} · {connection.repository}</option>)}
   </select>
  </HelpField>
  }
  {!connections&&!connectionError&&<p role="status">Loading source control connections…</p>}<ErrorBox error={connectionError&&`Could not load source control connections. ${connectionError}`}/>
  {(projectError||connectionError)&&<Btn small onClick={()=>setAttempt(attempt+1)}>Retry resource discovery</Btn>}
  {!editable&&<p className="muted small">An administrator can change these workflow defaults.</p>}
 </fieldset>;
}
