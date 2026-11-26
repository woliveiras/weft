import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { StrictMode } from 'react';
import { afterEach, expect, it, vi } from 'vitest';
import type { ChatPRReview } from '../api/chat';
import { api } from '../api/client';
import { ChatPRCard } from './ChatPRCard';

afterEach(() => { cleanup(); vi.restoreAllMocks(); });
const proposal: ChatPRReview = {
  id: 'review', digest: 'abc', state: 'awaiting_confirmation', expiresAt: '2027-01-01',
  pr: { number: 418, provider: 'github', connection: 'origin', repository: 'team/repo', head: 'head', base: 'base',
    graph: 'reviews', node: 'reader', version: 3, modelProvider: 'local', modelName: 'reader' },
};

it('keeps inspection and confirmation separate', () => {
  const decide = vi.fn(), view = vi.fn();
  render(<ChatPRCard sessionId="session" review={proposal} now={0} canConfirm onDecide={decide} onView={view}/>);
  const link = screen.getByRole('link', { name: 'View workflow' });
  expect(link.getAttribute('href')).toBe('#/g/reviews/canvas?sel=node%3Areader');
  fireEvent.click(link);
  expect(decide).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Analyze PR #418' }));
  expect(decide).toHaveBeenCalledWith('confirm');
});

it('links a completed draft to the observed execution', () => {
  render(<ChatPRCard sessionId="session" review={{...proposal, state: 'applied', prSummary: 'Check the empty input.', resultId: 'one'}}
    now={0} canConfirm={false} onDecide={vi.fn()} onView={vi.fn()}/>);
  expect(screen.getByText('Check the empty input.')).toBeTruthy();
  expect(screen.getByRole('link', {name: 'View execution'}).getAttribute('href')).toBe('#/g/reviews/runs?execution=one&section=result');
  expect(screen.queryByRole('button', {name: 'Analyze PR #418'})).toBeNull();
});

it('fetches the pinned diff on demand, without confirming the analysis', async () => {
  const inspect = vi.spyOn(api, 'chatPRDiff').mockResolvedValue({head:'head', base:'base', diff:'+++ b/file.ts\n@@ -1 +1 @@\n+fixture line'});
  const decide = vi.fn();
  render(<StrictMode><ChatPRCard sessionId="session" review={proposal} now={0} canConfirm onDecide={decide} onView={vi.fn()}/></StrictMode>);
  expect(inspect).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', {name: 'Inspect pinned diff'}));
  expect(await screen.findByText(/\+fixture line/)).toBeTruthy();
  expect(inspect).toHaveBeenCalledExactlyOnceWith('session', 'review');
  expect(decide).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', {name: 'Hide pinned diff'}));
  expect(screen.queryByRole('region', {name: 'Pinned pull request diff'})).toBeNull();
});
