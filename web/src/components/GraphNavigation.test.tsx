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
 render(<GraphList graphs={[{graph,record:{state:'running',version:1}} as GraphSummary]} onOpen={open} onAgent={configure} onChanged={()=>{}}/>);
 fireEvent.click(screen.getByRole('button',{name:'Configure Draft reviewer'}));
 expect(configure).toHaveBeenCalledWith('g','reviewer');expect(open).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole('button',{name:'Factory'}));expect(open).toHaveBeenCalledWith('g');
 expect(screen.getByRole('button',{name:'Delete'})).toHaveProperty('disabled',true);
});
