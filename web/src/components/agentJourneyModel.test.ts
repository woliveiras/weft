import {expect, it, vi} from 'vitest';
import {agentDefinitionSectionsChanged, mayDiscardAgentDraft, associationChanges} from './agentJourneyModel';
import type {Graph} from '../api/types';

it('guards dirty and busy drafts without asking for unchanged definitions',()=>{
  const confirm=vi.fn(()=>false);
  expect(mayDiscardAgentDraft({dirty:false,busy:false},confirm)).toBe(true);
  expect(mayDiscardAgentDraft({dirty:true,busy:true},confirm)).toBe(false);
  expect(confirm).not.toHaveBeenCalled();
  expect(mayDiscardAgentDraft({dirty:true,busy:false},confirm)).toBe(false);
  confirm.mockReturnValue(true);
  expect(mayDiscardAgentDraft({dirty:true,busy:false},confirm)).toBe(true);
});
it('reports shared name, parameters and workspace changes alongside grants',()=>{
  expect(agentDefinitionSectionsChanged({name:'Reviewer'}, {name:'Release reviewer',readOnly:true,parameters:[{name:'repository',required:true}],tools:[{name:'filesystem.read'}]}))
    .toEqual(['name','parameters','readOnly','tools']);
  expect(agentDefinitionSectionsChanged({name:'Reviewer'}, {name:'Reviewer'})).toEqual([]);
});
it('reviews exact changed association paths without copying instruction values',()=>{
  const before={metadata:{id:'g',version:3},spec:{nodes:[{id:'n'}]}} as Graph;
  const after={...before,spec:{...before.spec,instructions:{skillIds:['review']},nodes:[{id:'n',skillIds:[]}]}};
  expect(associationChanges(before,after)).toEqual(['spec.instructions','spec.nodes[0].skillIds']);
});
