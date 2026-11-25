import {act,cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it,vi} from 'vitest';
import {TasksPage} from './TasksPage';
import {fakeApi,apiError} from '../test/fakeApi';
import type {GraphSummary,Info} from '../api/types';
import type {TaskDraft} from '../api/tasks';
const graphs=[{graph:{metadata:{id:'flow',name:'Ready workflow',version:1},spec:{nodes:[{id:'worker',runtime:{kind:'echo'},isolation:{mode:'docker'}}]}}}] as GraphSummary[];
const info={identity:{role:'admin'}} as Info;
const initial:TaskDraft={id:'tsk_launch',revision:1,status:'draft',title:'Inspect',request:'Inspect code',outcome:'freeform',graphId:'flow',graphVersion:1,nodeId:'worker',resultNodeId:'worker',createdBy:'admin',updatedBy:'admin',createdAt:'',updatedAt:''};
const budget={currency:'USD',taskRunBudgetMicros:2_000_000,budgets:[]};
function setup({saveFailure=false,reviewFailure=false,authFailure=false,budgetFailure=false,stale=false,emptyScope=false,normalizedSave=false,mismatchedReview=false}={}){
 let task={...initial};let scope=emptyScope?'':'reviewed';let limit=budget.taskRunBudgetMicros;
 const backend=fakeApi({
  'GET /api/v1/providers':()=>[], 'GET /api/v1/projects':()=>[], 'GET /api/v1/scm/connections':()=>[],
  'GET /api/v1/tasks/tsk_launch':()=>task,
  'GET /api/v1/cost/budgets/preview':()=>budgetFailure?apiError(503,'Budget offline','unavailable'):{...budget,taskRunBudgetMicros:limit},
  'GET /api/v1/tasks/tsk_launch/review':()=>reviewFailure?apiError(503,'Review offline','unavailable'):{task:mismatchedReview?{...task,request:'Unexpected server input'}:task,currentGraphVersion:1,canStart:true,validation:{},blockers:[],agents:[{id:'worker',model:{},isolation:'docker',grants:[{name:'filesystem.read'}]}],grants:[],model:{},isolation:'docker'},
  'GET /api/v1/tasks/tsk_launch/authorization':()=>authFailure?apiError(503,'Authorization offline','unavailable'):{scope,record:stale?{scope:'old',mode:'full'}:undefined},
  'POST /api/v1/tasks':call=>{if(saveFailure)return apiError(409,'Save conflict','conflict');task={...initial,...call.body as object,...(normalizedSave?{request:'Unexpected server input'}:{})};return task;},
  'PUT /api/v1/tasks/tsk_launch':call=>{if(saveFailure)return apiError(409,'Save conflict','conflict');task={...task,...call.body as object,revision:task.revision+1};return task;},
  'POST /api/v1/tasks/tsk_launch/start':()=>{task={...task,revision:task.revision+1,status:'running',executionGraphId:'task-run-launch'};return task;},
  'GET /api/v1/tasks/tsk_launch/progress':()=>({task,state:'running',executions:[],pendingApprovals:[],blockers:[]}),
 });
 return {...backend,setScope:(value:string)=>{scope=value;},setLimit:(value:number)=>{limit=value;}};
}
afterEach(()=>{cleanup();vi.unstubAllGlobals();});
it('shows request, workflow and repository without advanced routing in the main path',async()=>{
 setup();render(<TasksPage create info={info} graphs={graphs}/>);
 await userEvent.type(screen.getByLabelText('Request'),'Inspect code');await userEvent.selectOptions(screen.getByLabelText('Workflow'),'flow');
 expect(screen.getByLabelText('Project').closest('details')).toBeNull();
 expect(screen.getByText('Advanced settings').closest('details')).toHaveProperty('open',false);
 expect(screen.getByRole('button',{name:'Run task'})).toBeTruthy();
 expect(screen.queryByRole('button',{name:'Review requirements'})).toBeNull();
 expect(screen.queryByRole('button',{name:'Start task'})).toBeNull();
});
it('opens advanced settings for invalid channel JSON and preserves it without saving or starting',async()=>{
 const backend=setup();const channelGraph=[{...graphs[0],graph:{...graphs[0].graph,spec:{...graphs[0].graph.spec,channels:[{id:'requests',to:['worker'],types:['work.request']}]}}}];
 render(<TasksPage create info={info} graphs={channelGraph}/>);
 await userEvent.type(screen.getByLabelText('Request'),'Inspect code');await userEvent.selectOptions(screen.getByLabelText('Workflow'),'flow');
 const advanced=screen.getByText('Advanced settings').closest('details')!;
 await userEvent.click(screen.getByText('Advanced settings'));
 fireEvent.change(screen.getByLabelText('Input payload (JSON)'),{target:{value:'{broken'}});
 await userEvent.click(screen.getByRole('button',{name:'Run task'}));
 expect(advanced).toHaveProperty('open',true);
 expect(screen.getByText('Enter valid JSON for the selected channel.')).toBeTruthy();
 expect(screen.getByLabelText('Input payload (JSON)')).toHaveProperty('value','{broken');
 expect(backend.find('POST','/api/v1/tasks')).toHaveLength(0);
 expect(backend.find('POST','/api/v1/tasks/tsk_launch/start')).toHaveLength(0);
});
it('saves a new request, displays a preflight and runs on the next explicit click without calling a model',async()=>{
 const backend=setup();render(<TasksPage create info={info} graphs={graphs}/>);
 await userEvent.type(screen.getByLabelText('Request'),'Inspect code');await userEvent.selectOptions(screen.getByLabelText('Workflow'),'flow');
 await userEvent.click(screen.getByRole('button',{name:'Run task'}));
 const preflight=await screen.findByRole('region',{name:'Launch review'});
 expect(preflight.textContent).toContain('Ask for approval (default)');expect(preflight.textContent).toContain('filesystem.read');
 expect(backend.find('POST','/api/v1/tasks')).toHaveLength(1);expect(backend.find('POST','/api/v1/tasks/tsk_launch/start')).toHaveLength(0);
 await userEvent.click(screen.getByRole('button',{name:'Run task'}));
 await waitFor(()=>expect(backend.find('POST','/api/v1/tasks/tsk_launch/start')).toHaveLength(1));
 expect(backend.find('POST','/api/v1/tasks/tsk_launch/start')[0].headers['If-Match']).toBe('1');
 expect(backend.find('POST','/api/v1/tasks/submit')).toHaveLength(0);expect(backend.find('POST','/api/v1/tasks/prompt')).toHaveLength(0);
 expect(backend.find('POST','/api/v1/tasks/tsk_launch/authorization')).toHaveLength(0);
});
it('starts a saved ready draft in one click after read-only inspection, and serializes repeated clicks',async()=>{
 const backend=setup();render(<TasksPage id={initial.id} info={info} graphs={graphs}/>);
 await screen.findByRole('region',{name:'Launch review'});
 const run=screen.getByRole('button',{name:'Run task'});
 await act(async()=>{fireEvent.click(run);fireEvent.click(run);});
 await waitFor(()=>expect(backend.find('POST','/api/v1/tasks/tsk_launch/start')).toHaveLength(1));
 expect(backend.find('PUT','/api/v1/tasks/tsk_launch')).toHaveLength(0);
});
it('stops for a changed scope or budget and requires another visible confirmation',async()=>{
 const backend=setup();render(<TasksPage id={initial.id} info={info} graphs={graphs}/>);
 await screen.findByRole('region',{name:'Launch review'});backend.setScope('changed');
 await userEvent.click(screen.getByRole('button',{name:'Run task'}));
 expect(backend.find('POST','/api/v1/tasks/tsk_launch/start')).toHaveLength(0);
 backend.setLimit(3_000_000);await userEvent.click(screen.getByRole('button',{name:'Run task'}));
 expect(backend.find('POST','/api/v1/tasks/tsk_launch/start')).toHaveLength(0);
 expect(within(screen.getByRole('region',{name:'Budget'})).getByText(/\$3.00/)).toBeTruthy();
 await userEvent.click(screen.getByRole('button',{name:'Run task'}));await waitFor(()=>expect(backend.find('POST','/api/v1/tasks/tsk_launch/start')).toHaveLength(1));
});
it.each([{saveFailure:true,message:'Save conflict'},{reviewFailure:true,message:'Review offline'},{authFailure:true,message:'Authorization offline'},{budgetFailure:true,message:'Budget offline'},{emptyScope:true,message:'Authorization review is incomplete'}])('retains input and never starts on $message',async options=>{
 const backend=setup(options);render(<TasksPage create info={info} graphs={graphs}/>);
 await userEvent.type(screen.getByLabelText('Request'),'My manual correction');await userEvent.selectOptions(screen.getByLabelText('Workflow'),'flow');
 await userEvent.click(screen.getByRole('button',{name:'Run task'}));
 await waitFor(()=>expect(screen.getAllByText(new RegExp(options.message)).length).toBeGreaterThan(0));
 expect(screen.getByLabelText('Request')).toHaveProperty('value','My manual correction');expect(backend.find('POST','/api/v1/tasks/tsk_launch/start')).toHaveLength(0);
});
it('does not start with stale Full consent or silently create an Ask record',async()=>{
 const backend=setup({stale:true});render(<TasksPage id={initial.id} info={info} graphs={graphs}/>);
 await screen.findByRole('region',{name:'Launch review'});
 await userEvent.click(screen.getByRole('button',{name:'Run task'}));
 expect(backend.find('POST','/api/v1/tasks/tsk_launch/start')).toHaveLength(0);
 expect(screen.getByText(/Saved authorization is stale/)).toBeTruthy();
 expect(backend.find('POST','/api/v1/tasks/tsk_launch/authorization')).toHaveLength(0);
});

it.each([{normalizedSave:true},{mismatchedReview:true}])('retains the visible request when an acknowledgment or review changes its input: %j',async options=>{
 const backend=setup(options);render(<TasksPage create info={info} graphs={graphs}/>);
 await userEvent.type(screen.getByLabelText('Request'),'My exact request');await userEvent.selectOptions(screen.getByLabelText('Workflow'),'flow');
 await userEvent.click(screen.getByRole('button',{name:'Run task'}));
 await screen.findByText(options.normalizedSave?/saved task settings differ/:/changed during inspection/);
 expect(screen.getByLabelText('Request')).toHaveProperty('value','My exact request');
 expect(screen.queryByRole('region',{name:'Launch review'})).toBeNull();
 expect(backend.find('POST','/api/v1/tasks/tsk_launch/start')).toHaveLength(0);
});
