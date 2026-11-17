import {act,cleanup,render,screen,within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it,vi} from 'vitest';
import {CheckEvidence,latestChecks} from './CheckEvidence';
import {fakeApi,apiError} from '../test/fakeApi';
import type {WeftEvent} from '../api/types';
afterEach(()=>{cleanup();vi.useRealTimers();vi.restoreAllMocks();vi.unstubAllGlobals();});

const run=(seq:number,data:Record<string,unknown>,extra:Partial<WeftEvent>={}):WeftEvent=>({id:`e${seq}`,seq,type:'check.completed',time:`2026-11-11T10:00:0${seq}Z`,graphId:'g',nodeId:'worker',replica:0,data:{command:'go test ./...',exitCode:data.passed===false?1:0,durationMs:1,workspace:{head:'abc',state:'s1'},...data},...extra});
const observed=(seq:number,data:Record<string,unknown>,extra:Partial<WeftEvent>={}):WeftEvent=>({id:`o${seq}`,seq,type:'workspace.observed',time:'2026-11-11T10:00:00Z',graphId:'g',nodeId:'worker',replica:0,data,...extra});
const graph=(checks:{name:string;command:string}[])=>()=>({graph:{spec:{nodes:[],checks}}});

it('keeps the latest run per check and replica and marks it stale after later changes',()=>{
 const rows=latestChecks([
  run(1,{check:'test',passed:false}),
  run(2,{check:'test',passed:true}),
  run(3,{check:'lint',passed:true}),
  run(4,{check:'test',passed:true},{nodeId:'reviewer'}),
  observed(5,{tool:'check.run',changes:[{path:'bin/app',operation:'created'}]}),
  observed(6,{tool:'filesystem.write',changes:[{path:'a.go',operation:'modified'}]},{nodeId:'reviewer'}),
  run(7,{check:'build',passed:true,workspace:{head:'abc',state:'s2'}}),
  run(8,{check:'nope'},{}),
 ]);
 const byKey=Object.fromEntries(rows.map(r=>[`${r.check}@${r.nodeId}`,r]));
 expect(Object.keys(byKey).sort()).toEqual(['build@worker','lint@worker','test@reviewer','test@worker']);
 expect(byKey['test@worker'].passed).toBe(true);
 // a later run saw a different tree in the same worktree
 expect(byKey['test@worker'].stale).toBe(true);
 expect(byKey['lint@worker'].stale).toBe(true);
 // an edit after the reviewer's run; a check's own build output is not one
 expect(byKey['test@reviewer'].stale).toBe(true);
 expect(byKey['build@worker'].stale).toBe(false);
 expect(latestChecks([run(1,{check:'test',passed:true}),run(2,{check:'test',passed:true},{nodeId:'reviewer'})],{nodeId:'reviewer',replica:0}).map(r=>r.nodeId)).toEqual(['reviewer']);
});

it('lists declared checks that never ran and does not claim all passed',async()=>{
 fakeApi({'GET /api/v1/graphs/g':graph([{name:'test',command:'go test ./...'},{name:'lint',command:'go vet ./...'}]),'GET /api/v1/graphs/g/events':()=>[run(1,{check:'test',passed:true})]});
 render(<CheckEvidence graphId="g"/>);
 expect(await screen.findByText(/1 of 2 declared checks passed on the current workspace; 1 were not run\./)).toBeTruthy();
 expect(screen.getByText(/· not run/)).toBeTruthy();
 expect(screen.queryByText(/All 2/)).toBeNull();
});

it('says nothing was verified when no check is declared or run',async()=>{
 fakeApi({'GET /api/v1/graphs/g':graph([]),'GET /api/v1/graphs/g/events':()=>[]});
 render(<CheckEvidence graphId="g"/>);
 expect(await screen.findByText(/Not independently verified. This task declares no checks/)).toBeTruthy();
});

it('shows failures, stale passes and changed test files',async()=>{
 fakeApi({'GET /api/v1/graphs/g':graph([{name:'test',command:'go test ./...'},{name:'build',command:'go build ./...'}]),'GET /api/v1/graphs/g/events':()=>[
  run(1,{check:'test',passed:false,format:'go',failureCount:3,failures:[{test:'TestAdd',file:'calc_test.go',line:9,message:'got 3, want 4'},{file:'main.go',line:4,message:'undefined: x'}]}),
  run(2,{check:'build',passed:true,workspace:{head:'abc',state:'s1',checkFilesChanged:['calc_test.go']}}),
  observed(3,{tool:'filesystem.edit',changes:[{path:'main.go',operation:'modified'}]}),
  run(4,{check:'secret',passed:true},{graphId:'other'}),
 ]});
 render(<CheckEvidence graphId="g"/>);
 expect(await screen.findByText(/0 of 2 declared checks passed on the current workspace; 1 failed; 1 need a rerun after later changes\./)).toBeTruthy();
 const failures=within(screen.getByRole('list',{name:'test failures'}));
 expect(failures.getByText('TestAdd calc_test.go:9: got 3, want 4')).toBeTruthy();
 expect(failures.getByText('1 more not shown')).toBeTruthy();
 expect(screen.getByText(/passed, but the workspace changed after this run/)).toBeTruthy();
 expect(screen.getByRole('note').textContent).toContain('calc_test.go');
 expect(screen.queryByText('secret')).toBeNull();
});

it('refreshes on request and on a timer',async()=>{
 vi.useFakeTimers({shouldAdvanceTime:true});
 let events:WeftEvent[]=[];
 const backend=fakeApi({'GET /api/v1/graphs/g':graph([{name:'test',command:'go test ./...'}]),'GET /api/v1/graphs/g/events':()=>events});
 render(<CheckEvidence graphId="g"/>);
 expect(await screen.findByText(/0 of 1 declared checks passed/)).toBeTruthy();
 events=[run(1,{check:'test',passed:true})];
 await act(async()=>{await vi.advanceTimersByTimeAsync(15000);});
 expect(await screen.findByText(/All 1 declared checks passed/)).toBeTruthy();
 events=[run(1,{check:'test',passed:true}),run(2,{check:'test',passed:false})];
 await userEvent.setup({advanceTimers:vi.advanceTimersByTime}).click(screen.getByRole('button',{name:'Refresh checks'}));
 expect(await screen.findByText(/0 of 1 declared checks passed on the current workspace; 1 failed/)).toBeTruthy();
 expect(backend.calls.every(c=>c.method==='GET')).toBe(true);
});

it('reports unavailable check evidence without claiming a result',async()=>{
 fakeApi({'GET /api/v1/graphs/g':graph([{name:'test',command:'x'}]),'GET /api/v1/graphs/g/events':()=>apiError(503,'Events unavailable','unavailable')});
 render(<CheckEvidence graphId="g"/>);
 expect(await screen.findByText(/Check results unavailable: Events unavailable/)).toBeTruthy();
 expect(screen.queryByText(/passed/)).toBeNull();
});
