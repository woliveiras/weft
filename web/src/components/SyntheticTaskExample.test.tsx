import {cleanup,render,screen,waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it,vi} from 'vitest';
import {SyntheticTaskExample,syntheticWorkflow} from './SyntheticTaskExample';
import {fakeApi,apiError} from '../test/fakeApi';
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.unstubAllGlobals();});
it('defines only an echo agent with no tool grants or repository',()=>{
 const g=syntheticWorkflow('example-fixture');
 expect(g.spec.nodes).toHaveLength(1);expect(g.spec.nodes[0].runtime?.kind).toBe('echo');
 expect(g.spec.nodes[0].tools).toEqual([]);expect(g.spec.workspace).toBeUndefined();expect(g.spec.channels).toBeUndefined();
});
it('requires preview and consent, then saves without starting',async()=>{
 const backend=fakeApi({'POST /api/v1/graphs':()=>({graph:{...syntheticWorkflow('example-fixture'),metadata:{id:'example-fixture',version:1}},record:{version:1}}),'POST /api/v1/tasks':()=>({id:'tsk_example'})});
 render(<SyntheticTaskExample/>);
 expect(backend.calls).toHaveLength(0);
 await userEvent.click(screen.getByRole('button',{name:'Preview example'}));
 expect((screen.getByRole('button',{name:'Prepare example'}) as HTMLButtonElement).disabled).toBe(true);
 await userEvent.click(screen.getByRole('checkbox'));await userEvent.click(screen.getByRole('button',{name:'Prepare example'}));
 await waitFor(()=>expect(location.hash).toBe('#/tasks/tsk_example'));
 expect(backend.calls.map(c=>c.path)).toEqual(['/api/v1/graphs','/api/v1/tasks']);
});
it('does not retry an uncertain partial preparation or start any agent',async()=>{
 const backend=fakeApi({'POST /api/v1/graphs':()=>({graph:{metadata:{id:'example-fixture',version:1}},record:{version:1}}),'POST /api/v1/tasks':()=>apiError(503,'Storage unavailable','unavailable')});
 render(<SyntheticTaskExample/>);await userEvent.click(screen.getByRole('button',{name:'Preview example'}));await userEvent.click(screen.getByRole('checkbox'));await userEvent.click(screen.getByRole('button',{name:'Prepare example'}));
 await screen.findByText('Storage unavailable');expect((screen.getByRole('button',{name:'Prepare example'}) as HTMLButtonElement).disabled).toBe(true);
 expect(screen.getByText(/Preparation may have saved/)).toBeTruthy();expect(backend.calls).toHaveLength(2);
});
it('reports unavailable secure randomness before any write',async()=>{
 vi.stubGlobal('crypto',{});
 const backend=fakeApi({});render(<SyntheticTaskExample/>);
 await userEvent.click(screen.getByRole('button',{name:'Preview example'}));await userEvent.click(screen.getByRole('checkbox'));await userEvent.click(screen.getByRole('button',{name:'Prepare example'}));
 await screen.findByText('Preparing an example requires HTTPS or a localhost browser connection.');expect(backend.calls).toHaveLength(0);
});
