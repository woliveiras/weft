import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { fakeApi } from '../test/fakeApi';
import { ChatPage } from './ChatPage';

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

it('reviews the exact old and replacement payload before saving', async () => {
  const path = '/api/v1/chat/sessions';
  const id = 'b1a120948c7c4b8b8411802a438ae602';
  const before = '{"section":"summary"}';
  const after = '{"section":"upgrade","includeExamples":true}';
  const storedReview = (state: string) => ({id, archived:false, messages:[{
    id:'payload-reply', role:'assistant', text:'Compare the JSON for this channel before saving the replacement.',
    review:{id:'payload-review', state, digest:'payload-digest', expiresAt:'2999-01-01', resultVersion:2,
      taskCorrection:{task:'tsk_sample', revision:1, workflow:'release-notes', workflowVersion:3, workflowDigest:'workflow-digest',
        payload:{channel:'requests', messageType:'review.request', before, after}}},
  }]});
  const confirm = `${path}/${id}/operations/payload-review/confirm`;
  const backend = fakeApi({
    'GET /api/v1/chat/settings': () => ({status:'configured',revision:1}),
    [`GET ${path}`]: () => ({items:[],shared:false}),
    [`GET ${path}/${id}`]: () => storedReview('awaiting_confirmation'),
    [`POST ${confirm}`]: () => storedReview('applied'),
  });
  render(<ChatPage id={id} archived={false} canOperate onOpen={vi.fn()}/>);
  const card = within(await screen.findByRole('region',{name:'Task draft correction'}));
  expect(card.getByText('Channel requests · Message type review.request')).toBeTruthy();
  expect(card.getByText(before).textContent).toBe(before);
  expect(card.getByText(after).textContent).toBe(after);
  expect(card.getByRole('link',{name:'Open task draft'}).getAttribute('href')).toBe('#/tasks/tsk_sample');
  expect(backend.find('POST',confirm)).toHaveLength(0);
  fireEvent.click(card.getByRole('button',{name:'Save payload replacement'}));
  expect(await card.findByText('Task draft revision 2 saved.')).toBeTruthy();
  expect(backend.find('POST',confirm).map(req => req.body)).toEqual([{digest:'payload-digest'}]);
});
