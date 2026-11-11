import {cleanup,render,screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it,vi} from 'vitest';
import type {Approval} from '../api/types';
import {fakeApi,apiError} from '../test/fakeApi';
import {TaskDecisions,taskDecisions} from './TaskDecisions';
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.unstubAllGlobals();});
const approval={id:'a',graphId:'task-run-a',kind:'message',status:'pending',title:'Review this task',createdAt:'2026-10-19T00:00:00Z'} as Approval;
it('keeps only pending decisions for the execution copy',()=>{
 expect(taskDecisions([approval,{...approval,id:'other',graphId:'other'},{...approval,id:'done',status:'approved'}],'task-run-a')).toEqual([approval]);
});
it('preserves decision authority and unavailable state',()=>{
 const {rerender}=render(<TaskDecisions graphId="task-run-a" approvals={[approval]} canDecide={false} canAdmin={false} onChanged={()=>{}}/>);
 expect(screen.queryByRole('button',{name:'Approve'})).toBeNull();expect(screen.getByText('Waiting for an authorized operator to decide.')).toBeTruthy();
 rerender(<TaskDecisions graphId="task-run-a" approvals={[{...approval,kind:'environment'}]} canDecide canAdmin={false} onChanged={()=>{}}/>);
 expect(screen.queryByRole('button',{name:'Approve'})).toBeNull();
 rerender(<TaskDecisions graphId="task-run-a" canDecide canAdmin onChanged={()=>{}}/>);
 expect(screen.getByText('Approval state is unavailable. Refresh the task to check.')).toBeTruthy();expect(screen.queryByText('No pending approvals in the latest task snapshot.')).toBeNull();
});
it('uses the existing decision API, surfaces conflict and refreshes only on success',async()=>{
 let fail=true;const done=vi.fn();
 const backend=fakeApi({'POST /api/v1/approvals/a/approve':()=>fail?apiError(409,'Decision already changed','conflict'):{...approval,status:'approved'}});
 render(<TaskDecisions graphId="task-run-a" approvals={[approval]} canDecide canAdmin onChanged={done}/>);
 await userEvent.click(screen.getByRole('button',{name:'Approve'}));expect(await screen.findByText('Decision already changed')).toBeTruthy();expect(done).not.toHaveBeenCalled();
 fail=false;await userEvent.click(screen.getByRole('button',{name:'Approve'}));expect(done).toHaveBeenCalledOnce();expect(backend.calls.map(c=>c.path)).toEqual(['/api/v1/approvals/a/approve','/api/v1/approvals/a/approve']);
});
