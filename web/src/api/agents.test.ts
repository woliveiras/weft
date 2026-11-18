import { describe, expect, it } from 'vitest';
import { nodeWithAgentVersion, type Agent } from './agents';
import type { GraphNode } from './types';

const agent: Agent = {
  id: 'reviewer',
  versions: [
    {version:1, createdAt:'', createdBy:'', definition:{name:'Reviewer'}},
    {version:2, createdAt:'', createdBy:'', definition:{name:'Release reviewer', prompt:'Review @repository.', parameters:[
      {name:'repository'}, {name:'branch', default:'main'}, {name:'team', required:true},
    ]}},
  ],
};
const node: GraphNode = {
  id:'review', localInstructions:'Check tests.', loop:{mode:'event'},
  agent:{id:agent.id, version:1, parameters:{repository:'other/project', retired:'old'}},
};

describe('nodeWithAgentVersion', () => {
  it('keeps compatible overrides and local instructions when applying a new definition', () => {
    expect(nodeWithAgentVersion(node, agent, 2)).toMatchObject({
      id:'review', name:'Release reviewer', prompt:'Review @repository.', localInstructions:'Check tests.', loop:{mode:'event'},
      agent:{id:'reviewer',version:2},
    });
    expect(nodeWithAgentVersion(node, agent, 2).agent?.parameters).toEqual({repository:'other/project'});
    expect(node.agent?.parameters).toEqual({repository:'other/project', retired:'old'});
  });

  it('leaves the draft unchanged for the current or an unavailable version, or a different agent', () => {
    expect(nodeWithAgentVersion(node, agent, 1)).toBe(node);
    expect(nodeWithAgentVersion(node, agent, 99)).toBe(node);
    expect(nodeWithAgentVersion(node, {...agent,id:'other'}, 2)).toBe(node);
    const unbound = {...node, agent:undefined};
    expect(nodeWithAgentVersion(unbound, agent, 2)).toBe(unbound);
  });
});
