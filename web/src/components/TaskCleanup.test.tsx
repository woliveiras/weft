import {cleanup,render,screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it,vi} from 'vitest';
import {fakeApi,apiError} from '../test/fakeApi';
import {TaskCleanup} from './TaskCleanup';
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.unstubAllGlobals();});
const preview={path:'/fixture/task',branch:'retained',head:'abc',status:'',clean:true,fingerprint:'a'.repeat(64)};
it('requires preview and explicit confirmation before cleanup and retains errors',async()=>{
 const backend=fakeApi({'GET /api/v1/tasks/t/cleanup':()=>preview,'POST /api/v1/tasks/t/cleanup':()=>apiError(409,'Checkout changed','conflict')});
 render(<TaskCleanup taskId="t" workspaces={[]} onChanged={()=>{}}/>);
 await userEvent.click(screen.getByText('Review workspace cleanup'));expect(backend.calls).toHaveLength(0);
 await userEvent.click(screen.getByRole('button',{name:'Preview cleanup'}));await screen.findByText('/fixture/task');
 expect((screen.getByRole('button',{name:'Remove reviewed checkout'}) as HTMLButtonElement).disabled).toBe(true);
 await userEvent.click(screen.getByRole('checkbox'));await userEvent.click(screen.getByRole('button',{name:'Remove reviewed checkout'}));await screen.findByText('Checkout changed');
 expect(backend.calls.at(-1)?.body).toEqual({confirm:true,fingerprint:preview.fingerprint});expect(screen.queryByRole('button',{name:'Remove reviewed checkout'})).toBeNull();
});
it('blocks dirty cleanup and clears review when workspace selection changes',async()=>{
 const backend=fakeApi({'GET /api/v1/tasks/t/cleanup':()=>({...preview,clean:false,status:'!! local.cache'})});
 render(<TaskCleanup taskId="t" workspaces={[{nodeId:'worker',replica:0,path:'/fixture/worker',inspectionNode:'worker',subdir:''}]} onChanged={()=>{}}/>);
 await userEvent.click(screen.getByText('Review workspace cleanup'));await userEvent.click(screen.getByRole('button',{name:'Preview cleanup'}));await screen.findByText('!! local.cache');
 expect(screen.queryByRole('button',{name:'Remove reviewed checkout'})).toBeNull();
 await userEvent.selectOptions(screen.getByLabelText('Workspace to clean'),JSON.stringify(['worker',0]));expect(screen.queryByRole('region',{name:'Workspace cleanup preview'})).toBeNull();expect(backend.calls.every(c=>c.method==='GET')).toBe(true);
});
