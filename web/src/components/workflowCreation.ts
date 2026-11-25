import type { Channel, Graph, GraphNode } from '../api/types';

export type WorkflowPurpose = 'task' | 'event' | 'continuous';
export interface GuidedWorkflowInput {
  id: string;
  name: string;
  description?: string;
  purpose: WorkflowPurpose;
  nodes: GraphNode[];
  channels: Channel[];
}
export interface WorkflowCreationIssue { field: string; message: string }
const dnsLabel = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/;

export function workflowSlug(name: string) {
  return name.toLowerCase().replace(/[^a-z0-9]+/g,'-').replace(/^-+|-+$/g,'').slice(0,63).replace(/-+$/,'');
}

/** New instances receive no tools or credentials; shared definitions remain explicit selections. */
export function newWorkflowAgent(name: string, prompt: string, nodes: GraphNode[], purpose: WorkflowPurpose): GraphNode {
  const base = workflowSlug(name) || 'agent';
  let id = base, suffix = 2;
  while (nodes.some(node=>node.id===id)) {
    const tail = '-' + suffix++;
    id = base.slice(0,63-tail.length) + tail;
  }
  return {id,name:name.trim(),runtime:{kind:'native'},prompt:prompt.trim(),tools:[],isolation:{mode:'docker'},
    loop:{mode:purpose==='continuous' && !nodes.length?'continuous':'event'},position:{x:280*nodes.length,y:0}};
}

/** Proposals are returned for review; callers choose whether to replace their current connections. */
export function proposeWorkflowConnections(nodes: GraphNode[], purpose: WorkflowPurpose): Channel[] {
  if (!nodes.length) return [];
  return [
    ...(purpose==='continuous'?[]:[{id:'requests',type:'queue' as const,to:[nodes[0].id],types:['work.request']}]),
    ...nodes.slice(1).map((node,index)=>({id:`handoff-${index+1}`,type:'queue' as const,from:nodes[index].id,to:[node.id]})),
  ];
}

export function guidedWorkflowIssues(input: GuidedWorkflowInput): WorkflowCreationIssue[] {
  const issues: WorkflowCreationIssue[] = [];
  const add = (field:string,message:string)=>issues.push({field,message});
  if (!dnsLabel.test(input.id)) add('id','Use a workflow ID of 1–63 lowercase letters, digits or hyphens, starting and ending with a letter or digit.');
  if (!input.name.trim()) add('name','Enter a workflow name.');
  if (!input.nodes.some(node=>!node.disabled)) add('nodes','Add at least one enabled agent.');
  const nodeIDs = new Set<string>();
  input.nodes.forEach((node,index)=>{
    if (!dnsLabel.test(node.id) || nodeIDs.has(node.id)) add(`nodes.${index}.id`,'Agent IDs must be unique DNS labels.');
    nodeIDs.add(node.id);
    if (!node.disabled && (node.runtime?.kind ?? 'native')==='native' && !node.prompt?.trim()) add(`nodes.${index}.prompt`,'Enter instructions for this native agent.');
    if (input.purpose==='task' && !node.disabled && !['once','event'].includes(node.loop?.mode ?? 'once')) add(`nodes.${index}.mode`,'Task workflows support once or event instances. Select a compatible run mode.');
  });
  const channelIDs = new Set<string>();
  input.channels.forEach((channel,index)=>{
    if (!dnsLabel.test(channel.id) || channelIDs.has(channel.id)) add(`channels.${index}.id`,'Connection IDs must be unique DNS labels.');
    channelIDs.add(channel.id);
    if (channel.from && !nodeIDs.has(channel.from)) add(`channels.${index}.from`,'Choose an existing source agent or external input.');
    if (!channel.to?.length || channel.to.some(id=>!nodeIDs.has(id))) add(`channels.${index}.to`,'Choose an existing receiving agent.');
  });
  return issues;
}

export function guidedWorkflow(input: GuidedWorkflowInput): Graph {
  const enabled = input.nodes.filter(node=>!node.disabled);
  const first = enabled[0];
  const ingress = input.channels.find(channel=>!channel.from && !channel.when && (channel.type??'queue')==='queue' && channel.to?.length===1 && channel.to[0]===first?.id);
  return {apiVersion:'weft.io/v1',kind:'Graph',metadata:{id:input.id,name:input.name.trim(),description:input.description?.trim() || undefined},spec:{
    nodes:input.nodes,channels:input.channels,
    ...(input.purpose==='task' && first ? {task:{nodeId:first.id,resultNodeId:enabled.at(-1)!.id,
      ...(ingress?{channelId:ingress.id,messageType:ingress.types?.[0] || 'work.request'}:{})}}:{}),
  }};
}
