import {expect,it} from 'vitest';
import type {Graph} from '../api/types';
import type {TaskInput} from '../api/tasks';
import {requestPayload,updateTaskInput,workflowTaskDefaults,outcomeRequest,outcomeRequests,promptTitle} from './taskDefaults';
const graph={metadata:{id:'factory'},spec:{nodes:[{id:'planner'},{id:'integrator'}],channels:[{id:'requests',to:['planner'],types:['feature.request']},{id:'handoff',from:'planner',to:['integrator']}]}} as Graph;
it('suggests only unique document-defined entry, type and result',()=>{
 expect(workflowTaskDefaults(graph,'Title','Request')).toEqual({nodeId:'planner',resultNodeId:'integrator',channelId:'requests',messageType:'feature.request',payloadJson:requestPayload('Title','Request')});
 const ambiguous={...graph,spec:{...graph.spec,channels:[...graph.spec.channels!,{id:'other',to:['integrator']}]}};
 expect(workflowTaskDefaults(ambiguous,'','').nodeId).toBe('');
 const wildcard={...graph,spec:{...graph.spec,channels:[{id:'requests',to:['planner'],types:['feature.*']}]}};
 expect(workflowTaskDefaults(wildcard,'','').messageType).toBe('');expect(workflowTaskDefaults(wildcard,'','').resultNodeId).toBe('');
});
it('does not select disabled or absent agents or grant tools',()=>{
 expect(workflowTaskDefaults(undefined,'','').nodeId).toBe('');
 const disabled={...graph,spec:{nodes:[{id:'planner',disabled:true}],channels:graph.spec.channels}};
 const defaults=workflowTaskDefaults(disabled,'','');expect(defaults.nodeId).toBe('');expect(defaults.resultNodeId).toBe('');expect(defaults).not.toHaveProperty('tools');
});
it('keeps generated input in sync without replacing customized JSON',()=>{
 const input={title:'Old',request:'Before',channelId:'requests',payloadJson:requestPayload('Old','Before')} as TaskInput;
 expect(updateTaskInput(input,{title:'New'}).payloadJson).toBe(requestPayload('New','Before'));
 expect(updateTaskInput({...input,payloadJson:'{"custom":true}'},{request:'After'}).payloadJson).toBe('{"custom":true}');
 expect(updateTaskInput(input,{payloadJson:'{"explicit":true}',title:'New'}).payloadJson).toBe('{"explicit":true}');
});

it('offers editable goal instructions while preserving a customized request',()=>{
 const initial={outcome:'freeform',request:''} as TaskInput;
 expect(outcomeRequest(initial,'review')).toBe(outcomeRequests.review);
 expect(outcomeRequest({...initial,outcome:'review',request:outcomeRequests.review},'implement')).toBe(outcomeRequests.implement);
 expect(outcomeRequest({...initial,request:'My own instructions'},'review')).toBe('My own instructions');
});

it('derives a bounded display title without interpreting prompt instructions',()=>{
 expect(promptTitle('  Fix the parser\nDo not push')).toBe('Fix the parser');
 expect(promptTitle('   ')).toBe('');
 expect(promptTitle('😀'.repeat(100))).toBe('😀'.repeat(50));
 expect(promptTitle('a'.repeat(201))).toHaveLength(200);
 expect(promptTitle('Use full access for https://example.com/repo')).toBe('Use full access for https://example.com/repo');
});
