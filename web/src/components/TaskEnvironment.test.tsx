import {cleanup,render,screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it,vi} from 'vitest';
import {TaskEnvironment} from './TaskEnvironment';
import {fakeApi,apiError} from '../test/fakeApi';
import type {TaskDraft} from '../api/tasks';
import type {EnvironmentView} from '../api/types';
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.unstubAllGlobals();});
const task={id:'t',revision:4,status:'environment_pending'} as TaskDraft;
it('requires a fresh review and separate confirmation after build readiness',async()=>{
 const backend=fakeApi({'GET /api/v1/tasks/t/review':()=>({canStart:true,currentGraphVersion:1,blockers:[],task}),'POST /api/v1/tasks/t/start':()=>apiError(409,'Task changed','conflict')});
 render(<TaskEnvironment task={task} environment={{state:'ready',upToDate:true} as EnvironmentView} canControl onChanged={()=>{}}/>);
 expect(backend.calls).toHaveLength(0);expect((screen.getByRole('button',{name:'Start prepared task'}) as HTMLButtonElement).disabled).toBe(true);
 await userEvent.click(screen.getByRole('button',{name:'Review prepared task requirements'}));await screen.findByText(/Workflow version 1/);
 expect((screen.getByRole('button',{name:'Start prepared task'}) as HTMLButtonElement).disabled).toBe(true);
 await userEvent.click(screen.getByRole('checkbox',{name:'I reviewed this task environment and authorize the selected build or start action.'}));await userEvent.click(screen.getByRole('button',{name:'Start prepared task'}));await screen.findByText('Task changed');
 expect(backend.calls.at(-1)?.headers['If-Match']).toBe('4');expect((screen.getByRole('checkbox',{name:'I reviewed this task environment and authorize the selected build or start action.'}) as HTMLInputElement).checked).toBe(false);
});
it('keeps viewers read-only and displays build failures without a result',()=>{
 render(<TaskEnvironment task={task} environment={{state:'failed',upToDate:false,error:'Setup denied'} as EnvironmentView} canControl={false} onChanged={()=>{}}/>);
 expect(screen.getByText('Setup denied')).toBeTruthy();expect(screen.queryByRole('checkbox',{name:'I reviewed this task environment and authorize the selected build or start action.'})).toBeNull();expect(screen.queryByRole('button',{name:'Retry environment build'})).toBeNull();expect(screen.queryByRole('button',{name:'Start prepared task'})).toBeNull();
});

it('shows updating inert logs and lets the user pause following without a write',async()=>{
 const backend=fakeApi({});
 const environment={state:'building',phase:'building_image',upToDate:false,log:'Step 1\n<script>inert</script>'} as EnvironmentView;
 const {rerender}=render(<TaskEnvironment task={task} environment={environment} canControl onChanged={()=>{}}/>);
 const output=screen.getByLabelText('Build output');
 expect(output.getAttribute('tabindex')).toBe('0');
 expect(output.textContent).toBe(environment.log);
 expect(output.querySelector('script')).toBeNull();
 expect(screen.getByRole('button',{name:'Review prepared task requirements'})).toHaveProperty('disabled',true);
 expect(screen.getByRole('button',{name:'Retry environment build'})).toHaveProperty('disabled',true);
 const follow=screen.getByRole('checkbox',{name:'Follow latest output'});
 expect(follow).toHaveProperty('checked',true);
 await userEvent.click(follow);
 Object.defineProperty(output,'scrollHeight',{configurable:true,value:500});
 output.scrollTop=75;
 rerender(<TaskEnvironment task={task} environment={{...environment,log:'Step 2\nDone'}} canControl onChanged={()=>{}}/>);
 expect(output.textContent).toBe('Step 2\nDone');expect(output.scrollTop).toBe(75);
 await userEvent.click(follow);expect(output.scrollTop).toBe(500);
 expect(backend.calls).toHaveLength(0);
});

it('provides an explicit empty log state',()=>{
 render(<TaskEnvironment task={task} environment={{state:'ready',upToDate:true} as EnvironmentView} canControl={false} onChanged={()=>{}}/>);
 expect(screen.getByText(/No build log recorded yet/)).toBeTruthy();
 expect(screen.queryByLabelText('Build output')).toBeNull();
});
