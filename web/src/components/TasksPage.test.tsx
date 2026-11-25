import {act,cleanup,render,screen,waitFor,within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {TasksPage,taskOutcomes,needsModel} from './TasksPage';
import {fakeApi,apiError} from '../test/fakeApi';
import type {GraphSummary,Info} from '../api/types';
const graphs=[{graph:{metadata:{id:'factory',name:'Software Factory',version:1},spec:{nodes:[{id:'worker',name:'Worker',runtime:{kind:'echo'}}]}}}] as GraphSummary[];
const info={identity:{role:'operator'}} as Info;
const showModal=Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype,'showModal');
beforeEach(()=>Object.defineProperty(HTMLDialogElement.prototype,'showModal',{configurable:true,value(this:HTMLDialogElement){this.setAttribute('open','');}}));
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.unstubAllGlobals();if(showModal)Object.defineProperty(HTMLDialogElement.prototype,'showModal',showModal);else Reflect.deleteProperty(HTMLDialogElement.prototype,'showModal');});
it('offers distinct outcomes without an automatic target or permission choice',()=>{
 expect(new Set(taskOutcomes.map(o=>o.id)).size).toBe(5);
 fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks':()=>({items:[]})});
 render(<TasksPage create info={info} graphs={graphs}/>);
 expect((screen.getByLabelText('Workflow') as HTMLSelectElement).value).toBe('');
 expect((screen.getByLabelText('Entry agent') as HTMLSelectElement).value).toBe('');
 expect(screen.getByLabelText('Workflow').getAttribute('aria-invalid')).toBeNull();
 expect(screen.getByLabelText('Entry agent').getAttribute('aria-invalid')).toBeNull();
 expect(screen.queryByRole('button',{name:'Start'})).toBeNull();
});
it('preserves request on conflict and sends no tool grants',async()=>{
 const backend=fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks':()=>({items:[]}), 'POST /api/v1/tasks':()=>apiError(409,'Workflow changed','conflict')});
 render(<TasksPage create info={info} graphs={graphs}/>);
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
it('exposes configuration, derives an editable title and preserves errors',async()=>{
 const backend=fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks':()=>({items:[]}), 'POST /api/v1/tasks':()=>apiError(503,'Storage unavailable','unavailable')});
 render(<TasksPage create info={info} graphs={graphs}/>);
 expect(screen.getByLabelText('Project').closest('details')).toBeNull();
 expect(screen.getByLabelText('Workflow version').closest('details')).toBeNull();
 await userEvent.type(screen.getByLabelText('Request'),'Fix https://example.com/repo\nDo not push');
 await userEvent.selectOptions(screen.getByLabelText('Workflow'),'factory');
 await userEvent.click(screen.getByRole('button',{name:'Save draft'}));
 await screen.findByText('Storage unavailable');
 expect(backend.find('POST','/api/v1/tasks')[0].body).toMatchObject({title:'Fix https://example.com/repo',request:'Fix https://example.com/repo\nDo not push',nodeId:'worker'});
 expect(backend.calls.filter(c=>c.method!=='GET')).toHaveLength(1);
 expect(screen.getByLabelText('Title').closest('details')).toBeNull();
 await userEvent.clear(screen.getByLabelText('Title'));
 await userEvent.type(screen.getByLabelText('Title'),'My title');
 await userEvent.type(screen.getByLabelText('Request'),' and inspect tests');
 expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('My title');
 expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('My title');
});
it('shows project discovery errors with a retry beside visible repository selection',async()=>{
 fakeApi({'GET /api/v1/projects':()=>apiError(503,'Projects unavailable','unavailable'),'GET /api/v1/tasks':()=>({items:[]})});
 render(<TasksPage create info={info} graphs={graphs}/>);
 const error=await screen.findByText('Projects unavailable');
 expect(error.closest('details')).toBeNull();
 expect(screen.getByRole('button',{name:'Retry projects'})).toBeTruthy();
 expect(screen.getByLabelText('Project').closest('details')).toBeNull();
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
 expect(within(screen.getByRole('region',{name:'Save and review task'})).getByRole('alert').textContent).toContain('Submission outcome unavailable');
 expect(screen.getByRole('button',{name:'Run task'}).getAttribute('aria-busy')).toBe('false');
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
it('defaults the version and reports remaining missing configuration without reinterpreting a draft',async()=>{
 const draft={id:'tsk_feedback',revision:4,status:'draft',submitted:true,title:'Inspect',request:'Inspect dependencies',outcome:'freeform',graphId:'factory',graphVersion:0,nodeId:'',resolution:{state:'invalid_proposal',message:'Invalid proposal'}};
 const backend=fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks/tsk_feedback':()=>draft,
 'GET /api/v1/tasks/tsk_feedback/review':()=>({task:draft,blockers:['Invalid proposal']})});
 render(<TasksPage id="tsk_feedback" info={{identity:{role:'admin'}} as Info} graphs={graphs}/>);
 await userEvent.click(await screen.findByRole('button',{name:'Continue task'}));
 const actions=screen.getByRole('region',{name:'Save and review task'});
 expect(within(actions).getByRole('status').textContent).toContain('Task has not started. Complete entry agent');
 expect(screen.getByRole('combobox',{name:'Workflow version'})).toHaveProperty('value','1');
 expect(screen.getByLabelText('Workflow version').getAttribute('aria-invalid')).toBeNull();
 expect(screen.getByLabelText('Entry agent').getAttribute('aria-invalid')).toBe('true');
 await userEvent.click(within(actions).getByRole('button',{name:'Complete task setup'}));
 expect(document.activeElement).toBe(screen.getByRole('region',{name:'2. Workflow & repository'}));
 expect(screen.getByLabelText('Request')).toHaveProperty('value',draft.request);
 expect(backend.calls.filter(c=>c.method!=='GET')).toEqual([]);
 await userEvent.selectOptions(screen.getByLabelText('Entry agent'),'worker');
 expect(screen.getByLabelText('Entry agent').getAttribute('aria-invalid')).toBeNull();
 expect(within(actions).queryByRole('button',{name:'Complete task setup'})).toBeNull();
 expect(within(actions).getByRole('status').textContent).toContain('Unsaved changes');
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

it('shows historical spend without an active budget pause on a cancelled task',async()=>{
 const task={id:'tsk_r',revision:2,status:'cancel_requested',title:'Task',request:'Explain',outcome:'freeform',graphId:'factory',graphVersion:1,nodeId:'worker',resultNodeId:'worker',executionGraphId:'task-run-r'};
 const staleLedger={scope:'run',id:'task-run-r',currency:'USD',limitMicros:1_000_000,chargedMicros:1_000_000,estimatedMicros:0,reservedMicros:0,remainingMicros:0,reached:true,paused:true,notResumed:['task-run-r']};
 fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks/tsk_r':()=>task,
  'GET /api/v1/tasks/tsk_r/progress':()=>({task,state:'cancelled',blockers:[],executions:[],pendingApprovals:[]}),
  'GET /api/v1/cost/budgets/run/task-run-r':()=>staleLedger});
 render(<TasksPage id="tsk_r" info={{identity:{role:'admin'}} as Info} graphs={graphs}/>);
 await screen.findByText('Task status: cancelled');
 expect(await screen.findByText(/\$1.00 spent/)).toBeTruthy();
 expect(screen.queryByText(/paused because|Runs paused|Resume pending/)).toBeNull();
 expect(screen.queryByRole('button',{name:'Raise budget'})).toBeNull();
});

it('renders a tracked issue as text with separate replies and no link back to itself', async () => {
 const task={id:'tsk_issue',revision:2,status:'cancelled',title:'Repair task output',outcome:'freeform',graphId:'factory',graphVersion:2,nodeId:'worker',resultNodeId:'worker',
  request:JSON.stringify({notice:'Internal task context',issue:'example/repo#10',title:'Repair task output',body:'Repair this output.\n\nKeep <b>literal text</b>.',replies:['Existing issue reply.']})};
 fakeApi({'GET /api/v1/tasks/tsk_issue':()=>task,
  'GET /api/v1/tasks/tsk_issue/progress':()=>({task,state:'cancelled',blockers:[],executions:[],pendingApprovals:[]}),
  'GET /api/v1/tasks/tsk_issue/authorization':()=>apiError(409,'storage: conflict','conflict')});
 render(<TasksPage id="tsk_issue" info={info} graphs={graphs}/>);
 await screen.findByRole('heading',{level:1,name:'Repair task output'});
 await userEvent.click(screen.getByRole('link',{name:'Request'}));
 const request=screen.getByRole('region',{name:'Request'});
 expect(within(request).getByText('example/repo#10')).toBeTruthy();
 expect(request.textContent).toContain('Repair this output.\n\nKeep <b>literal text</b>.');
 expect(request.textContent).not.toContain('Internal task context');
 expect(within(request).getByText('Issue replies (1)')).toBeTruthy();
 await userEvent.click(screen.getByRole('link',{name:'Settings & access'}));
 expect(await screen.findByText(/Authorization review is out of date/)).toBeTruthy();
 expect(screen.queryByRole('link',{name:'Task details'})).toBeNull();
});
it('offers Same as entry when the result is omitted',async()=>{
 fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks':()=>({items:[]})});
 render(<TasksPage create info={info} graphs={graphs}/>);
 expect(within(screen.getByLabelText('Result agent')).getByRole('option',{name:'Same as entry'}).getAttribute('value')).toBe('');
});
it('shows a retained launch failure and the pending environment approval on a draft',async()=>{
 const draft={id:'tsk_launch',revision:2,status:'draft',title:'Issue task',request:'Fix parser',outcome:'implement',graphId:'factory',graphVersion:1,nodeId:'worker',resultNodeId:'worker',launchError:'Task environment preparation failed. Select an enabled result agent.',environmentApprovalId:'apr_env'};
 fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks':()=>({items:[draft]}),'GET /api/v1/tasks/tsk_launch':()=>draft});
 render(<TasksPage id="tsk_launch" info={info} graphs={graphs}/>);
 expect((await screen.findByRole('alert')).textContent).toBe(draft.launchError);
 expect(screen.getByRole('link',{name:'Approvals'}).getAttribute('href')).toBe('#/approvals');
 expect(screen.getByText(/The issue remains in progress/)).toBeTruthy();
});

it('repairs incomplete interpretation manually, preserving inputs and reviewing without another model submission',async()=>{
 const draft={id:'tsk_repair',revision:6,status:'draft',submitted:true,title:'Security task',request:'Inspect alerts without merging.',outcome:'freeform',graphId:'factory',graphVersion:0,nodeId:'',executionMode:'sandbox',projectId:'p',baseCommit:'a'.repeat(40),resolution:{state:'invalid_proposal',message:'The model returned an invalid proposal.'}};
 let stored:Record<string,unknown>={...draft};
 const backend=fakeApi({
  'GET /api/v1/providers':()=>[{status:'ready'}],
  'GET /api/v1/projects':()=>[{id:'p',name:'Repository',head:draft.baseCommit}],
  'GET /api/v1/tasks/tsk_repair':()=>stored,
  'PUT /api/v1/tasks/tsk_repair':c=>{stored={...c.body as object,id:draft.id,revision:7,status:'draft',submitted:true};return stored;},
  'GET /api/v1/tasks/tsk_repair/authorization':()=>({scope:'repaired',capabilities:[{id:'worker',isolation:'docker',grants:[]}]}),
  'GET /api/v1/tasks/tsk_repair/review':()=>({task:stored,currentGraphVersion:1,canStart:false,blockers:[],validation:{errors:[],warnings:[]},agents:[]}),
 });
 render(<TasksPage id="tsk_repair" info={{identity:{role:'admin'}} as Info} graphs={graphs}/>);
 await userEvent.click(await screen.findByRole('button',{name:'Configure task manually'}));
 expect(screen.getByLabelText('Workflow').closest('details')).toBeNull();
 expect(document.activeElement).toBe(screen.getByRole('region',{name:'2. Workflow & repository'}));
 expect(screen.queryByText(/different workflow version/)).toBeNull();
 expect(backend.find('GET','/api/v1/tasks/tsk_repair/authorization')).toHaveLength(0);
 expect(screen.queryByRole('article',{name:'Task requirements'})).toBeNull();
 expect(screen.queryByText(/Isolation: process/)).toBeNull();
 expect(screen.queryByText(/Current workflow version: 0/)).toBeNull();
 await userEvent.selectOptions(screen.getByRole('combobox',{name:'Workflow version'}),'1');
 await userEvent.selectOptions(screen.getByLabelText('Entry agent'),'worker');
 await userEvent.click(screen.getByRole('button',{name:'Save draft'}));
 await screen.findByText('Isolation: docker');
 expect(stored).toMatchObject({request:draft.request,title:draft.title,projectId:'p',baseCommit:draft.baseCommit,executionMode:'sandbox',graphVersion:1,nodeId:'worker'});
 await userEvent.click(screen.getByRole('button',{name:'Review and run'}));
 await screen.findByRole('article',{name:'Task requirements'});
 expect(backend.calls.filter(c=>c.method!=='GET').map(c=>c.path)).toEqual(['/api/v1/tasks/tsk_repair']);
 expect(backend.find('POST','/api/v1/tasks/submit')).toHaveLength(0);
 expect((screen.getByRole('button',{name:'Start task'}) as HTMLButtonElement).disabled).toBe(true);
});

it('ignores a late review of the incomplete revision after a manual repair is saved',async()=>{
 const draft={id:'tsk_late',revision:1,status:'draft',submitted:true,title:'Task',request:'Inspect alerts.',outcome:'freeform',graphId:'factory',graphVersion:0,nodeId:'',resolution:{state:'invalid_proposal',message:'Invalid proposal'}};
 let finish!:(value:unknown)=>void;
 const backend=fakeApi({
  'GET /api/v1/providers':()=>[{status:'ready'}], 'GET /api/v1/projects':()=>[],
  'GET /api/v1/tasks/tsk_late':()=>draft,
  'GET /api/v1/tasks/tsk_late/review':()=>new Promise(resolve=>{finish=resolve;}),
  'PUT /api/v1/tasks/tsk_late':c=>({...c.body as object,id:draft.id,revision:2,status:'draft'}),
  'GET /api/v1/tasks/tsk_late/authorization':()=>({scope:'new',capabilities:[]}),
 });
 render(<TasksPage id="tsk_late" info={{identity:{role:'admin'}} as Info} graphs={graphs}/>);
 await userEvent.click(await screen.findByRole('button',{name:'Configure task manually'}));
 await waitFor(()=>expect(backend.find('GET','/api/v1/tasks/tsk_late/review')).toHaveLength(1));
 await userEvent.selectOptions(screen.getByRole('combobox',{name:'Workflow version'}),'1');
 await userEvent.selectOptions(screen.getByLabelText('Entry agent'),'worker');
 await userEvent.click(screen.getByRole('button',{name:'Save draft'}));
 await screen.findByRole('button',{name:'Review and run'});
 await act(async()=>{finish({task:draft,currentGraphVersion:0,canStart:true,blockers:[],validation:{errors:[],warnings:[]},isolation:'process'});});
 await waitFor(()=>expect(screen.queryByRole('article',{name:'Task requirements'})).toBeNull());
 expect(screen.queryByRole('button',{name:'Start task'})).toBeNull();
 expect(screen.queryByText(/Isolation: process/)).toBeNull();
 expect(backend.calls.filter(c=>c.method!=='GET').map(c=>c.path)).toEqual(['/api/v1/tasks/tsk_late']);
});

it('keeps repository selection through discovery failure and retries with distinct snapshot labels',async()=>{
 let available=false;
 const draft={id:'tsk_project',revision:1,status:'draft',title:'Task',request:'Check repo',outcome:'freeform',graphId:'factory',graphVersion:1,nodeId:'worker',projectId:'second',baseCommit:'b'.repeat(40)};
 const backend=fakeApi({'GET /api/v1/tasks/tsk_project':()=>draft,'GET /api/v1/projects':()=>available?[
  {id:'first',name:'weft.git',head:'a'.repeat(40),repositoryUrl:'https://example.com/repo.git',repositoryRef:'refs/heads/main'},
  {id:'second',name:'weft.git',head:draft.baseCommit,repositoryUrl:'https://example.com/repo.git',repositoryRef:'refs/heads/security'}
 ]:apiError(503,'Projects unavailable','unavailable')});
 render(<TasksPage id="tsk_project" info={info} graphs={graphs}/>);
 await screen.findByText('Projects unavailable');
 expect((screen.getByLabelText('Project') as HTMLSelectElement).value).toBe('second');
 available=true;
 await userEvent.click(screen.getByRole('button',{name:'Retry projects'}));
 const choices=await screen.findAllByRole('option',{name:/weft.git/});
 expect(choices.map(c=>c.textContent)).toEqual([
  'weft.git · https://example.com/repo.git · refs/heads/main · aaaaaaaaaaaa',
  'weft.git · https://example.com/repo.git · refs/heads/security · bbbbbbbbbbbb'
 ]);
 expect((screen.getByLabelText('Project') as HTMLSelectElement).value).toBe('second');
 expect(backend.calls.filter(c=>c.method!=='GET')).toHaveLength(0);
});

it('saves visible configuration together and places execution after the selected repository',async()=>{
 const backend=fakeApi({'GET /api/v1/projects':()=>[{id:'p',name:'Repo',head:'a'.repeat(40),path:'/repo',executionMode:'sandbox'}],
 'POST /api/v1/tasks':c=>({...c.body as object,id:'tsk_visible',revision:1,status:'draft'})});
 render(<TasksPage create info={info} graphs={graphs}/>);
 expect(screen.queryByText('Review settings')).toBeNull();
 await userEvent.type(screen.getByLabelText('Request'),'Inspect dependencies');
 await userEvent.selectOptions(screen.getByLabelText('Workflow'),'factory');
 await userEvent.selectOptions(screen.getByLabelText('Project'),'p');
 expect((screen.getByLabelText('Execution environment') as HTMLSelectElement).value).toBe('sandbox');
 expect(screen.getByLabelText('Workflow version')).toHaveProperty('value','1');
 const form=screen.getByRole('form',{name:'Task request'});
 const order=[...form.querySelectorAll('select,button')];
 expect(order.indexOf(screen.getByLabelText('Project'))).toBeLessThan(order.indexOf(screen.getByRole('button',{name:'Run task'})));
 await userEvent.click(screen.getByRole('button',{name:'Save draft'}));
 await waitFor(()=>expect(backend.find('POST','/api/v1/tasks')).toHaveLength(1));
 expect(backend.find('POST','/api/v1/tasks')[0].body).toMatchObject({graphId:'factory',graphVersion:1,nodeId:'worker',projectId:'p',baseCommit:'a'.repeat(40),executionMode:'sandbox',request:'Inspect dependencies'});
 expect(backend.find('POST','/api/v1/tasks/submit')).toHaveLength(0);
});

it('deletes only the saved draft after confirmation and returns to Tasks without submitting edits',async()=>{
 const draft={id:'tsk_deletepage',revision:3,status:'draft',title:'Saved draft',request:'Inspect',outcome:'freeform',graphId:'factory',graphVersion:1,nodeId:'worker'};
 let failure=true;
 const backend=fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks/tsk_deletepage':()=>draft,
 'DELETE /api/v1/tasks/tsk_deletepage':()=>failure?apiError(409,'Changed','conflict'):new Response(null,{status:204})});
 window.location.hash='#/tasks/tsk_deletepage';
 render(<TasksPage id="tsk_deletepage" info={info} graphs={graphs}/>);
 await userEvent.click(await screen.findByRole('button',{name:'Delete draft'}));
 await userEvent.click(screen.getByRole('button',{name:'Cancel'}));
 expect(backend.find('DELETE','/api/v1/tasks/tsk_deletepage')).toHaveLength(0);
 await userEvent.type(screen.getByLabelText('Request'),' unsaved edit');
 await userEvent.click(screen.getByRole('button',{name:'Delete draft'}));
 await userEvent.click(screen.getByRole('button',{name:'Delete draft permanently'}));
 await screen.findByRole('alert');
 expect(window.location.hash).toBe('#/tasks/tsk_deletepage');
 expect(screen.getByLabelText('Request')).toHaveProperty('value','Inspect unsaved edit');
 failure=false;
 await userEvent.click(screen.getByRole('button',{name:'Delete draft permanently'}));
 await waitFor(()=>expect(window.location.hash).toBe('#/tasks'));
 expect(backend.calls.filter(c=>c.method!=='GET').map(c=>c.method)).toEqual(['DELETE','DELETE']);
 expect(backend.find('DELETE','/api/v1/tasks/tsk_deletepage')[1].headers['If-Match']).toBe('3');
});

it('continues corrected bindings without model resolution and retains the saved revision after review failure',async()=>{
 const draft={id:'tsk_continue',revision:4,status:'draft',submitted:true,scmConnection:'delivery',resultNodeId:'worker',channelId:'requests',messageType:'security.request',payloadJson:'{"custom":true}',title:'Original',request:'fixture-invalid',outcome:'freeform',graphId:'factory',graphVersion:0,nodeId:'',resolution:{state:'invalid_proposal',message:'Invalid proposal'}};
 let stored:Record<string,unknown>={...draft};
 let failSave=true,failReview=true;
 let finishSave!:(v:unknown)=>void;
 const backend=fakeApi({
 'GET /api/v1/projects':()=>[{id:'p',name:'Repository',head:'a'.repeat(40)}],
 'GET /api/v1/tasks/tsk_continue':()=>stored,
 'GET /api/v1/scm/connections':()=>[{name:'delivery',repository:'owner/repo'}],
 'GET /api/v1/tasks/tsk_continue/review':()=>failReview?apiError(503,'Review unavailable','unavailable'):{task:stored,currentGraphVersion:1,blockers:['Confirm authorization'],validation:{errors:[],warnings:[]}},
 'GET /api/v1/tasks/tsk_continue/authorization':()=>({scope:'saved',capabilities:[]}),
 'PUT /api/v1/tasks/tsk_continue':c=>failSave?apiError(409,'Draft changed','conflict'):new Promise(resolve=>{finishSave=v=>{stored={...c.body as object,id:draft.id,revision:5,status:'draft',submitted:true};resolve(v??stored);};}),
 });
 render(<TasksPage id="tsk_continue" info={{identity:{role:'admin'}} as Info} graphs={graphs}/>);
 await userEvent.selectOptions(await screen.findByLabelText('Entry agent'),'worker');
 await userEvent.selectOptions(screen.getByLabelText('Project'),'p');
 await userEvent.selectOptions(screen.getByLabelText('Execution environment'),'sandbox');
 await userEvent.clear(screen.getByLabelText('Title'));await userEvent.type(screen.getByLabelText('Title'),'Corrected');
 await userEvent.click(screen.getByRole('button',{name:'Continue task'}));
 await screen.findByText('Draft changed');
 expect(screen.getByLabelText('Title')).toHaveProperty('value','Corrected');
 expect(screen.getByLabelText('Entry agent')).toHaveProperty('value','worker');
 expect(stored.revision).toBe(4);
 failSave=false;
 await userEvent.click(screen.getByRole('button',{name:'Continue task'}));
 await waitFor(()=>expect(finishSave).toBeTypeOf('function'));
 expect(screen.getByRole('button',{name:'Continuing task…'})).toHaveProperty('disabled',true);
 expect(screen.getByRole('button',{name:'Continuing task…'}).getAttribute('aria-busy')).toBe('true');
 await act(async()=>finishSave(undefined));
 await screen.findByText('Review unavailable');
 expect(stored).toMatchObject({revision:5,title:'Corrected',request:'fixture-invalid',graphVersion:1,nodeId:'worker',projectId:'p',baseCommit:'a'.repeat(40),executionMode:'sandbox',scmConnection:'delivery',resultNodeId:'worker',channelId:'requests',messageType:'security.request',payloadJson:'{"custom":true}'});
 expect(stored).not.toHaveProperty('resolution');expect(stored).not.toHaveProperty('status','running');
 failReview=false;
 await userEvent.click(screen.getByRole('button',{name:'Review and run'}));
 await screen.findByRole('article',{name:'Task requirements'});
 expect(backend.find('PUT','/api/v1/tasks/tsk_continue')).toHaveLength(2);
 expect(backend.find('PUT','/api/v1/tasks/tsk_continue')[1].headers['If-Match']).toBe('4');
 expect(backend.find('POST','/api/v1/tasks/submit')).toHaveLength(0);
 expect(screen.getByRole('button',{name:'Start task'})).toHaveProperty('disabled',true);
 expect(screen.getByLabelText('Title')).toHaveProperty('value','Corrected');
});

it('retains a saved version until the operator chooses the current version and preserves it on conflict',async()=>{
 const draft={id:'tsk_version',revision:2,status:'draft',title:'Pinned',request:'Inspect',outcome:'freeform',graphId:'factory',graphVersion:1,nodeId:'worker'};
 const updated=[{graph:{...graphs[0].graph,metadata:{...graphs[0].graph.metadata,version:2}}}] as GraphSummary[];
 const backend=fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks/tsk_version':()=>draft,'PUT /api/v1/tasks/tsk_version':()=>apiError(409,'Workflow changed','conflict')});
 render(<TasksPage id="tsk_version" info={{identity:{role:'admin'}} as Info} graphs={updated}/>);
 const version=await screen.findByRole('combobox',{name:'Workflow version'});
 expect(version).toHaveProperty('value','1');
 expect(screen.getByRole('option',{name:'Version 1 (saved)'})).toBeTruthy();
 await userEvent.selectOptions(version,'2');
 await userEvent.click(screen.getByRole('button',{name:'Review and run'}));
 await screen.findByText('Workflow changed');
 expect(version).toHaveProperty('value','2');
 expect(backend.find('PUT','/api/v1/tasks/tsk_version')[0].body).toMatchObject({graphVersion:2});
 expect(backend.find('POST','/api/v1/tasks/submit')).toHaveLength(0);
});

it('defaults an unselected version after workflow loading and never follows later publication silently',async()=>{
 const draft={id:'tsk_loading',revision:1,status:'draft',title:'Task',request:'Inspect',outcome:'freeform',graphId:'factory',graphVersion:0,nodeId:''};
 fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks/tsk_loading':()=>draft});
 const {rerender}=render(<TasksPage id="tsk_loading" info={info} graphs={[]}/>);
 const version=await screen.findByRole('combobox',{name:'Workflow version'});
 expect(version).toHaveProperty('disabled',true);
 rerender(<TasksPage id="tsk_loading" info={info} graphs={graphs}/>);
 await waitFor(()=>expect(version).toHaveProperty('value','1'));
 rerender(<TasksPage id="tsk_loading" info={info} graphs={[{...graphs[0],graph:{...graphs[0].graph,metadata:{...graphs[0].graph.metadata,version:2}}}]}/>);
 expect(version).toHaveProperty('value','1');
 expect(screen.getByRole('option',{name:'Current workflow version (Version 2)'})).toBeTruthy();
});

it('explains environment preparation beside the action and focuses its reviewed build without starting',async()=>{
 const draft={id:'tsk_build_ux',revision:4,status:'draft',title:'Build task',request:'Inspect dependencies',outcome:'freeform',graphId:'factory',graphVersion:1,nodeId:'worker'};
 const raw='spec.environment: environment not ready: it has not been built yet; build it in Graph settings → Environment (needed by worker)';
 const review={task:draft,currentGraphVersion:1,canStart:false,canPrepareEnvironment:true,environmentDefinition:{image:'alpine:3.22'},blockers:[raw,'Task authorization is stale.'],validation:{errors:[{path:'spec.environment',message:raw.slice('spec.environment: '.length)}],warnings:[{path:'spec.environment',message:'environment not ready: warning fixture'},{path:'spec.nodes',message:'Review optional agent setting'}]}};
 let fail!:(error:Error)=>void;
 const backend=fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks/tsk_build_ux':()=>draft,
 'GET /api/v1/tasks/tsk_build_ux/authorization':()=>({scope:'fixture',capabilities:[]}),
 'GET /api/v1/tasks/tsk_build_ux/review':()=>review,
 'POST /api/v1/tasks/tsk_build_ux/prepare-environment':()=>new Promise((_,reject)=>{fail=reject;})});
 render(<TasksPage id={draft.id} info={{identity:{role:'admin'}} as Info} graphs={graphs}/>);
 await userEvent.click(await screen.findByRole('button',{name:'Review and run'}));
 const notice=screen.getByRole('complementary',{name:'Environment preparation required'});
 expect(notice.textContent).toContain('You can prepare it from this page');
 expect(notice.textContent).toContain('Approve the build in Approvals if requested');
 expect(notice.textContent).not.toContain('Graph settings');
 expect(screen.getByRole('region',{name:'Save and review task'}).textContent).toContain('Review and prepare its Docker environment here');
 expect(screen.getByText('Task authorization is stale.',{selector:'article > p'})).toBeTruthy();
 expect(screen.getAllByText(raw).every(text=>text.closest('details')?.open===false)).toBe(true);
 expect(screen.getByText('spec.environment: environment not ready: warning fixture').closest('details')).toHaveProperty('open',false);
 expect(screen.getByText('spec.nodes: Review optional agent setting',{selector:'article > p'})).toBeTruthy();
 await userEvent.click(within(notice).getByRole('button',{name:'Review environment preparation'}));
 const preparation=screen.getByRole('region',{name:'Review task environment preparation'});
 expect(document.activeElement).toBe(preparation);
 expect(preparation.textContent).toContain('Preparation does not start agents.');
 expect(preparation.textContent).toContain('alpine:3.22');
 expect(screen.getByRole('button',{name:'Prepare task environment'})).toHaveProperty('disabled',true);
 expect(screen.getByRole('button',{name:'Start task'})).toHaveProperty('disabled',true);
 expect(backend.calls.filter(c=>c.method!=='GET')).toEqual([]);
 await userEvent.click(within(preparation).getByRole('checkbox'));
 await userEvent.click(screen.getByRole('button',{name:'Prepare task environment'}));
 expect(screen.getByRole('button',{name:'Preparing environment…'}).getAttribute('aria-busy')).toBe('true');
 expect(within(preparation).getByRole('status').textContent).toContain('No agents are starting.');
 await act(async()=>fail(new Error('Build service unavailable')));
 await screen.findByText('network error: Build service unavailable');
 expect(screen.getByLabelText('Request')).toHaveProperty('value',draft.request);
 expect(screen.getByRole('button',{name:'Start task'})).toHaveProperty('disabled',true);
 expect(backend.find('POST','/api/v1/tasks/tsk_build_ux/prepare-environment')).toHaveLength(1);
 expect(backend.find('POST','/api/v1/tasks/tsk_build_ux/start')).toHaveLength(0);
});

it('shows the preparation path to an operator while leaving the build administrator-only',async()=>{
 const draft={id:'tsk_operator_build',revision:1,status:'draft',title:'Task',request:'Inspect',outcome:'freeform',graphId:'factory',graphVersion:1,nodeId:'worker'};
 const backend=fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/tasks/tsk_operator_build':()=>draft,
 'GET /api/v1/tasks/tsk_operator_build/authorization':()=>({scope:'fixture',capabilities:[]}),
 'GET /api/v1/tasks/tsk_operator_build/review':()=>({task:draft,canStart:false,canPrepareEnvironment:true,blockers:[],validation:{errors:[],warnings:[]}})});
 render(<TasksPage id={draft.id} info={info} graphs={graphs}/>);
 await userEvent.click(await screen.findByRole('button',{name:'Review requirements'}));
 await userEvent.click(screen.getByRole('button',{name:'Review environment preparation'}));
 expect(screen.getByText('An administrator must prepare this task environment.')).toBeTruthy();
 await userEvent.click(within(screen.getByRole('region',{name:'Review task environment preparation'})).getByRole('checkbox'));
 expect(screen.getByRole('button',{name:'Prepare task environment'})).toHaveProperty('disabled',true);
 expect(backend.calls.filter(c=>c.method!=='GET')).toEqual([]);
});
