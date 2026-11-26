import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { ChatPage } from './ChatPage';
import { fakeApi } from '../test/fakeApi';

const paths = '/api/v1/chat/sessions';
const id = '8921e80ce56b4b50a83981d63b1a911b';
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

it('offers an offline setup path and distinguishes shared local history', async () => {
  const backend = fakeApi({
    'GET /api/v1/chat/settings': () => ({status:'unconfigured', revision:0}),
    [`GET ${paths}`]: () => ({items:[],shared:true}),
  });
  render(<ChatPage archived={false} onOpen={vi.fn()}/>);
  expect(await screen.findByText('Shared local history')).toBeTruthy();
  expect((await screen.findByRole('link',{name:'Open Chat model settings'})).getAttribute('href')).toBe('#/models/chat');
  fireEvent.change(screen.getByRole('textbox',{name:'Message'}),{target:{value:'Follow a task'}});
  expect((screen.getByRole('button',{name:'Send'}) as HTMLButtonElement).disabled).toBe(true);
  expect(backend.find('POST',paths)).toHaveLength(0);
});

it('shows simultaneous running and unread success cues with metadata and keyboard access', async () => {
  const open = vi.fn();
  fakeApi({
    'GET /api/v1/chat/settings': () => ({status:'unconfigured',revision:0}),
    [`GET ${paths}`]: () => ({shared:false,items:[{
      id, title:'Follow important release work', lastActivity:'2026-11-26T08:00:00Z',
      running:1, unread:2, unreadSuccess:1, unreadFailure:1, unreadCancellation:0, decisions:1, unknown:0,
    }]}),
  });
  render(<ChatPage archived={false} onOpen={open}/>);
  const row = await screen.findByRole('listitem');
  expect(within(row).getByRole('img',{name:/running.*completed task unread.*failed or blocked task unread.*decision required/})).toBeTruthy();
  const disclosure = within(row).getByRole('button',{name:'Details for Follow important release work'});
  fireEvent.click(disclosure);
  expect(disclosure.getAttribute('aria-expanded')).toBe('true');
  fireEvent.keyDown(disclosure,{key:'Escape'});
  expect(disclosure.getAttribute('aria-expanded')).toBe('false');
  fireEvent.click(within(row).getByRole('button',{name:/^Follow important release work$/}));
  expect(open).toHaveBeenCalledWith(id,false);
});
