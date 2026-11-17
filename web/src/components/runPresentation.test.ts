import { expect,it } from 'vitest';
import type { Execution,WeftEvent } from '../api/types';
import { blockedToolCalls,matchingRuns,runEvents,runLink,runSection } from './runPresentation';
it('filters by graph, query and status without interpreting success as tool success',()=>{
 const runs=[{id:'a',graphId:'g',nodeId:'Reviewer',status:'succeeded'},{id:'b',graphId:'other',nodeId:'Reviewer',status:'succeeded'}] as Execution[];
 expect(matchingRuns(runs,'g',' REVIEWER ','succeeded')).toEqual([runs[0]]);
 expect(matchingRuns(runs,'g','','failed')).toEqual([]);
 const events=[{id:'denied',graphId:'g',executionId:'a',seq:2,type:'tool.denied'},{id:'old',graphId:'g',executionId:'old',seq:1,type:'tool.denied'},{id:'start',graphId:'g',executionId:'a',seq:1,type:'tool.invoked'}] as WeftEvent[];
 expect(runEvents(events,runs[0]).map(e=>e.id)).toEqual(['start','denied']);
 expect(blockedToolCalls(runEvents(events,runs[0]))).toBe(1);expect(runs[0].status).toBe('succeeded');
});
it('builds encoded links and defaults invalid sections safely',()=>{
 expect(runLink('a/b','exec/1','tools')).toBe('#/g/a%2Fb/runs?execution=exec%2F1&section=tools');
 expect(runLink('g')).toBe('#/g/g/runs');expect(runSection('bogus')).toBe('result');expect(runSection('messages')).toBe('messages');
});
