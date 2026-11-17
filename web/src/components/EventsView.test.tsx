import {cleanup,render,screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,it,expect,vi} from 'vitest';
import {fakeApi} from '../test/fakeApi';
import {EventsView} from './EventsView';
import type {Graph} from '../api/types';
vi.mock('../hooks/useEventStream',()=>({useEventStream:()=>({connected:true})}));
afterEach(cleanup);
it('keeps long event types, actors and expandable details separately readable',async()=>{
 fakeApi({'GET /api/v1/graphs/g/events':()=>[{id:'e',seq:1,graphId:'g',nodeId:'reader',type:'execution.instructions.loaded',time:'2026-10-17T19:00:00Z',data:{entries:1}}]});
 render(<EventsView graph={{metadata:{id:'g'},spec:{nodes:[{id:'reader'}]}} as Graph}/>);
 expect(await screen.findByText('execution.instructions.loaded')).toBeTruthy();
 expect(screen.getByText('reader',{selector:'.tl-who'})).toBeTruthy();
 await userEvent.click(screen.getByRole('button',{name:/data/}));
 expect(screen.getByText(/"entries": 1/)).toBeTruthy();
 await userEvent.selectOptions(screen.getByLabelText('node'),'reader');
 expect(screen.getByText('execution.instructions.loaded')).toBeTruthy();
});
