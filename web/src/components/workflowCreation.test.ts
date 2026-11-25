import { describe, expect, it } from 'vitest';
import type { GraphNode } from '../api/types';
import { guidedWorkflow, guidedWorkflowIssues, newWorkflowAgent, proposeWorkflowConnections } from './workflowCreation';

const worker = (id: string, mode: 'once' | 'event' | 'continuous' = 'event'): GraphNode => ({id, name:id, runtime:{kind:'native'}, prompt:'Do the requested work.', loop:{mode}});

describe('guided workflow creation', () => {
  it('builds a reviewed task definition with explicit routing and no implicit grants', () => {
    const nodes = [worker('writer'), worker('reviewer')];
    const channels = proposeWorkflowConnections(nodes, 'task');
    const input = {id:'review-work', name:'Review work', description:'Implement and review', purpose:'task' as const, nodes, channels};
    expect(guidedWorkflowIssues(input)).toEqual([]);
    const graph = guidedWorkflow(input);
    expect(graph.spec).toMatchObject({task:{nodeId:'writer',resultNodeId:'reviewer',channelId:'requests',messageType:'work.request'}});
    expect(graph.spec.channels).toEqual([{id:'requests',type:'queue',to:['writer'],types:['work.request']},{id:'handoff-1',type:'queue',from:'writer',to:['reviewer']}]);
    expect(graph.spec.nodes).toEqual(nodes);
    expect(graph.spec.nodes.every(node => !node.tools?.length)).toBe(true);
    expect(input.nodes[0].loop?.mode).toBe('event');
  });

  it('creates a new native Docker agent with no permissions and preserves catalogue instance modes', () => {
    const agent = newWorkflowAgent('Worker', 'Do the work.', [], 'task');
    expect(agent).toMatchObject({id:'worker',runtime:{kind:'native'},isolation:{mode:'docker'},tools:[],loop:{mode:'event'}});
    expect(newWorkflowAgent('Worker', 'More work.', [agent], 'task').id).toBe('worker-2');
    expect(newWorkflowAgent('Loop', 'Repeat.', [], 'continuous').loop?.mode).toBe('continuous');
    const nodes = [worker('loop','continuous')];
    expect(guidedWorkflowIssues({id:'task',name:'Task',purpose:'task',nodes,channels:[]})).toContainEqual(expect.objectContaining({field:'nodes.0.mode'}));
    expect(nodes[0].loop?.mode).toBe('continuous');
    expect(guidedWorkflow({id:'events',name:'Events',purpose:'event',nodes,channels:[]}).spec.task).toBeUndefined();
  });

  it('identifies empty, duplicate, invalid reference and missing prompt configurations', () => {
    expect(guidedWorkflowIssues({id:'INVALID',name:'',purpose:'task',nodes:[],channels:[]}).map(issue=>issue.field)).toEqual(['id','name','nodes']);
    const input = {id:'work',name:'Work',purpose:'task' as const,nodes:[worker('same'),{...worker('same'),prompt:''}],channels:[{id:'inbox',type:'queue' as const,to:['missing']},{id:'inbox',type:'queue' as const,from:'missing',to:['same']}]};
    const issues = guidedWorkflowIssues(input);
    expect(issues).toContainEqual(expect.objectContaining({field:'nodes.1.id'}));
    expect(issues).toContainEqual(expect.objectContaining({field:'nodes.1.prompt'}));
    expect(issues).toContainEqual(expect.objectContaining({field:'channels.0.to'}));
    expect(issues).toContainEqual(expect.objectContaining({field:'channels.1.id'}));
    expect(issues).toContainEqual(expect.objectContaining({field:'channels.1.from'}));
  });
});
