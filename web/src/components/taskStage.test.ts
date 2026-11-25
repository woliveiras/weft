import {describe,it,expect} from 'vitest';
import {taskStage} from './taskStage';
import type {TaskProgress} from '../api/tasks';
describe('task stages',()=>{
 it.each([['running','Agents are working'],['succeeded','Ready for review'],['cancel_requested','Stopping work'],['cancelled','Work stopped'],['failed','Work needs attention'],['blocked','Work needs attention'],['environment_pending','Preparing the environment']])('%s explains the next step',(state,title)=>{expect(taskStage({state} as TaskProgress).title).toBe(title);});
 it('does not claim progress from stale data',()=>{expect(taskStage({state:'succeeded'} as TaskProgress,true).title).toBe('Progress unavailable');expect(taskStage().title).toBe('Loading task');});
 it('prioritizes a durable pending decision',()=>{expect(taskStage({state:'running',pendingApprovals:[{id:'same-approval'}]} as TaskProgress).title).toBe('Needs your decision');});
});
it('uses recorded expired approval status without diagnosing expiry from arbitrary text',()=>{
 const expired={state:'failed',decisions:[{id:'a',status:'expired'}]} as TaskProgress;
 expect(taskStage(expired).title).toBe('An approval expired');
 expect(taskStage(expired).next).toContain('cannot be approved retroactively');
 expect(taskStage(expired).next).toContain('Other recorded failures may also need attention');
 expect(taskStage({...expired,state:'running'}).title).toBe('Agents are working');
 expect(taskStage({...expired,decisions:[{id:'a',status:'rejected',title:'expired'}]} as TaskProgress).title).toBe('Work needs attention');
 expect(taskStage(expired,true).title).toBe('Progress unavailable');
});

it('directs trigger-owned expired approvals to issue recovery',()=>{
 for(const origin of [{triggerGitHubIssue:'owner/repo#1'},{triggerIssueOrigin:{kind:'jira'}}]){
  const stage=taskStage({state:'failed',decisions:[{id:'a',status:'expired'}],task:origin} as TaskProgress);
  expect(stage.next).toContain('Recover this issue through its issue trigger');
  expect(stage.next).not.toContain('Try again');
 }
});
