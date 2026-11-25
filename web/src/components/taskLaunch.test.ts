import {describe,expect,it} from 'vitest';
import {taskInputIdentity,taskLaunchIdentity,type TaskLaunchReview} from './taskLaunch';
const fixture=():TaskLaunchReview=>({
 review:{task:{id:'tsk_a',revision:2,status:'draft',graphId:'flow',graphVersion:1,nodeId:'worker',request:'Inspect',title:'Inspect',outcome:'freeform',createdBy:'admin',updatedBy:'admin',createdAt:'',updatedAt:''},currentGraphVersion:1,canStart:true,blockers:[],validation:{valid:true,errors:[],warnings:[]},executionGraphId:'task-run-a',agents:[{id:'worker',model:{name:'model'},isolation:'docker',grants:[{name:'filesystem.read'}]}],grants:[],model:{name:'model'},isolation:'docker'},
 authorization:{scope:'reviewed'},budget:{currency:'USD',taskRunBudgetMicros:2_000_000,budgets:[{scope:'installation',id:'month',limitMicros:10_000_000,chargedMicros:0,estimatedMicros:0,reservedMicros:0,remainingMicros:10_000_000,reached:false,paused:false}]},
});
describe('task launch review identity',()=>{
 it('requires an exact reviewed scope and rejects stale saved consent without falling back to Ask',()=>{
  const value=fixture();expect(taskLaunchIdentity(value)).toBeTypeOf('string');
  value.authorization.scope='';expect(taskLaunchIdentity(value)).toBeUndefined();
  value.authorization={scope:'new',record:{scope:'old',mode:'full',taskId:'tsk_a',revision:1,actor:'admin',policyVersion:1}};
  expect(taskLaunchIdentity(value)).toBeUndefined();
 });
 it('changes for grants, mode, workflow, task revision, repository and environment changes',()=>{
  const original=taskLaunchIdentity(fixture());
  for(const change of [(v:TaskLaunchReview)=>{v.review.agents![0].grants.push({name:'git.write'});},(v:TaskLaunchReview)=>{v.authorization.record={scope:'reviewed',mode:'full',taskId:'tsk_a',revision:1,actor:'admin',policyVersion:1};},(v:TaskLaunchReview)=>{v.review.currentGraphVersion=2;},(v:TaskLaunchReview)=>{v.review.task.revision=3;},(v:TaskLaunchReview)=>{v.authorization.scope='different-repository';},(v:TaskLaunchReview)=>{v.review.environmentDefinition={image:'changed'};}]){
   const value=fixture();change(value);expect(taskLaunchIdentity(value)).not.toBe(original);
  }
 });
 it('compares spending limits and holds while ignoring concurrent charges',()=>{
  const value=fixture(),original=taskLaunchIdentity(value);
  value.budget.budgets[0].remainingMicros--;value.budget.budgets[0].chargedMicros++;
  expect(taskLaunchIdentity(value)).toBe(original);
  value.budget.budgets[0].limitMicros++;expect(taskLaunchIdentity(value)).not.toBe(original);
  const paused=fixture();paused.budget.budgets[0].reached=true;expect(taskLaunchIdentity(paused)).not.toBe(original);
 });
 it('requires another displayed review when the pinned workflow workspace changes',()=>{
  const value=fixture();value.workspace={repo:'https://git.example/first.git',git:true};
  const original=taskLaunchIdentity(value);
  value.workspace={repo:'https://git.example/second.git',git:true};expect(taskLaunchIdentity(value)).not.toBe(original);
  value.workspace={};expect(taskLaunchIdentity(value)).not.toBe(original);
 });
});

it('compares effective input, including source, payload and repository, independently of revisions',()=>{
 const original=fixture().review.task;
 expect(taskInputIdentity({...original,resultNodeId:original.nodeId,executionMode:'workflow',channelId:''})).toBe(taskInputIdentity(original));
 for(const update of [{request:'Changed request'},{baseCommit:'a'.repeat(40)},{payloadJson:'{"changed":true}'},{source:{kind:'jira' as const,connectionId:'jira',reference:'TEST-1'}}])expect(taskInputIdentity({...original,...update})).not.toBe(taskInputIdentity(original));
 const value=fixture();value.authorization.record={scope:'reviewed',mode:'full',taskId:'another-task',revision:1,actor:'admin',policyVersion:1};
 expect(taskLaunchIdentity(value)).toBeUndefined();
});
