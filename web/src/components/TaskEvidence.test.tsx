import {cleanup,render,screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it,vi} from 'vitest';
import {TaskEvidence} from './TaskEvidence';
import {fakeApi,apiError} from '../test/fakeApi';
import type {Execution} from '../api/types';
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.unstubAllGlobals();});
it('loads Git evidence only on request and keeps every call read-only',async()=>{
 const backend=fakeApi({'GET /api/v1/graphs/task-run-fixture/git/status':()=>({repository:true,directory:'/fixture',branch:'task',content:' M file.txt'}),'GET /api/v1/graphs/task-run-fixture':()=>({graph:{spec:{nodes:[]}}}),'GET /api/v1/graphs/task-run-fixture/events':()=>[],'GET /api/v1/graphs/task-run-fixture/executions':()=>[]});
 render(<TaskEvidence graphId="task-run-fixture" executions={[{id:'exec-fixture',nodeId:'worker',status:'succeeded'} as Execution]}/>);
 expect(screen.getByText('Remote state not verified. No automatic push is performed on task completion.')).toBeTruthy();
 expect(screen.getByText('Not inferred from task completion. Inspect local Git history for the selected workspace.')).toBeTruthy();
 expect(backend.calls.filter(c=>!(c.method==='GET'&&(c.path.endsWith('/events')||c.path==='/api/v1/graphs/task-run-fixture')))).toHaveLength(0);expect(screen.getByRole('link',{name:/worker.*Tool calls/}).getAttribute('href')).toBe('#/g/task-run-fixture/runs?execution=exec-fixture&section=tools');
 await userEvent.click(screen.getByRole('button',{name:'Inspect changes'}));await screen.findByText('M file.txt');
 expect(backend.calls.every(c=>c.method==='GET')).toBe(true);
});
it('shows unavailable evidence without inventing a passing test result',async()=>{
 fakeApi({'GET /api/v1/graphs/g/git/status':()=>apiError(503,'Git temporarily unavailable','unavailable'),'GET /api/v1/graphs/g':()=>({graph:{spec:{nodes:[],checks:[{name:'test',command:'go test ./...'}]}}}),'GET /api/v1/graphs/g/events':()=>[],'GET /api/v1/graphs/g/executions':()=>[]});
 render(<TaskEvidence graphId="g" executions={[]}/>);await userEvent.click(screen.getByRole('button',{name:'Inspect changes'}));
 await screen.findByText('Git temporarily unavailable');expect(screen.getByText('No execution evidence recorded yet.')).toBeTruthy();expect(screen.getByText(/not proof that tests passed/)).toBeTruthy();
});
it('inspects exact execution outcomes inline without treating completion as tool success',async()=>{
 const backend=fakeApi({'GET /api/v1/graphs/task-run-fixture':()=>({graph:{spec:{nodes:[]}}}),'GET /api/v1/graphs/task-run-fixture/events':()=>[
 {id:'one',seq:1,graphId:'task-run-fixture',executionId:'exec-fixture',type:'tool.invoked',time:'2026-10-19T10:00:00Z',data:{tool:'shell.exec',outcome:'started'}},
 {id:'two',seq:2,graphId:'task-run-fixture',executionId:'exec-fixture',type:'tool.denied',time:'2026-10-19T10:00:01Z',data:{tool:'shell.exec',outcome:'denied'}},
 {id:'other',seq:3,graphId:'other',executionId:'exec-fixture',type:'tool.completed',data:{tool:'private-marker'}}]});
 render(<TaskEvidence graphId="task-run-fixture" role="viewer" executions={[{id:'exec-fixture',graphId:'task-run-fixture',nodeId:'worker',status:'succeeded'} as Execution]}/>);
 expect(backend.calls.filter(c=>!(c.method==='GET'&&(c.path.endsWith('/events')||c.path==='/api/v1/graphs/task-run-fixture')))).toHaveLength(0);
 await userEvent.selectOptions(screen.getByLabelText('Inspect execution evidence'),'exec-fixture');
 expect(await screen.findByText('1 blocked tool calls in this window. The execution status remains Completed.')).toBeTruthy();
 expect(screen.getByText('Execution: succeeded. Individual tool outcomes may differ.')).toBeTruthy();
 expect(screen.queryByText('private-marker')).toBeNull();expect(backend.calls.every(c=>c.method==='GET')).toBe(true);
 expect(screen.getByText('Execution tree and interruption')).toBeTruthy();
});

it('switches read-only inspection to the selected agent workspace',async()=>{
 const backend=fakeApi({'GET /api/v1/graphs/g/git/status':()=>({repository:true,directory:'/fixture/worker',branch:'worker',content:' M agent.txt'}),'GET /api/v1/graphs/g':()=>({graph:{spec:{nodes:[],checks:[{name:'test',command:'go test ./...'}]}}}),'GET /api/v1/graphs/g/events':()=>[],'GET /api/v1/graphs/g/executions':()=>[]});
 const workspaces=[{nodeId:'worker',replica:1,path:'/fixture/worker',branch:'worker',inspectionNode:'worker',subdir:''}];
 const view=render(<TaskEvidence graphId="g" executions={[]} workspaces={workspaces}/>);
 await userEvent.selectOptions(screen.getByLabelText('Workspace to review'),JSON.stringify(['worker',1]));
 expect(screen.getByText('/fixture/worker')).toBeTruthy();
 await userEvent.click(screen.getByRole('button',{name:'Inspect changes'}));await screen.findByText('M agent.txt');
 expect(backend.fetch.mock.calls.some(([url])=>String(url).includes('node=worker')&&String(url).includes('replica=1'))).toBe(true);
 expect(backend.calls.every(c=>c.method==='GET')).toBe(true);
 view.rerender(<TaskEvidence graphId="g" executions={[]} workspaces={[]}/>);
 expect(screen.queryByRole('region',{name:'Git inspection'})).toBeNull();
 expect((screen.getByRole('button',{name:'Hide changes'}) as HTMLButtonElement).disabled).toBe(true);
});

it('shows recorded declared checks instead of an unverified test claim',async()=>{
 const backend=fakeApi({'GET /api/v1/graphs/g':()=>({graph:{spec:{nodes:[],checks:[{name:'test',command:'go test ./...'}]}}}),'GET /api/v1/graphs/g/events':()=>[
 {id:'c1',seq:1,graphId:'g',nodeId:'worker',type:'check.completed',time:'2026-11-11T10:00:00Z',data:{check:'test',command:'go test ./...',passed:true,exitCode:0,durationMs:900,workspace:{head:'abc',state:'s1'}}}]});
 render(<TaskEvidence graphId="g" executions={[]}/>);
 expect(await screen.findByText(/All 1 declared checks passed on the current workspace\./)).toBeTruthy();
 expect(screen.queryByText(/Not independently verified/)).toBeNull();
 expect(backend.fetch.mock.calls.some(([url])=>String(url).includes('types=check.completed%2Cworkspace.observed'))).toBe(true);
 expect(backend.calls.every(c=>c.method==='GET')).toBe(true);
});
