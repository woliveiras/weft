import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import { apiError, fakeApi } from '../test/fakeApi';
import { WorkflowCreate } from './WorkflowCreate';
afterEach(cleanup);

async function newAgent() {
  await userEvent.type(screen.getByLabelText('Workflow name'), 'Review requests');
  await userEvent.click(screen.getByRole('button',{name:'Create new agent'}));
  await userEvent.type(screen.getByLabelText('New agent name'),'Reviewer');
  await userEvent.type(screen.getByLabelText('New agent instructions'),'Review the request carefully.');
  await userEvent.click(screen.getByRole('button',{name:'Add new agent'}));
}

it('reviews a task workflow before saving without granting tools or starting work', async () => {
  const created = vi.fn();
  const backend = fakeApi({
    'POST /api/v1/graphs/validate':()=>({valid:true}),
    'POST /api/v1/graphs':call=>({graph:call.body,record:{state:'stopped',version:1}}),
  });
  render(<WorkflowCreate onCancel={vi.fn()} onCreated={created}/>);
  await newAgent();
  await userEvent.click(screen.getByRole('button',{name:'Propose connections'}));
  expect(backend.calls).toEqual([]);
  await userEvent.click(screen.getByRole('button',{name:'Review workflow'}));
  const heading=await screen.findByRole('heading',{name:'Review before saving'});
  await waitFor(()=>expect(document.activeElement).toBe(heading));
  expect(screen.getByText(/Versioned definition only/)).toBeTruthy();
  expect(backend.find('POST','/api/v1/graphs')).toHaveLength(0);
  await userEvent.click(screen.getByRole('button',{name:'Save workflow'}));
  await waitFor(()=>expect(created).toHaveBeenCalledWith(expect.objectContaining({graph:expect.objectContaining({metadata:expect.objectContaining({id:'review-requests'})})})));
  expect(backend.find('POST','/api/v1/graphs')[0].body).toMatchObject({metadata:{id:'review-requests'},spec:{task:{nodeId:'reviewer',resultNodeId:'reviewer',channelId:'requests',messageType:'work.request'},nodes:[{id:'reviewer',tools:[],isolation:{mode:'docker'}}]}});
  expect(backend.calls.filter(call=>/start|prepare|approval/.test(call.path))).toEqual([]);
});

it('retains corrections after failed validation or save and locks duplicate saves',async()=>{
  let valid=false;
  let finish!: (response:Response)=>void;
  const created=vi.fn();
  const backend=fakeApi({'POST /api/v1/graphs/validate':()=> valid?{valid:true}:{valid:false,errors:[{path:'spec.nodes[0]',message:'Provider unavailable'}]},'POST /api/v1/graphs':()=>new Promise<Response>(resolve=>{finish=resolve;})});
  render(<WorkflowCreate onCancel={vi.fn()} onCreated={created}/>);
  await newAgent();
  await userEvent.click(screen.getByRole('button',{name:'Review workflow'}));
  expect(await screen.findByText(/Provider unavailable/)).toBeTruthy();
  expect(screen.queryByRole('button',{name:'Save workflow'})).toBeNull();
  expect(screen.getByLabelText('Workflow name')).toHaveProperty('value','Review requests');
  valid=true;
  await userEvent.click(screen.getByRole('button',{name:'Review workflow'}));
  await userEvent.click(await screen.findByRole('button',{name:'Save workflow'}));
  expect(screen.getByRole('button',{name:'Saving workflow…'})).toHaveProperty('disabled',true);
  expect(backend.find('POST','/api/v1/graphs')).toHaveLength(1);
  finish(apiError(503,'Storage unavailable','unavailable'));
  expect(await screen.findByText('Storage unavailable')).toBeTruthy();
  expect(screen.getByRole('button',{name:'Save workflow'})).toHaveProperty('disabled',false);
  expect(created).not.toHaveBeenCalled();
});

it('keeps advanced and visual drafts separate and cancels without a write',async()=>{
  vi.spyOn(window,'confirm').mockReturnValue(false);
  const cancelled=vi.fn();
  const backend=fakeApi({});
  render(<WorkflowCreate onCancel={cancelled} onCreated={vi.fn()}/>);
  await userEvent.type(screen.getByLabelText('Workflow name'),'Retained visual draft');
  await userEvent.click(screen.getByRole('button',{name:'Advanced YAML / JSON'}));
  await userEvent.clear(screen.getByLabelText('Workflow document'));
  await userEvent.type(screen.getByLabelText('Workflow document'),'# advanced draft');
  await userEvent.click(screen.getByRole('button',{name:'Visual builder'}));
  expect(screen.getByLabelText('Workflow name')).toHaveProperty('value','Retained visual draft');
  await userEvent.click(screen.getByRole('button',{name:'Advanced YAML / JSON'}));
  expect(screen.getByLabelText('Workflow document')).toHaveProperty('value','# advanced draft');
  await userEvent.click(screen.getByRole('button',{name:'Cancel'}));
  expect(cancelled).not.toHaveBeenCalled();
  vi.mocked(window.confirm).mockReturnValue(true);
  await userEvent.click(screen.getByRole('button',{name:'Cancel'}));
  expect(cancelled).toHaveBeenCalledOnce();
  expect(backend.calls).toEqual([]);
});

it('exposes catalogue loading failure and retry without dropping the workflow draft',async()=>{
  let fail=true;
  fakeApi({'GET /api/v1/agents':()=>fail?apiError(503,'Catalogue unavailable','unavailable'):[]});
  render(<WorkflowCreate onCancel={vi.fn()} onCreated={vi.fn()}/>);
  await userEvent.type(screen.getByLabelText('Workflow name'),'Keep this work');
  await userEvent.click(screen.getByRole('button',{name:'Add existing agent'}));
  expect(await screen.findByText('Catalogue unavailable')).toBeTruthy();
  fail=false;
  await userEvent.click(screen.getByRole('button',{name:'Retry loading agents'}));
  expect(await screen.findByText(/No reusable agents yet/)).toBeTruthy();
  await userEvent.click(within(screen.getByRole('region',{name:'Agent selection'})).getByRole('button',{name:'Cancel'}));
  expect(screen.getByLabelText('Workflow name')).toHaveProperty('value','Keep this work');
});

it('blocks definition creation for viewers',async()=>{
  const backend=fakeApi({});
  render(<WorkflowCreate canCreate={false} onCancel={vi.fn()} onCreated={vi.fn()}/>);
  expect(screen.getByText('An administrator must create workflow definitions.')).toBeTruthy();
  expect(screen.queryByLabelText('Workflow name')).toBeNull();
  expect(backend.calls).toEqual([]);
});

it('reuses an explicit catalogue version and workflow parameters while retaining its granted capabilities',async()=>{
  const definition={name:'Reusable reviewer',runtime:{kind:'native'},prompt:'Review @repository.',tools:[{name:'filesystem.read'}],parameters:[{name:'repository',required:true}]};
  const backend=fakeApi({
    'GET /api/v1/agents':()=>[{id:'reviewer',name:definition.name,latest:2}],
    'GET /api/v1/agents/reviewer':()=>({id:'reviewer',versions:[{version:2,definition}]}),
    'POST /api/v1/graphs/validate':()=>({valid:true}),
  });
  render(<WorkflowCreate onCancel={vi.fn()} onCreated={vi.fn()}/>);
  await userEvent.type(screen.getByLabelText('Workflow name'),'Reused review');
  await userEvent.click(screen.getByRole('button',{name:'Add existing agent'}));
  const picker=screen.getByRole('region',{name:'Agent selection'});
  await screen.findByRole('option',{name:'Reusable reviewer · reviewer'});
  await userEvent.selectOptions(within(picker).getByLabelText('Reusable agent'),'reviewer');
  await within(picker).findByLabelText('Agent version');
  expect(within(picker).getByRole('button',{name:'Add to workflow'})).toHaveProperty('disabled',true);
  await userEvent.selectOptions(within(picker).getByLabelText('Agent version'),'2');
  await userEvent.type(within(picker).getByLabelText('repository (required)'),'team/project');
  await userEvent.click(within(picker).getByRole('button',{name:'Add to workflow'}));
  await userEvent.click(screen.getByRole('button',{name:'Review workflow'}));
  await screen.findByRole('heading',{name:'Review before saving'});
  expect(backend.find('POST','/api/v1/graphs/validate')[0].body).toMatchObject({spec:{nodes:[{agent:{id:'reviewer',version:2,parameters:{repository:'team/project'}},loop:{mode:'event'},tools:[{name:'filesystem.read'}]}]}});
  expect(backend.find('POST','/api/v1/graphs')).toHaveLength(0);
});

it('does not replace a newer advanced edit when an uploaded file finishes reading later',async()=>{
  fakeApi({});
  render(<WorkflowCreate onCancel={vi.fn()} onCreated={vi.fn()}/>);
  await userEvent.click(screen.getByRole('button',{name:'Advanced YAML / JSON'}));
  let finish!: (value:string)=>void;
  const file=new File(['old document'],'old.yaml',{type:'application/yaml'});
  Object.defineProperty(file,'text',{value:()=>new Promise<string>(resolve=>{finish=resolve;})});
  await userEvent.upload(screen.getByLabelText('Upload a workflow document'),file);
  expect(screen.getByRole('status').textContent).toBe('Reading workflow document…');
  expect(screen.getByRole('button',{name:'Review workflow'})).toHaveProperty('disabled',true);
  await userEvent.clear(screen.getByLabelText('Workflow document'));
  await userEvent.type(screen.getByLabelText('Workflow document'),'# newer edit');
  finish('# old upload');
  await waitFor(()=>expect(screen.getByLabelText('Workflow document')).toHaveProperty('value','# newer edit'));
  expect(screen.getByRole('button',{name:'Review workflow'})).toHaveProperty('disabled',false);
});
