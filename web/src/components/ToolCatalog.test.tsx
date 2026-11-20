import {cleanup,fireEvent,render,screen,within} from '@testing-library/react';
import {afterEach,expect,it} from 'vitest';
import {ToolCatalog} from './ToolCatalog';
import {fakeApi,apiError} from '../test/fakeApi';
afterEach(cleanup);
it('shows descriptions, combines search and capability filters and resets pagination',async()=>{
 const tools=Array.from({length:42},(_,i)=>({name:`tool.${i}`,description:'Inspect files',capability:i%2?'git.write':'git.read',schema:{}}));
 const fixture=fakeApi({'GET /api/v1/tools':()=>tools});render(<ToolCatalog/>);
 const table=await screen.findByRole('table');expect(within(table).getAllByRole('row')).toHaveLength(21);
 fireEvent.click(screen.getByRole('button',{name:'Next'}));expect(screen.getByText('Page 2 of 3')).toBeTruthy();
 fireEvent.change(screen.getByLabelText('Capability'),{target:{value:'git.read'}});expect(screen.getByText('Page 1 of 2')).toBeTruthy();
 fireEvent.change(screen.getByLabelText('Search tools'),{target:{value:'missing'}});expect(screen.getByText('No tools match these filters.')).toBeTruthy();
 fireEvent.click(screen.getByRole('button',{name:'Clear filters'}));expect(screen.getByText('42 of 42 tools · Name A–Z')).toBeTruthy();
 expect(within(screen.getByRole('table')).getAllByText('Inspect files')).toHaveLength(20);
 expect(fixture.calls.every(call=>call.method==='GET')).toBe(true);
});
it('recovers from unavailable catalogue without pretending it is an empty result',async()=>{
 let fail=true;fakeApi({'GET /api/v1/tools':()=>fail?apiError(503,'Unavailable','unavailable'):[]});render(<ToolCatalog/>);
 await screen.findByRole('alert');expect(screen.queryByText('No tools registered.')).toBeNull();fail=false;
 fireEvent.click(screen.getByRole('button',{name:'Retry tool catalog'}));await screen.findByText('No tools registered.');
});
