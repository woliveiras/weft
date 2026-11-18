import { expect, it } from 'vitest';
import type { Graph } from '../api/types';
import { agentBoundaries, agentDefinitionChanged, agentSection, changedGraphFields } from './agentSettingsModel';
const graph = {spec:{defaults:{tools:[{name:'filesystem.read',paths:['src/**']}],isolation:{mode:'docker',network:'bridge'}},nodes:[],channels:[]}} as unknown as Graph;
it('resolves inherited tools and isolation without broadening explicit overrides',()=>{
 expect(agentBoundaries(graph,{id:'a'})).toMatchObject({isolation:'docker',readOnly:false,tools:graph.spec.defaults?.tools});
 expect(agentBoundaries(graph,{id:'a',tools:[],isolation:{mode:'process'},workspace:{readOnly:true}})).toMatchObject({isolation:'process',readOnly:true,tools:[]});
 expect(agentBoundaries({...graph,spec:{nodes:[]}}, {id:'a'})).toMatchObject({isolation:'process',tools:[],loop:'once'});
});

it('detects edits across agent sections and treats cleared optional fields like the saved definition', () => {
  const saved = { name: 'Reviewer', parameters: [{ name: 'repository' }] };
  expect(agentDefinitionChanged(saved, { ...saved, name: 'Release reviewer' })).toBe(true);
  expect(agentDefinitionChanged(saved, { ...saved, readOnly: true })).toBe(true);
  expect(agentDefinitionChanged(saved, { ...saved, tools: ['filesystem.read'] })).toBe(true);
  expect(agentDefinitionChanged(saved, {
    ...saved, description: '', prompt: '', readOnly: false, tools: [],
    parameters: [{ name: 'repository', description: '', default: '', required: false }],
  })).toBe(false);
});
it('handles unknown sections and identifies every changed graph path without exposing values',()=>{
 expect(agentSection('tools')).toBe('tools');expect(agentSection('invalid')).toBe('general');
 const before={spec:{nodes:[{id:'a',prompt:'old'},{id:'b',tools:[]}],channels:[],parameters:[{name:'token',value:'synthetic-old'}]}};
 const after={spec:{nodes:[{id:'a',prompt:'new'},{id:'b',tools:[{name:'shell.exec'}]}],channels:[{id:'queue'}],parameters:[{name:'token',value:'synthetic-new'}]}};
 expect(changedGraphFields(before,after)).toEqual(['spec.channels[0]','spec.nodes[0].prompt','spec.nodes[1].tools[0]','spec.parameters[0].value']);
 expect(changedGraphFields(before,structuredClone(before))).toEqual([]);
 expect(changedGraphFields({a:1,b:2},{b:2,a:1})).toEqual([]);
 expect(changedGraphFields({x:true},{})).toEqual(['x']);
 expect(JSON.stringify(changedGraphFields(before,after))).not.toContain('synthetic');
});
