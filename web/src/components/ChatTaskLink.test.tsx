import { cleanup, render, screen, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { fakeApi } from '../test/fakeApi';
import { ChatPage } from './ChatPage';

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

it('links only the verified task to its exact detail route', async () => {
  const path = '/api/v1/chat/sessions';
  const id = 'b1a120948c7c4b8b8411802a438ae602';
  const task = 'tsk_releasereview';
  fakeApi({
    'GET /api/v1/chat/settings': () => ({status:'configured',revision:1}),
    [`GET ${path}`]: () => ({items:[],shared:false}),
    [`GET ${path}/${id}`]: () => ({id,archived:false,messages:[
      {id:'link', role:'assistant', text:'Open the task details.', task:{id:task, state:'', linkOnly:true}},
      {id:'missing', role:'assistant', text:'No task was found with that ID.'},
    ]}),
  });
  render(<ChatPage id={id} archived={false} onOpen={vi.fn()}/>);
  const link = await screen.findByRole('link', {name:'Open task'});
  expect(link.getAttribute('href')).toBe(`#/tasks/${task}`);
  expect(screen.queryByRole('link', {name:'View task result'})).toBeNull();
  const missingReply = screen.getByText('No task was found with that ID.').closest('article');
  expect(missingReply).toBeTruthy();
  expect(within(missingReply!).queryByRole('link')).toBeNull();
});
