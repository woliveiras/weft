import {cleanup,fireEvent,render,screen} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
import {GraphList} from './GraphList';
import {AgentList} from './AgentList';
import type {GraphSummary, Graph} from '../api/types';
afterEach(cleanup);
const graph={metadata:{id:'g',name:'Factory'},spec:{nodes:[{id:'reviewer',name:'Draft reviewer'}]}} as Graph;
it('finds a workflow with a local draft and can refresh after a loading failure',()=>{
 const onOpen=vi.fn(), onChanged=vi.fn();
 const props={graphs:[{graph,record:{state:'stopped'}} as GraphSummary],dirty:{g:true},onOpen,onChanged};
 const view=render(<GraphList {...props}/>);
 expect(screen.getByText('Unsaved draft')).toBeTruthy();
 const search=screen.getByLabelText('Search workflows');
 fireEvent.change(search,{target:{value:'missing'}});
 expect(screen.getByText('No workflows match these filters.')).toBeTruthy();
 fireEvent.change(search,{target:{value:'factory'}});
 fireEvent.click(screen.getByRole('button',{name:'Factory'}));
 expect(onOpen).toHaveBeenCalledWith('g');
 view.rerender(<GraphList {...props} error="Connection unavailable"/>);
 expect(screen.getByText('Connection unavailable')).toBeTruthy();
 fireEvent.click(screen.getByRole('button',{name:'Refresh workflows'}));
 expect(onChanged).toHaveBeenCalledOnce();
});
it('lists draft agents without inventing runtime state and delegates selection and configuration',()=>{
 const select=vi.fn(),configure=vi.fn();
 render(<AgentList graph={graph} statusError="offline" selected="reviewer" onSelect={select} onConfigure={configure}/>);
 const item=screen.getByRole('button',{name:/Draft reviewer.*Status unavailable/});
 expect(item.getAttribute('aria-current')).toBe('true');
 fireEvent.click(item);expect(select).toHaveBeenCalledWith('reviewer');
 fireEvent.click(screen.getByRole('button',{name:'Configure Draft reviewer'}));expect(configure).toHaveBeenCalledWith('reviewer');
 fireEvent.change(screen.getByLabelText('Search agents'),{target:{value:'missing'}});
 expect(screen.getByText('No agents match this search.')).toBeTruthy();
});

it('exposes graph agents and keeps navigation separate from graph actions',async()=>{
 const {GraphList}=await import('./GraphList');
 const open=vi.fn(), configure=vi.fn();
 render(<GraphList canDelete graphs={[{graph,record:{state:'running',version:1}} as GraphSummary]} onOpen={open} onAgent={configure} onChanged={()=>{}}/>);
 fireEvent.click(screen.getByText('1 agent', {exact:false}));
 fireEvent.click(screen.getByRole('button',{name:'Configure Draft reviewer'}));
 expect(configure).toHaveBeenCalledWith('g','reviewer');expect(open).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole('button',{name:'Factory'}));expect(open).toHaveBeenCalledWith('g');
 fireEvent.click(screen.getByText('More actions'));
 expect(screen.getByRole('button',{name:'Delete'})).toHaveProperty('disabled',true);
});

it('shows the cleanup failure and requires administrator confirmation to defer it', async () => {
 const {GraphList} = await import('./GraphList');
 const {api, ApiError} = await import('../api/client');
 const changed = vi.fn();
 const deleteGraph = vi.spyOn(api, 'deleteGraph')
   .mockRejectedValueOnce(new ApiError(409, 'Sandbox daemon unavailable', 'environment_cleanup_incomplete'))
   .mockResolvedValueOnce(undefined);
 const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true);
 try {
   render(<GraphList canDelete graphs={[{graph, record:{state:'stopped', version:1}} as GraphSummary]}
     onOpen={vi.fn()} onChanged={changed} />);
   fireEvent.click(screen.getByText('More actions'));
   fireEvent.click(screen.getByRole('button', {name:/^Delete$/}));
   expect((await screen.findByRole('alert')).textContent).toContain('Sandbox daemon unavailable');
   expect(deleteGraph).toHaveBeenCalledOnce();
   expect(deleteGraph).toHaveBeenCalledWith('g', false);
   fireEvent.click(screen.getByRole('button', {name:'Delete and defer cleanup'}));
   expect(deleteGraph).toHaveBeenLastCalledWith('g', true);
   await vi.waitFor(() => expect(changed).toHaveBeenCalledOnce());
 } finally {
   deleteGraph.mockRestore();
   confirm.mockRestore();
 }
});

it('shows the budget refusal beside the graph resume control', async () => {
 const {GraphList} = await import('./GraphList');
 const {api, ApiError} = await import('../api/client');
 const message = 'Resume blocked by run budget g. Ask an administrator to raise it in Models → Costs.';
 const resume = vi.spyOn(api, 'graphAction').mockRejectedValueOnce(new ApiError(409, message, 'budget_resume_blocked'));
 const changed = vi.fn();
 try {
   const {container} = render(<GraphList graphs={[{graph, record:{state:'paused', version:1}} as GraphSummary]}
     onOpen={()=>{}} onChanged={changed}/>);
   fireEvent.click(screen.getByRole('button', {name:'Resume'}));
   const alert = await screen.findByRole('alert');
   expect(alert.textContent).toBe(message);
   expect(alert.closest('tr')).toBe(container.querySelector('tbody tr'));
   expect(resume).toHaveBeenCalledWith('g', 'resume');
   expect(changed).not.toHaveBeenCalled();
 } finally {
   resume.mockRestore();
 }
});

it('excludes task execution copies from editable workflows and actions',async()=>{
 const {GraphList}=await import('./GraphList');
 const task={...graph,metadata:{id:'task-run-fixture',name:'Execution',labels:{'weft.task':'tsk_fixture'}}};
 render(<GraphList canDelete graphs={[{graph:task,record:{state:'running'}} as unknown as GraphSummary]} onOpen={vi.fn()} onChanged={vi.fn()}/>);
 expect(screen.queryByText('Execution')).toBeNull();
 for(const name of ['Stop','Pause','Delete','Open']) expect(screen.queryByRole('button',{name:new RegExp(`^${name}$`)})).toBeNull();
 expect(screen.getByText(/No workflows yet/)).toBeTruthy();
});
