import {cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it,vi} from 'vitest';
import {TasksPage,SCMHandOffField} from './TasksPage';
import {fakeApi,apiError} from '../test/fakeApi';
import type {GraphSummary,Info} from '../api/types';
const graph={graph:{metadata:{id:'flow',name:'Reusable work',version:3},spec:{nodes:[{id:'worker',runtime:{kind:'echo'},isolation:{mode:'docker'}}]}}} as GraphSummary;
const info={identity:{role:'admin'}} as Info;
const project={id:'repo',name:'Repository',head:'a'.repeat(40),repositoryUrl:'https://example.com/team/repo.git',executionMode:'sandbox'};
afterEach(()=>{cleanup();vi.unstubAllGlobals();vi.restoreAllMocks();});

it('carries project/workflow context through the real API client into a reviewed saved draft without starting',async()=>{
 let savedTask:Record<string,unknown>={};
 const backend=fakeApi({'GET /api/v1/providers':()=>[], 'GET /api/v1/projects':()=>[project], 'GET /api/v1/scm/connections':()=>[],
  'POST /api/v1/tasks':call=>{savedTask={...call.body as object,id:'tsk_new',revision:1,status:'draft'};return savedTask;},
  'GET /api/v1/cost/budgets/preview':()=>({taskRunBudgetMicros:0,currency:'USD',budgets:[]}),
  'GET /api/v1/tasks/tsk_new/authorization':()=>({scope:'fixture'}),
  'GET /api/v1/tasks/tsk_new/review':()=>({task:savedTask,blockers:[],canStart:true,validation:{},model:{},grants:[]}),
 });
 render(<TasksPage create entry={{project:'repo',workflow:'flow'}} info={info} graphs={[graph]}/>);
 await waitFor(()=>expect(screen.getByLabelText('Project')).toHaveProperty('value','repo'));
 expect(screen.getByLabelText('Workflow version')).toHaveProperty('value','3');
 expect(screen.getByLabelText('Entry agent')).toHaveProperty('value','worker');
 await userEvent.type(screen.getByLabelText('Request'),'Inspect the repository');
 expect(screen.getByRole('region',{name:'Task setup summary'}).textContent).toContain('Commit '+project.head);
 await userEvent.click(screen.getByRole('button',{name:'Run task'}));
 await screen.findByRole('region',{name:'Launch review'});
 expect(backend.find('GET','/api/v1/tasks/tsk_new/review').length).toBeGreaterThan(0);
 expect(backend.find('POST','/api/v1/tasks')[0].body).toMatchObject({projectId:'repo',baseCommit:project.head,graphVersion:3,nodeId:'worker'});
 expect(backend.calls.filter(c=>c.method!=='GET').map(c=>c.path)).toEqual(['/api/v1/tasks']);
});

it('saves before configuration handoff and keeps failed handoffs in the editor',async()=>{
 const save=vi.fn().mockReturnValueOnce(apiError(503,'Storage offline','unavailable')).mockImplementation(call=>({...call.body,id:'tsk_handoff',revision:1,status:'draft'}));
 const backend=fakeApi({'GET /api/v1/providers':()=>[], 'GET /api/v1/projects':()=>[], 'GET /api/v1/scm/connections':()=>[], 'POST /api/v1/tasks':save});
 location.hash='#/tasks/new';
 render(<TasksPage create info={info} graphs={[graph]}/>);
 await userEvent.type(screen.getByLabelText('Request'),'Retain my work');
 await userEvent.click(screen.getByText('Prepare this task for a specific goal'));
 await userEvent.click(screen.getByRole('button',{name:'Configure models and return'}));
 await screen.findByText('Could not save the task before opening configuration: Storage offline');
 expect(location.hash).toBe('#/tasks/new');expect(screen.getByLabelText('Request')).toHaveProperty('value','Retain my work');
 await userEvent.click(screen.getByRole('button',{name:'Configure models and return'}));
 await waitFor(()=>expect(location.hash).toBe('#/models?returnTo=%23%2Ftasks%2Ftsk_handoff'));
 expect(backend.calls.filter(c=>c.method!=='GET').every(c=>c.path==='/api/v1/tasks')).toBe(true);
});

it('distinguishes SCM failure, retry, empty and saved unavailable choices',async()=>{
 fakeApi({'GET /api/v1/scm/connections':vi.fn().mockReturnValueOnce(apiError(503,'SCM offline','unavailable')).mockReturnValue([])});
 render(<SCMHandOffField value="saved" onChange={()=>{}}/>);
 await screen.findByText('SCM offline');
 expect(screen.getByRole('combobox')).toHaveProperty('value','saved');
 expect(screen.queryByText(/No source control connections configured/)).toBeNull();
 await userEvent.click(screen.getByRole('button',{name:'Retry source control connections'}));
 await screen.findByText(/No source control connections configured/);
 expect(within(screen.getByRole('combobox')).getByRole('option',{name:'saved (saved selection unavailable)'})).toBeTruthy();
});

it('keeps entry context when request typing precedes slow project discovery',async()=>{
 let release!:(value:unknown)=>void;
 const pending=new Promise(resolve=>{release=resolve;});
 fakeApi({'GET /api/v1/providers':()=>[],'GET /api/v1/projects':()=>pending,'GET /api/v1/scm/connections':()=>[]});
 render(<TasksPage create entry={{workflow:'flow',project:'repo'}} info={info} graphs={[graph]}/>);
 await userEvent.type(screen.getByLabelText('Request'),'Already typing');
 release([project]);
 await waitFor(()=>expect(screen.getByLabelText('Project')).toHaveProperty('value','repo'));
 expect(screen.getByLabelText('Workflow')).toHaveProperty('value','flow');
 expect(screen.getByLabelText('Entry agent')).toHaveProperty('value','worker');
 expect(screen.getByLabelText('Request')).toHaveProperty('value','Already typing');
});

it('keeps project entry context ahead of a selected workflow preference during slow discovery',async()=>{
 let release!:(value:unknown)=>void;
 fakeApi({'GET /api/v1/providers':()=>[],'GET /api/v1/projects':()=>new Promise(resolve=>{release=resolve;}),'GET /api/v1/scm/connections':()=>[]});
 const preferred={...graph,graph:{...graph.graph,spec:{...graph.graph.spec,task:{projectId:'other'}}}};
 render(<TasksPage create entry={{project:'repo'}} info={info} graphs={[preferred]}/>);
 fireEvent.change(screen.getByLabelText('Workflow'),{target:{value:'flow'}});
 release([project,{...project,id:'other',name:'Other repository'}]);
 await waitFor(()=>expect(screen.getByLabelText('Project')).toHaveProperty('value','repo'));
 await waitFor(()=>expect(screen.getByRole('region',{name:'Task setup summary'}).textContent).toContain('Commit '+project.head));
});

it('retries failed workflow discovery without consuming entry context or request edits',async()=>{
 const reload=vi.fn();
 fakeApi({'GET /api/v1/providers':()=>[],'GET /api/v1/projects':()=>[project],'GET /api/v1/scm/connections':()=>[]});
 const view=render(<TasksPage create entry={{workflow:'flow',project:'repo'}} info={info} graphs={[]} graphsError="Workflow discovery offline" onReloadGraphs={reload}/>);
 await screen.findByText('Workflow discovery offline');
 await userEvent.type(screen.getByLabelText('Request'),'Keep this request');
 await userEvent.click(screen.getByRole('button',{name:'Retry workflows'}));
 expect(reload).toHaveBeenCalledTimes(1);
 view.rerender(<TasksPage create entry={{workflow:'flow',project:'repo'}} info={info} graphs={[graph]} onReloadGraphs={reload}/>);
 await waitFor(()=>expect(screen.getByLabelText('Entry agent')).toHaveProperty('value','worker'));
 expect(screen.getByLabelText('Workflow version')).toHaveProperty('value','3');
 expect(screen.getByLabelText('Project')).toHaveProperty('value','repo');
 expect(screen.getByLabelText('Request')).toHaveProperty('value','Keep this request');
});

it('retains explicit environment and delivery choices while applying late workflow and project context',async()=>{
 let release!:(value:unknown)=>void;
 fakeApi({'GET /api/v1/providers':()=>[],'GET /api/v1/projects':()=>new Promise(resolve=>{release=resolve;}),
  'GET /api/v1/scm/connections':()=>[{name:'mine',repository:'team/mine'},{name:'workflow',repository:'team/default'}]});
 const preferred={...graph,graph:{...graph.graph,spec:{...graph.graph.spec,task:{scmConnection:'workflow'}}}};
 render(<TasksPage create entry={{workflow:'flow',project:'repo'}} info={info} graphs={[preferred]}/>);
 await screen.findByRole('option',{name:'mine (team/mine)'});
 fireEvent.change(screen.getByLabelText('Source control hand-off'),{target:{value:'mine'}});
 fireEvent.change(screen.getByLabelText('Execution environment'),{target:{value:'workflow'}});
 release([project]);
 await waitFor(()=>expect(screen.getByLabelText('Project')).toHaveProperty('value','repo'));
 expect(screen.getByLabelText('Source control hand-off')).toHaveProperty('value','mine');
 expect(screen.getByLabelText('Execution environment')).toHaveProperty('value','workflow');
});

it('retries failed project discovery before applying task entry context',async()=>{
 fakeApi({'GET /api/v1/providers':()=>[],
  'GET /api/v1/projects':vi.fn().mockReturnValueOnce(apiError(503,'Project discovery offline','unavailable')).mockReturnValue([project]),
  'GET /api/v1/scm/connections':()=>[]});
 render(<TasksPage create entry={{workflow:'flow',project:'repo'}} info={info} graphs={[graph]}/>);
 await screen.findByText('Project discovery offline');
 await userEvent.type(screen.getByLabelText('Request'),'Retain after project retry');
 await userEvent.click(screen.getByRole('button',{name:'Retry projects'}));
 await waitFor(()=>expect(screen.getByLabelText('Entry agent')).toHaveProperty('value','worker'));
 expect(screen.getByLabelText('Project')).toHaveProperty('value','repo');
 expect(screen.getByLabelText('Request')).toHaveProperty('value','Retain after project retry');
});

it('retains a deliberate empty project and manual delivery choice during late discovery',async()=>{
 let release!:(value:unknown)=>void;
 fakeApi({'GET /api/v1/providers':()=>[],'GET /api/v1/projects':()=>new Promise(resolve=>{release=resolve;}),
  'GET /api/v1/scm/connections':()=>[]});
 const preferred={...graph,graph:{...graph.graph,spec:{...graph.graph.spec,task:{projectId:'repo',scmConnection:'workflow'}}}};
 render(<TasksPage create entry={{workflow:'flow',project:'repo'}} info={info} graphs={[preferred]}/>);
 fireEvent.change(screen.getByLabelText('Project'),{target:{value:''}});
 fireEvent.change(screen.getByLabelText('Source control hand-off'),{target:{value:''}});
 release([project]);
 await waitFor(()=>expect(screen.getByLabelText('Entry agent')).toHaveProperty('value','worker'));
 expect(screen.getByLabelText('Project')).toHaveProperty('value','');
 expect(screen.getByLabelText('Source control hand-off')).toHaveProperty('value','');
});
