import {cleanup,fireEvent,render,screen} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
import {ChatSessionSearch} from './ChatSessionSearch';
import {api} from '../api/client';
afterEach(()=>{cleanup();vi.restoreAllMocks();});
it('searches beyond the visible page and opens the selected result',async()=>{
  const search=vi.spyOn(api,'chatSessions');
  const row={id:'first',title:'Latest',running:0,lastActivity:'',unread:0,unreadSuccess:0,unreadFailure:0,unreadCancellation:0,decisions:0,unknown:0};
  search.mockImplementation(async(_scope,cursor)=>({items:[cursor?{...row,id:'second',title:'Older release'}:row],shared:false,archivedUnread:0,next:cursor?undefined:'older'}));
  const open=vi.fn();
  render(<ChatSessionSearch archived onClose={vi.fn()} onSelect={open}/>);
  await screen.findByRole('button',{name:'Latest'});
  fireEvent.change(screen.getByRole('searchbox'),{target:{value:'release'}});
  fireEvent.click(await screen.findByRole('button',{name:'Older release'}));
  expect(open).toHaveBeenCalledWith('second',true);
});
