import {describe,it,expect} from 'vitest';
import {taskStage} from './taskStage';
import type {TaskProgress} from '../api/tasks';
describe('task stages',()=>{
 it.each([['running','Agents are working'],['succeeded','Ready for review'],['cancel_requested','Stopping work'],['cancelled','Work stopped'],['failed','Work needs attention'],['blocked','Work needs attention'],['environment_pending','Preparing the environment']])('%s explains the next step',(state,title)=>{expect(taskStage({state} as TaskProgress).title).toBe(title);});
 it('does not claim progress from stale data',()=>{expect(taskStage({state:'succeeded'} as TaskProgress,true).title).toBe('Progress unavailable');expect(taskStage().title).toBe('Loading task');});
 it('prioritizes a durable pending decision',()=>{expect(taskStage({state:'running',pendingApprovals:[{id:'same-approval'}]} as TaskProgress).title).toBe('Needs your decision');});
});
