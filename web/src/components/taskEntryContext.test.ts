import {expect,it} from 'vitest';
import {applyTaskEntryContext,taskCreateLink,taskReturnLink,taskRepairLink} from './taskEntryContext';
import type {TaskInput,Project} from '../api/tasks';
import type {Graph} from '../api/types';
const input:TaskInput={title:'Work',request:'Inspect',outcome:'freeform',graphId:'',graphVersion:0,nodeId:''};
const graph={metadata:{id:'flow',version:3},spec:{nodes:[{id:'worker'}]}} as Graph;
const project={id:'repo',head:'a'.repeat(40),executionMode:'sandbox'} as Project;

it('resolves explicit project and workflow entry into concrete version and commit suggestions',()=>{
 const result=applyTaskEntryContext(input,{workflow:'flow',project:'repo'},[graph],[project]);
 expect(result).toMatchObject({graphId:'flow',graphVersion:3,nodeId:'worker',projectId:'repo',baseCommit:project.head,executionMode:'sandbox'});
 expect(input.graphVersion).toBe(0);
});
it('does not replace a manual selection or guess a resource for an unknown link',()=>{
 expect(applyTaskEntryContext({...input,graphId:'manual',graphVersion:2,nodeId:'mine'}, {workflow:'flow'},[graph],[])).toMatchObject({graphId:'manual',graphVersion:2,nodeId:'mine'});
 expect(applyTaskEntryContext(input,{workflow:'missing',project:'missing'},[graph],[project])).toMatchObject({graphId:'missing',graphVersion:0,projectId:'missing'});
});
it('retains explicit project context ahead of a workflow preference',()=>{
 const preferred={...graph,spec:{...graph.spec,task:{projectId:'other'}}};
 expect(applyTaskEntryContext(input,{workflow:'flow',project:'repo'},[preferred],[project])).toMatchObject({projectId:'repo',baseCommit:project.head});
});
it('preserves explicit environment and delivery choices when context arrives',()=>{
 const preferred={...graph,spec:{...graph.spec,task:{scmConnection:'workflow'}}};
 expect(applyTaskEntryContext({...input,executionMode:'workflow',scmConnection:'mine'},{workflow:'flow',project:'repo'},[preferred],[project]))
  .toMatchObject({executionMode:'workflow',scmConnection:'mine',projectId:'repo'});
});
it('encodes entry context and permits return navigation only to a saved task',()=>{
 expect(taskCreateLink({project:'repo & name',workflow:'flow'})).toBe('#/tasks/new?project=repo+%26+name&workflow=flow');
 expect(taskReturnLink(taskRepairLink('#/models','tsk_test'))).toBe('#/tasks/tsk_test');
 expect(taskReturnLink('#/models?returnTo=https%3A%2F%2Fevil.test')).toBeUndefined();
 expect(taskReturnLink('#/models?returnTo=%23%2Ftasks%2Fnew')).toBeUndefined();
 expect(taskReturnLink('#/models?returnTo=%23%2Ftasks%2Ftsk_test%3Fexecute%3Dtrue')).toBeUndefined();
});
