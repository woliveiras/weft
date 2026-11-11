import {cleanup,render,screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it,vi} from 'vitest';
import {fakeApi,apiError} from '../test/fakeApi';
import {TaskCheckpoints} from './TaskCheckpoints';
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.unstubAllGlobals();});
const cp=(seq:number,reason='before_edit',tool='filesystem.write')=>({id:`dev/r0/${seq}`,ref:`refs/weft/checkpoints/g/dev/r0/0000000${seq}`,commit:'c'.repeat(40),tree:'t'.repeat(40),nodeId:'dev',replica:0,sequence:seq,executionId:'exe-1',reason,tool,createdAt:`2026-11-11T10:0${seq}:00Z`});
const list=[cp(3,'before_restore','api'),cp(2),cp(1)];

it('lists checkpoints only when opened and keeps viewers read-only',async()=>{
 const backend=fakeApi({'GET /api/v1/graphs/g/checkpoints':()=>list});
 render(<TaskCheckpoints graphId="g" canRestore={false}/>);
 expect(backend.calls).toHaveLength(0);
 await userEvent.click(screen.getByText('Checkpoints'));
 expect(await screen.findByText('dev/r0/2')).toBeTruthy();
 expect(screen.getByText('Before a restore')).toBeTruthy();expect(screen.getAllByText('Before filesystem.write')).toHaveLength(2);
 expect(screen.getByText('Only administrators can restore a checkpoint.')).toBeTruthy();
 expect(screen.queryByRole('button',{name:'Restore checkpoint'})).toBeNull();
 expect(backend.calls.every(c=>c.method==='GET')).toBe(true);
});

it('restores the last good checkpoint only after explicit confirmation',async()=>{
 const backend=fakeApi({'GET /api/v1/graphs/g/checkpoints':()=>list,'POST /api/v1/graphs/g/checkpoints/restore':()=>({restored:cp(2),saved:cp(4,'before_restore','api'),written:3,removed:1})});
 render(<TaskCheckpoints graphId="g" canRestore/>);
 await userEvent.click(screen.getByText('Checkpoints'));await screen.findByText('dev/r0/2');
 // The newest checkpoint recorded before an edit is preselected, not the saved pre-restore state.
 expect((screen.getByLabelText('Checkpoint to restore') as HTMLSelectElement).value).toBe('dev/r0/2');
 const button=screen.getByRole('button',{name:'Restore checkpoint'}) as HTMLButtonElement;expect(button.disabled).toBe(true);
 await userEvent.click(screen.getByRole('checkbox'));await userEvent.click(button);
 expect(await screen.findByText('Restored dev/r0/2: 3 files written, 1 removed. The previous state is checkpoint dev/r0/4.')).toBeTruthy();
 expect(backend.find('POST','/api/v1/graphs/g/checkpoints/restore').map(c=>c.body)).toEqual([{checkpoint:'dev/r0/2',confirm:true}]);
 expect(backend.find('GET','/api/v1/graphs/g/checkpoints')).toHaveLength(2);
 expect((screen.getByRole('checkbox') as HTMLInputElement).checked).toBe(false);
});

it('reports a refused restore and requires confirming again',async()=>{
 fakeApi({'GET /api/v1/graphs/g/checkpoints':()=>list,'POST /api/v1/graphs/g/checkpoints/restore':()=>apiError(409,'dev replica 0 is running a turn','conflict')});
 render(<TaskCheckpoints graphId="g" canRestore/>);
 await userEvent.click(screen.getByText('Checkpoints'));await screen.findByText('dev/r0/2');
 await userEvent.selectOptions(screen.getByLabelText('Checkpoint to restore'),'dev/r0/1');
 expect(screen.getByText(/rewrites the files of dev replica 0/)).toBeTruthy();
 await userEvent.click(screen.getByRole('checkbox'));await userEvent.click(screen.getByRole('button',{name:'Restore checkpoint'}));
 expect(await screen.findByText('dev replica 0 is running a turn')).toBeTruthy();
 expect((screen.getByRole('checkbox') as HTMLInputElement).checked).toBe(false);
 expect((screen.getByRole('button',{name:'Restore checkpoint'}) as HTMLButtonElement).disabled).toBe(true);
});

it('explains when there are no checkpoints and shows load errors',async()=>{
 fakeApi({'GET /api/v1/graphs/g/checkpoints':()=>[]});
 render(<TaskCheckpoints graphId="g" canRestore/>);
 await userEvent.click(screen.getByText('Checkpoints'));
 expect(await screen.findByText(/No checkpoints recorded/)).toBeTruthy();
 expect(screen.queryByRole('button',{name:'Restore checkpoint'})).toBeNull();
 cleanup();
 fakeApi({'GET /api/v1/graphs/h/checkpoints':()=>apiError(404,'graph not found','not_found')});
 render(<TaskCheckpoints graphId="h" canRestore/>);
 await userEvent.click(screen.getByText('Checkpoints'));
 expect(await screen.findByText('graph not found')).toBeTruthy();
});
