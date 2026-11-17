import {cleanup,render,screen,waitFor,within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it,vi} from 'vitest';
import {TasksPage,taskOutcomes,needsModel} from './TasksPage';
import {fakeApi,apiError} from '../test/fakeApi';
import type {GraphSummary,Info} from '../api/types';
const graphs=[{graph:{metadata:{id:'factory',name:'Software Factory',version:1},spec:{nodes:[{id:'worker',name:'Worker',runtime:{kind:'echo'}}]}}}] as GraphSummary[];
const info={identity:{role:'operator'}} as Info;
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.unstubAllGlobals();});
it('offers distinct outcomes without an automatic target or permission choice',()=>{
 expect(new Set(taskOutcomes.map(o=>o.id)).size).toBe(5);
 fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks':()=>({items:[]})});
 render(<TasksPage create info={info} graphs={graphs}/>);
 expect((screen.getByLabelText('Workflow') as HTMLSelectElement).value).toBe('');
 expect((screen.getByLabelText('Entry agent') as HTMLSelectElement).value).toBe('');
 expect(screen.queryByRole('button',{name:'Start'})).toBeNull();
});
it('preserves request on conflict and sends no tool grants',async()=>{
 const backend=fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks':()=>({items:[]}), 'POST /api/v1/tasks':()=>apiError(409,'Workflow changed','conflict')});
 render(<TasksPage create info={info} graphs={graphs}/>);
 await userEvent.click(screen.getByText('Review settings'));
 await userEvent.type(screen.getByLabelText('Title'),'Investigate a bug');
 await userEvent.type(screen.getByLabelText('Request'),'Check the parser');
 await userEvent.selectOptions(screen.getByLabelText('Workflow'),'factory');
 await userEvent.selectOptions(screen.getByLabelText('Entry agent'),'worker');
 await userEvent.click(screen.getByRole('button',{name:'Save draft'}));
 await screen.findByText('Workflow changed');
 expect((screen.getByLabelText('Request') as HTMLTextAreaElement).value).toBe('Check the parser');
 expect(backend.find('POST','/api/v1/tasks')[0].body).toEqual({title:'Investigate a bug',request:'Check the parser',outcome:'freeform',graphId:'factory',graphVersion:1,nodeId:'worker',resultNodeId:'worker'});
 expect(backend.calls.filter(c=>c.method!=='GET')).toHaveLength(1);
});
it('renders viewer fields read-only and reports loading failures',async()=>{
 fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks':()=>apiError(503,'Task storage unavailable','unavailable')});
 const {unmount}=render(<TasksPage info={{identity:{role:'viewer'}} as Info} graphs={graphs}/>);
 await screen.findByText('Task storage unavailable');
 expect(screen.queryByRole('link',{name:'New task'})).toBeNull();
 unmount();render(<TasksPage create info={{identity:{role:'viewer'}} as Info} graphs={graphs}/>);
 expect((screen.getByRole('button',{name:'Save draft'}) as HTMLButtonElement).disabled).toBe(true);
 await waitFor(()=>expect(screen.getByLabelText('Title').closest('fieldset')?.disabled).toBe(true));
});
it('requires reviewed confirmation and admin authority before starting',async()=>{
 const draft={id:'tsk_a',revision:1,status:'draft',title:'Task',request:'Explain',outcome:'freeform',graphId:'factory',graphVersion:1,nodeId:'worker',resultNodeId:'worker'};
 const backend=fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks':()=>({items:[draft]}),'GET /api/v1/tasks/tsk_a':()=>draft,'GET /api/v1/tasks/tsk_a/review':()=>({task:draft,canStart:true,blockers:[],validation:{errors:[],warnings:[]},executionGraphId:'task-run-a',grants:[],model:{},isolation:'process'})});
 render(<TasksPage id="tsk_a" info={info} graphs={graphs}/>);
 await userEvent.click(await screen.findByRole('button',{name:'Review requirements'}));
 await userEvent.click(await screen.findByRole('checkbox'));
 expect((screen.getByRole('button',{name:'Start task'}) as HTMLButtonElement).disabled).toBe(true);
 expect(backend.calls.filter(c=>c.method!=='GET')).toHaveLength(0);
});
it('starts with a prompt and collapsed settings, derives an editable title and preserves errors',async()=>{
 const backend=fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks':()=>({items:[]}), 'POST /api/v1/tasks':()=>apiError(503,'Storage unavailable','unavailable')});
 render(<TasksPage create info={info} graphs={graphs}/>);
 const settings=screen.getByText('Review settings').closest('details')!;
 expect(settings.open).toBe(false);
 await userEvent.type(screen.getByLabelText('Request'),'Fix https://example.com/repo\nDo not push');
 await userEvent.selectOptions(screen.getByLabelText('Workflow'),'factory');
 await userEvent.click(screen.getByRole('button',{name:'Save draft'}));
 await screen.findByText('Storage unavailable');
 expect(backend.find('POST','/api/v1/tasks')[0].body).toMatchObject({title:'Fix https://example.com/repo',request:'Fix https://example.com/repo\nDo not push',nodeId:'worker'});
 expect(backend.calls.filter(c=>c.method!=='GET')).toHaveLength(1);
 expect(settings.open).toBe(false);
 await userEvent.click(screen.getByText('Review settings'));
 await userEvent.clear(screen.getByLabelText('Title'));
 await userEvent.type(screen.getByLabelText('Title'),'My title');
 await userEvent.type(screen.getByLabelText('Request'),' and inspect tests');
 expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('My title');
 await userEvent.click(screen.getByText('Review settings'));
 await userEvent.click(screen.getByText('Review settings'));
 expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('My title');
});
it('shows project-loading errors outside collapsed advanced settings',async()=>{
 fakeApi({'GET /api/v1/projects':()=>apiError(503,'Projects unavailable','unavailable'),'GET /api/v1/tasks':()=>({items:[]})});
 render(<TasksPage create info={info} graphs={graphs}/>);
 const error=await screen.findByText('Projects unavailable');
 expect(error.closest('details')).toBeNull();
 expect(screen.getByText('Review settings').closest('details')?.open).toBe(false);
});
it('resolves a prompt through the client and exposes durable repair without granting or starting',async()=>{
 const draft={id:'tsk_prompt',revision:2,status:'draft',title:'Explain',request:'Explain',outcome:'freeform',graphId:'',graphVersion:0,nodeId:'',resolution:{state:'needs_model',message:'Configure a default model and resolve again.'}};
 const backend=fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks':()=>({items:[]}), 'POST /api/v1/tasks/prompt':()=>draft});
 render(<TasksPage create info={info} graphs={graphs}/>);
 await userEvent.type(screen.getByLabelText('Request'),'Explain');
 await userEvent.click(screen.getByRole('button',{name:'Resolve request'}));
 await screen.findByText('Configure a default model and resolve again.');
 expect(screen.getByRole('link',{name:'Configure models'}).getAttribute('href')).toBe('#/models');
 expect(backend.find('POST','/api/v1/tasks/prompt')[0].body).toEqual({request:'Explain',graphId:'',settings:{title:'Explain'}});
 expect(backend.calls.filter(c=>c.method!=='GET')).toHaveLength(1);
 expect((screen.getByLabelText('Request') as HTMLTextAreaElement).value).toBe('Explain');
});
it('keeps the prompt editable after interpretation transport failure',async()=>{
 fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks':()=>({items:[]}), 'POST /api/v1/tasks/prompt':()=>apiError(503,'Unavailable','unavailable')});
 render(<TasksPage create info={info} graphs={graphs}/>);
 await userEvent.type(screen.getByLabelText('Request'),'Keep original');
 await userEvent.click(screen.getByRole('button',{name:'Resolve request'}));
 await screen.findByText('Unavailable');
 expect((screen.getByLabelText('Request') as HTMLTextAreaElement).value).toBe('Keep original');
 expect((screen.getByRole('button',{name:'Resolve request'}) as HTMLButtonElement).disabled).toBe(false);
});
it('shows the repair reason for an unresolved draft in the task list',async()=>{
 fakeApi({'GET /api/v1/tasks':()=>({items:[{id:'tsk_pending',title:'Saved request',status:'draft',graphId:'',graphVersion:0,resolution:{state:'pending',message:'Request saved. Resolve to continue.'}}]})});
 render(<TasksPage info={info} graphs={graphs}/>);
 await screen.findByText('Request saved. Resolve to continue.');
 expect(screen.queryByText(/Workflow.*Version 0/)).toBeNull();
});
it('submits a prompt with a stable replay key and explicit authority, preserving input on failure',async()=>{
 const backend=fakeApi({'GET /api/v1/projects':()=>[],'POST /api/v1/tasks/submit':()=>apiError(503,'Submission outcome unavailable','unavailable')});
 render(<TasksPage create info={{identity:{role:'admin'}} as Info} graphs={graphs}/>);
 await userEvent.type(screen.getByLabelText('Request'),'Explain the parser');await userEvent.selectOptions(screen.getByLabelText('Workflow'),'factory');
 await userEvent.click(screen.getByRole('button',{name:'Run task'}));await screen.findByText('Submission outcome unavailable');
 await userEvent.click(screen.getByRole('button',{name:'Run task'}));
 await waitFor(()=>expect(backend.find('POST','/api/v1/tasks/submit')).toHaveLength(2));
 const requests=backend.find('POST','/api/v1/tasks/submit').map(c=>c.body as {key:string;confirm:boolean;request:string});
 expect(requests[0]).toEqual(requests[1]);expect(requests[0].confirm).toBe(true);expect(requests[0].request).toBe('Explain the parser');
 expect((screen.getByLabelText('Request') as HTMLTextAreaElement).value).toBe('Explain the parser');
});
it('keeps prompt execution administrator-only',()=>{
 fakeApi({'GET /api/v1/projects':()=>[]});render(<TasksPage create info={info} graphs={graphs}/>);
 expect((screen.getByRole('button',{name:'Run task'}) as HTMLButtonElement).disabled).toBe(true);
});
it('explains a missing model provider and blocks running only workflows that need a model',async()=>{
 const ready={name:'local',type:'ollama',status:'ready',default:true,source:'managed',editable:true,credential:{configured:false,source:'none'}};
 let providers:unknown[]=[];
 fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks':()=>({items:[]}),'GET /api/v1/providers':()=>providers});
 const admin={identity:{role:'admin'}} as Info;
 const modelGraphs=[...graphs,{graph:{metadata:{id:'factory-native',name:'Native factory',version:2},spec:{nodes:[{id:'planner',name:'Planner'}]}}}] as GraphSummary[];
 expect(needsModel(graphs[0].graph)).toBe(false);
 expect(needsModel(modelGraphs[1].graph)).toBe(true);
 const list=render(<TasksPage info={admin} graphs={modelGraphs}/>);
 const note=await screen.findByRole('note',{name:'No model provider'});
 expect(note.querySelector('a[href="#/models"]')).toBeTruthy();
 expect(screen.getByRole('link',{name:'New task'})).toBeTruthy(); // echo workflows still run
 list.unmount();

 render(<TasksPage create info={admin} graphs={modelGraphs}/>);
 await userEvent.type(screen.getByLabelText('Request'),'Plan the change');
 await userEvent.click(screen.getByText('Review settings'));
 await userEvent.selectOptions(screen.getByLabelText('Workflow'),'factory-native');
 expect(await screen.findByRole('note',{name:'No model provider'})).toBeTruthy();
 expect((screen.getByRole('button',{name:'Run task'}) as HTMLButtonElement).disabled).toBe(true);
 expect((screen.getByRole('button',{name:'Save draft'}) as HTMLButtonElement).disabled).toBe(false);
 // An echo-only workflow is not blocked.
 await userEvent.selectOptions(screen.getByLabelText('Workflow'),'factory');
 await waitFor(()=>expect(screen.queryByRole('note',{name:'No model provider'})).toBeNull());
 expect((screen.getByRole('button',{name:'Run task'}) as HTMLButtonElement).disabled).toBe(false);
 cleanup();

 // An unavailable provider does not count; a ready one clears the notice.
 providers=[{...ready,status:'unavailable'}];
 const again=render(<TasksPage info={admin} graphs={modelGraphs}/>);
 expect(await screen.findByRole('note',{name:'No model provider'})).toBeTruthy();
 again.unmount();
 providers=[ready];
 render(<TasksPage info={admin} graphs={modelGraphs}/>);
 await screen.findByRole('link',{name:'New task'});
 await waitFor(()=>expect(screen.queryByRole('note',{name:'No model provider'})).toBeNull());
});
it('does not block tasks when the provider list cannot be loaded',async()=>{
 fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks':()=>({items:[]}),'GET /api/v1/providers':()=>apiError(503,'unavailable','unavailable')});
 render(<TasksPage info={{identity:{role:'admin'}} as Info} graphs={graphs}/>);
 expect(await screen.findByRole('link',{name:'New task'})).toBeTruthy();
 expect(screen.queryByRole('note',{name:'No model provider'})).toBeNull();
});
it('shows the budgets a new task is held to before it runs',async()=>{
 const backend=fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks':()=>({items:[]}),'GET /api/v1/cost/budgets/preview':()=>({taskRunBudgetMicros:2_000_000,currency:'USD',budgets:[{scope:'user',id:'operator@2026-11',currency:'USD',limitMicros:10_000_000,chargedMicros:4_000_000,estimatedMicros:0,reservedMicros:0,remainingMicros:6_000_000,reached:false,paused:false}]})});
 render(<TasksPage create info={info} graphs={graphs}/>);
 await userEvent.click(screen.getByText('Review settings'));
 await userEvent.selectOptions(screen.getByLabelText('Workflow'),'factory');
 const budget=await screen.findByRole('region',{name:'Budget'});
 expect(budget.textContent).toContain('This run may spend up to $2.00');
 expect(budget.textContent).toContain('User operator · 2026-11: $6.00 left of $10.00');
 expect(backend.find('GET','/api/v1/cost/budgets/preview').at(-1)?.path).toBe('/api/v1/cost/budgets/preview');
});
it('explains a run paused by its budget and lets an administrator raise it',async()=>{
 const task={id:'tsk_r',revision:2,status:'running',title:'Task',request:'Explain',outcome:'freeform',graphId:'factory',graphVersion:1,nodeId:'worker',resultNodeId:'worker',executionGraphId:'task-run-r'};
 const paused={scope:'run',id:'task-run-r',currency:'USD',limitMicros:1_000_000,chargedMicros:900_000,estimatedMicros:0,reservedMicros:0,remainingMicros:100_000,reached:true,paused:true};
 let current=paused;
 const backend=fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks/tsk_r':()=>task,'GET /api/v1/tasks/tsk_r/progress':()=>({task,state:'running',blockers:[],executions:[],pendingApprovals:[]}),
  'GET /api/v1/cost/budgets/run/task-run-r':()=>current,
  'POST /api/v1/cost/budgets/run/task-run-r/raise':()=>{current={...paused,limitMicros:5_000_000,remainingMicros:4_100_000,reached:false,paused:false};return current;}});
 vi.stubGlobal('prompt',()=>'5');
 const view=render(<TasksPage id="tsk_r" info={{identity:{role:'operator'}} as Info} graphs={graphs}/>);
 expect((await screen.findByText(/paused because it reached its budget/)).textContent).toMatch(/Ask an administrator/);
 expect(screen.queryByRole('button',{name:'Raise budget'})).toBeNull();
 view.unmount();
 render(<TasksPage id="tsk_r" info={{identity:{role:'admin'}} as Info} graphs={graphs}/>);
 await userEvent.click(await screen.findByRole('button',{name:'Raise budget'}));
 await waitFor(()=>expect(backend.find('POST','/api/v1/cost/budgets/run/task-run-r/raise')[0]?.body).toEqual({limitMicros:5_000_000}));
 await waitFor(()=>expect(screen.queryByText(/paused because it reached its budget/)).toBeNull());
});
it('chooses a source control hand-off and shows what it delivered',async()=>{
 const backend=fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks':()=>({items:[]}),'GET /api/v1/scm/connections':()=>[{name:'gh',kind:'github',repository:'acme/app',tokenConfigured:true}],'POST /api/v1/tasks':(c)=>({...(c.body as object),id:'tsk_n',revision:1,status:'draft'})});
 render(<TasksPage create info={info} graphs={graphs}/>);
 await userEvent.click(screen.getByText('Review settings'));
 await userEvent.type(screen.getByLabelText('Title'),'Fix');
 await userEvent.type(screen.getByLabelText('Request'),'Fix it');
 await userEvent.selectOptions(screen.getByLabelText('Workflow'),'factory');
 await userEvent.selectOptions(screen.getByLabelText('Entry agent'),'worker');
 await userEvent.selectOptions(await screen.findByLabelText(/^Source control hand-off/),'gh');
 await userEvent.click(screen.getByRole('button',{name:'Save draft'}));
 await waitFor(()=>expect(backend.find('POST','/api/v1/tasks')[0]?.body).toMatchObject({scmConnection:'gh'}));
 cleanup();
 const task={id:'tsk_d',revision:2,status:'running',title:'Task',request:'Explain',outcome:'freeform',graphId:'factory',graphVersion:1,nodeId:'worker',resultNodeId:'worker',executionGraphId:'task-run-d',scmConnection:'gh'};
 fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks/tsk_d':()=>task,'GET /api/v1/tasks/tsk_d/progress':()=>({task,state:'running',blockers:[],executions:[],pendingApprovals:[],delivery:{connection:'gh',branch:'weft/task-d',repository:'acme/app',pullRequestUrl:'https://github.example/pr/4',pullRequestNumber:4,failures:['scm.pull_request.create rejected: not approved']}})});
 render(<TasksPage id="tsk_d" info={info} graphs={graphs}/>);
 const delivery=await screen.findByRole('region',{name:'Source control delivery'});
 expect(delivery.textContent).toContain('Pushed weft/task-d to acme/app (gh).');
 expect(within(delivery).getByRole('link',{name:'#4'}).getAttribute('href')).toBe('https://github.example/pr/4');
 expect(within(delivery).getByRole('alert',{name:'Delivery failures'}).textContent).toContain('not approved');
});
