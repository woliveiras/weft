import {cleanup,render,screen,waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it,vi} from 'vitest';
import {canRunPreparedTask,TaskEnvironment} from './TaskEnvironment';
import {fakeApi as routeApi,apiError} from '../test/fakeApi';
import type {TaskAuthorization,TaskDraft,TaskReview} from '../api/tasks';
import type {BudgetView,EnvironmentView} from '../api/types';
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.unstubAllGlobals();});
const task={id:'t',revision:4,status:'environment_pending',graphId:'workflow',graphVersion:1,nodeId:'worker'} as TaskDraft;
const ready={state:'ready',upToDate:true} as EnvironmentView;
const authorization={scope:'current'} as TaskAuthorization;
const budget={taskRunBudgetMicros:2000000,currency:'USD',budgets:[]};
const reviewed=(overrides:Partial<TaskReview>={}):TaskReview=>({canStart:true,currentGraphVersion:1,blockers:[],task,isolation:'docker',grants:[{name:'filesystem.read'}],...overrides}) as TaskReview;
const fakeApi=(routes:Parameters<typeof routeApi>[0])=>routeApi({'GET /api/v1/graphs/workflow':()=>({graph:{metadata:{id:'workflow',version:1},spec:{}}}),...routes});

it('only permits prepared admission for the reviewed task revision and current authorization',()=>{
 expect(canRunPreparedTask(task,ready,reviewed(),authorization)).toBe(true);
 expect(canRunPreparedTask(task,ready,reviewed())).toBe(false);
 expect(canRunPreparedTask(task,ready,reviewed(),{scope:''})).toBe(false);
 expect(canRunPreparedTask(task,ready,reviewed(),{scope:'current',record:{taskId:'other',scope:'current'}} as TaskAuthorization)).toBe(false);
 expect(canRunPreparedTask(task,ready,reviewed({task:{...task,id:'other'}}),authorization)).toBe(false);
 expect(canRunPreparedTask(task,ready,reviewed({task:{...task,revision:3}}),authorization)).toBe(false);
 expect(canRunPreparedTask({...task,status:'running'},ready,reviewed(),authorization)).toBe(false);
 expect(canRunPreparedTask(task,{...ready,state:'building'},reviewed(),authorization)).toBe(false);
 expect(canRunPreparedTask(task,{...ready,upToDate:false},reviewed(),authorization)).toBe(false);
 expect(canRunPreparedTask(task,ready,reviewed({canStart:false}),authorization)).toBe(false);
 expect(canRunPreparedTask(task,ready,reviewed(),{scope:'new',record:{scope:'old'}} as TaskAuthorization)).toBe(false);
 expect(canRunPreparedTask(task,ready,reviewed(),{scope:'current',record:{taskId:'t',scope:'current',mode:'full'}} as TaskAuthorization)).toBe(true);
});

it('checks readiness automatically and starts only after the explicit Run task click',async()=>{
 const backend=fakeApi({'GET /api/v1/cost/budgets/preview':()=>budget,'GET /api/v1/tasks/t/review':()=>reviewed(),'GET /api/v1/tasks/t/authorization':()=>authorization,'POST /api/v1/tasks/t/start':()=>apiError(409,'Task changed','conflict')});
 render(<TaskEnvironment task={task} environment={ready} canControl onChanged={()=>{}}/>);
 await screen.findByText('workflow · Version 1');
 expect(screen.getByText('Ask for approval')).toBeTruthy();
 expect(screen.getByText(/Granted tools: filesystem.read/)).toBeTruthy();
 expect(backend.calls.every(call=>call.method==='GET')).toBe(true);
 expect(screen.getByRole('button',{name:'Run task'})).toHaveProperty('disabled',false);
 await userEvent.click(screen.getByRole('button',{name:'Run task'}));await screen.findByText('Task changed');
 expect(backend.find('POST','/api/v1/tasks/t/start')[0]?.headers['If-Match']).toBe('4');
 expect(screen.getByRole('button',{name:'Run task'})).toHaveProperty('disabled',true);
 await userEvent.click(screen.getByRole('button',{name:'Check requirements again'}));
 await waitFor(()=>expect(screen.getByRole('button',{name:'Run task'})).toHaveProperty('disabled',false));
 expect(backend.find('POST','/api/v1/tasks/t/start')).toHaveLength(1);
});

it('rechecks after a build becomes ready and after revision changes without starting work',async()=>{
 let current=task;
 const backend=fakeApi({'GET /api/v1/cost/budgets/preview':()=>budget,'GET /api/v1/tasks/t/review':()=>reviewed({task:current}),'GET /api/v1/tasks/t/authorization':()=>authorization});
 const {rerender}=render(<TaskEnvironment task={task} environment={{state:'building',upToDate:false} as EnvironmentView} canControl onChanged={()=>{}}/>);
 expect(backend.calls).toHaveLength(0);
 rerender(<TaskEnvironment task={task} environment={ready} canControl onChanged={()=>{}}/>);
 await waitFor(()=>expect(screen.getByRole('button',{name:'Run task'})).toHaveProperty('disabled',false));
 current={...task,revision:5};
 rerender(<TaskEnvironment task={current} environment={ready} canControl onChanged={()=>{}}/>);
 await waitFor(()=>expect(backend.find('GET','/api/v1/tasks/t/review')).toHaveLength(2));
 await waitFor(()=>expect(screen.getByRole('button',{name:'Run task'})).toHaveProperty('disabled',false));
 expect(backend.calls.every(call=>call.method==='GET')).toBe(true);
});

it('ignores an old readiness response and submits one start while the request is pending',async()=>{
 let releaseOld!: (review:TaskReview)=>void;
 let releaseStart!: (value:TaskDraft)=>void;
 let reviews=0;
 const next={...task,revision:5};
 const backend=fakeApi({'GET /api/v1/cost/budgets/preview':()=>budget,'GET /api/v1/tasks/t/review':()=>++reviews===1?new Promise<TaskReview>(resolve=>{releaseOld=resolve;}):reviewed({task:next}),'GET /api/v1/tasks/t/authorization':()=>authorization,'POST /api/v1/tasks/t/start':()=>new Promise<TaskDraft>(resolve=>{releaseStart=resolve;})});
 const {rerender}=render(<TaskEnvironment task={task} environment={ready} canControl onChanged={()=>{}}/>);
 await waitFor(()=>expect(reviews).toBe(1));
 rerender(<TaskEnvironment task={next} environment={ready} canControl onChanged={()=>{}}/>);
 await waitFor(()=>expect(screen.getByRole('button',{name:'Run task'})).toHaveProperty('disabled',false));
 releaseOld(reviewed());
 await userEvent.dblClick(screen.getByRole('button',{name:'Run task'}));
 expect(backend.find('POST','/api/v1/tasks/t/start')).toHaveLength(1);
 expect(backend.find('POST','/api/v1/tasks/t/start')[0]?.headers['If-Match']).toBe('5');
 releaseStart({...next,status:'running'});
 await waitFor(()=>expect(screen.getByRole('button',{name:'Run task'})).toHaveProperty('disabled',true));
});

it('retains loading and failure states and never offers running with unavailable access',async()=>{
 let fail=true;
 const backend=fakeApi({'GET /api/v1/cost/budgets/preview':()=>budget,'GET /api/v1/tasks/t/review':()=>reviewed(),'GET /api/v1/tasks/t/authorization':()=>fail?apiError(503,'Authorization unavailable','unavailable'):authorization});
 render(<TaskEnvironment task={task} environment={ready} canControl onChanged={()=>{}}/>);
 expect(screen.getByRole('button',{name:'Run task'})).toHaveProperty('disabled',true);
 await screen.findByText('Authorization unavailable');
 expect(screen.queryByText('Ask for approval')).toBeNull();
 expect(screen.getByRole('button',{name:'Run task'})).toHaveProperty('disabled',true);
 fail=false;await userEvent.click(screen.getByRole('button',{name:'Check requirements again'}));
 await screen.findByText('Ask for approval');
 expect(backend.calls.every(call=>call.method==='GET')).toBe(true);
});

it('shows changed access and budget before asking for a new explicit Run task click',async()=>{
 let currentAuthorization:TaskAuthorization=authorization;
 let currentBudget=budget;
 const backend=fakeApi({'GET /api/v1/cost/budgets/preview':()=>currentBudget,'GET /api/v1/tasks/t/review':()=>reviewed(),'GET /api/v1/tasks/t/authorization':()=>currentAuthorization,'POST /api/v1/tasks/t/start':()=>({...task,status:'running'})});
 render(<TaskEnvironment task={task} environment={ready} canControl onChanged={()=>{}}/>);
 await screen.findByText('Ask for approval');
 currentAuthorization={scope:'new-scope',record:{taskId:'t',mode:'full',scope:'new-scope',actor:'admin',policyVersion:1,revision:1}};
 currentBudget={...budget,taskRunBudgetMicros:3000000};
 await userEvent.click(screen.getByRole('button',{name:'Run task'}));
 await screen.findByText(/Task requirements changed/);
 expect(screen.getByText('Full access')).toBeTruthy();expect(screen.getByText(/Task budget: \$3.00/)).toBeTruthy();
 expect(backend.find('POST','/api/v1/tasks/t/start')).toHaveLength(0);
 await userEvent.click(screen.getByRole('button',{name:'Run task'}));
 await waitFor(()=>expect(backend.find('POST','/api/v1/tasks/t/start')).toHaveLength(1));
});

it('fails closed when budget inspection fails at readiness or immediately before starting',async()=>{
 let failing=true;
 const backend=fakeApi({'GET /api/v1/cost/budgets/preview':()=>failing?apiError(503,'Budget unavailable','unavailable'):budget,'GET /api/v1/tasks/t/review':()=>reviewed(),'GET /api/v1/tasks/t/authorization':()=>authorization});
 render(<TaskEnvironment task={task} environment={ready} canControl onChanged={()=>{}}/>);
 await screen.findByText('Budget unavailable');
 expect(screen.getByRole('button',{name:'Run task'})).toHaveProperty('disabled',true);
 failing=false;await userEvent.click(screen.getByRole('button',{name:'Check requirements again'}));
 await screen.findByText('Ask for approval');
 failing=true;await userEvent.click(screen.getByRole('button',{name:'Run task'}));
 await screen.findByText('Budget unavailable');
 expect(backend.calls.every(call=>call.method==='GET')).toBe(true);
});

it('refuses mismatched reviews and stale authorization while preserving their explanation',async()=>{
 const backend=fakeApi({'GET /api/v1/cost/budgets/preview':()=>budget,'GET /api/v1/tasks/t/review':()=>reviewed({task:{...task,revision:5}}),'GET /api/v1/tasks/t/authorization':()=>authorization});
 render(<TaskEnvironment task={task} environment={ready} canControl onChanged={()=>{}}/>);
 await screen.findByText(/The task changed while checking requirements/);
 expect(screen.getByRole('button',{name:'Run task'})).toHaveProperty('disabled',true);
 expect(backend.calls.every(call=>call.method==='GET')).toBe(true);
 cleanup();
 fakeApi({'GET /api/v1/cost/budgets/preview':()=>budget,'GET /api/v1/tasks/t/review':()=>reviewed(),'GET /api/v1/tasks/t/authorization':()=>({scope:'changed',record:{scope:'old',mode:'full'}})});
 render(<TaskEnvironment task={task} environment={ready} canControl onChanged={()=>{}}/>);
 await screen.findByText(/Saved authorization is out of date/);
 expect(screen.getByText('Full access')).toBeTruthy();
 expect(screen.getByRole('button',{name:'Run task'})).toHaveProperty('disabled',true);
});

it('requires reviewed build definition and explicit consent before retrying the failed environment',async()=>{
 const onChanged=vi.fn();
 const backend=fakeApi({'GET /api/v1/cost/budgets/preview':()=>budget,'GET /api/v1/tasks/t/review':()=>reviewed({canStart:false,canPrepareEnvironment:false,blockers:['Build failed'],environmentDefinition:{dockerfile:'FROM reviewed:image'}}),'GET /api/v1/tasks/t/authorization':()=>authorization,'POST /api/v1/tasks/t/prepare-environment':()=>({...task,revision:5})});
 render(<TaskEnvironment task={task} environment={{state:'failed',upToDate:false} as EnvironmentView} canControl onChanged={onChanged}/>);
 await screen.findByText('Build failed');
 expect(screen.getByRole('button',{name:'Retry environment build'})).toHaveProperty('disabled',true);
 await userEvent.click(screen.getByText('Environment build definition'));
 expect(screen.getByText(/FROM reviewed:image/)).toBeTruthy();
 await userEvent.click(screen.getByRole('checkbox',{name:'I reviewed the environment build definition and authorize retrying this build.'}));
 await userEvent.click(screen.getByRole('button',{name:'Retry environment build'}));
 await waitFor(()=>expect(onChanged).toHaveBeenCalledOnce());
 expect(backend.find('POST','/api/v1/tasks/t/prepare-environment')[0]?.headers['If-Match']).toBe('4');
 expect(backend.find('POST','/api/v1/tasks/t/start')).toHaveLength(0);
});

it('keeps viewers read-only and displays build failures without a result',async()=>{
 const backend=fakeApi({'GET /api/v1/cost/budgets/preview':()=>budget,'GET /api/v1/tasks/t/review':()=>reviewed({canStart:false}),'GET /api/v1/tasks/t/authorization':()=>authorization});
 render(<TaskEnvironment task={task} environment={{state:'failed',upToDate:false,error:'Setup denied'} as EnvironmentView} canControl={false} onChanged={()=>{}}/>);
 expect(screen.getByText('Setup denied')).toBeTruthy();expect(screen.queryByRole('button',{name:'Retry environment build'})).toBeNull();expect(screen.queryByRole('button',{name:'Run task'})).toBeNull();
 await screen.findByText('workflow · Version 1');
 expect(backend.calls.every(call=>call.method==='GET')).toBe(true);
});

it('shows updating inert logs and lets the user pause following without a write',async()=>{
 const backend=fakeApi({});
 const environment={state:'building',phase:'building_image',upToDate:false,log:'Step 1\n<script>inert</script>'} as EnvironmentView;
 const {rerender}=render(<TaskEnvironment task={task} environment={environment} canControl onChanged={()=>{}}/>);
 const output=screen.getByLabelText('Build output');
 expect(output.getAttribute('tabindex')).toBe('0');
 expect(output.textContent).toBe(environment.log);
 expect(output.querySelector('script')).toBeNull();
 expect(screen.queryByRole('button',{name:'Run task'})).toBeNull();
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
 render(<TaskEnvironment task={{...task,status:'running'}} environment={ready} canControl={false} onChanged={()=>{}}/>);
 expect(screen.getByText(/No build log recorded yet/)).toBeTruthy();
 expect(screen.queryByLabelText('Build output')).toBeNull();
});


it('describes a zero per-task budget as uncapped instead of a zero-dollar allowance',async()=>{
 fakeApi({'GET /api/v1/cost/budgets/preview':()=>({...budget,taskRunBudgetMicros:0}),'GET /api/v1/tasks/t/review':()=>reviewed(),'GET /api/v1/tasks/t/authorization':()=>authorization});
 render(<TaskEnvironment task={task} environment={ready} canControl onChanged={()=>{}}/>);
 await screen.findByText('No per-task spending limit is configured.');
 expect(screen.queryByText(/Task budget: \$0.00/)).toBeNull();
 expect(screen.getByRole('button',{name:'Run task'})).toHaveProperty('disabled',false);
});

it('explains a spending hold and keeps Run task disabled until limits can admit work',async()=>{
 const backend=fakeApi({'GET /api/v1/cost/budgets/preview':()=>({...budget,budgets:[{scope:'installation',id:'installation',limitMicros:10000000,reached:true,currency:'USD'} as BudgetView]}),'GET /api/v1/tasks/t/review':()=>reviewed(),'GET /api/v1/tasks/t/authorization':()=>authorization});
 render(<TaskEnvironment task={task} environment={ready} canControl onChanged={()=>{}}/>);
 await screen.findByText(/A spending limit prevents this task from starting/);
 expect(screen.getByRole('button',{name:'Run task'})).toHaveProperty('disabled',true);
 await userEvent.click(screen.getByRole('button',{name:'Run task'}));
 expect(backend.calls.every(call=>call.method==='GET')).toBe(true);
});

it('shows the pinned inherited repository and rechecks it before the explicit start',async()=>{
 let repo='https://git.example/first.git';
 const backend=fakeApi({'GET /api/v1/graphs/workflow':()=>({graph:{metadata:{id:'workflow',version:1},spec:{workspace:{repo}}}}),'GET /api/v1/cost/budgets/preview':()=>budget,'GET /api/v1/tasks/t/review':()=>reviewed(),'GET /api/v1/tasks/t/authorization':()=>authorization,'POST /api/v1/tasks/t/start':()=>({...task,status:'running'})});
 render(<TaskEnvironment task={task} environment={ready} canControl onChanged={()=>{}}/>);
 await screen.findByText(repo);
 expect(String(backend.fetch.mock.calls.find(([url])=>String(url).includes('/graphs/workflow'))?.[0])).toContain('version=1');
 repo='https://git.example/changed.git';
 await userEvent.click(screen.getByRole('button',{name:'Run task'}));
 await screen.findByText(repo);await screen.findByText(/Task requirements changed/);
 expect(backend.find('POST','/api/v1/tasks/t/start')).toHaveLength(0);
 await userEvent.click(screen.getByRole('button',{name:'Run task'}));
 await waitFor(()=>expect(backend.find('POST','/api/v1/tasks/t/start')).toHaveLength(1));
});

it('blocks starting when the pinned workflow repository cannot be loaded or verified',async()=>{
 let wrongVersion=false;
 const backend=fakeApi({'GET /api/v1/graphs/workflow':()=>wrongVersion?{graph:{metadata:{id:'workflow',version:2},spec:{workspace:{repo:'https://git.example/current.git'}}}}:apiError(503,'Pinned repository unavailable','unavailable'),'GET /api/v1/cost/budgets/preview':()=>budget,'GET /api/v1/tasks/t/review':()=>reviewed(),'GET /api/v1/tasks/t/authorization':()=>authorization});
 render(<TaskEnvironment task={task} environment={ready} canControl onChanged={()=>{}}/>);
 await screen.findByText('Pinned repository unavailable');
 expect(screen.getByRole('button',{name:'Run task'})).toHaveProperty('disabled',true);
 wrongVersion=true;await userEvent.click(screen.getByRole('button',{name:'Check requirements again'}));
 await screen.findByText(/The pinned workflow could not be verified/);
 expect(screen.getByRole('button',{name:'Run task'})).toHaveProperty('disabled',true);
 expect(backend.calls.every(call=>call.method==='GET')).toBe(true);
});
