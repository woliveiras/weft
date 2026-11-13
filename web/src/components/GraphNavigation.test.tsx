import {cleanup,fireEvent,render,screen} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
import {GraphSelector} from './GraphSelector';
import {AgentList} from './AgentList';
import type {GraphSummary, Graph} from '../api/types';
afterEach(cleanup);
const graph={metadata:{id:'g',name:'Factory'},spec:{nodes:[{id:'reviewer',name:'Draft reviewer'}]}} as Graph;
it('searches, selects and displays dirty graphs, with recoverable loading failures',()=>{
 const select=vi.fn(), reload=vi.fn();
 const props={graphs:[{graph,record:{state:'stopped'}} as GraphSummary],dirty:{g:true},loading:false,onSelect:select,onReload:reload};
 const view=render(<GraphSelector {...props}/>);
 fireEvent.click(screen.getByRole('button',{name:'Select graph'}));
 expect(screen.getByText(/unsaved draft/)).toBeTruthy();
 fireEvent.change(screen.getByLabelText('Search graphs'),{target:{value:'missing'}});
 expect(screen.getByText('No graphs match this search.')).toBeTruthy();
 fireEvent.change(screen.getByLabelText('Search graphs'),{target:{value:'factory'}});
 fireEvent.click(screen.getByRole('button',{name:/Factory.*unsaved draft/}));
 expect(select).toHaveBeenCalledWith('g');
 view.rerender(<GraphSelector {...props} error="Connection unavailable"/>);
 fireEvent.click(screen.getByRole('button',{name:'Select graph'}));
 expect(screen.getByText('Connection unavailable')).toBeTruthy();
 fireEvent.click(screen.getByRole('button',{name:'Reload graphs'}));expect(reload).toHaveBeenCalledOnce();
 fireEvent.keyDown(screen.getByLabelText('Search graphs'),{key:'Escape'});
 expect(screen.queryByRole('region',{name:'Graph selector'})).toBeNull();
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
 fireEvent.click(screen.getByRole('button',{name:'Configure Draft reviewer'}));
 expect(configure).toHaveBeenCalledWith('g','reviewer');expect(open).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole('button',{name:'Factory'}));expect(open).toHaveBeenCalledWith('g');
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
   expect(alert.closest('article')).toBe(container.querySelector('article'));
   expect(resume).toHaveBeenCalledWith('g', 'resume');
   expect(changed).not.toHaveBeenCalled();
 } finally {
   resume.mockRestore();
 }
});
