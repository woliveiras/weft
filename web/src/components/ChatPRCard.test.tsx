import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import type { ChatPRReview } from '../api/chat';
import { ChatPRCard } from './ChatPRCard';

afterEach(cleanup);
const proposal: ChatPRReview = {
  id: 'review', digest: 'abc', state: 'awaiting_confirmation', expiresAt: '2027-01-01',
  pr: { number: 418, provider: 'github', connection: 'origin', repository: 'team/repo', head: 'head', base: 'base',
    graph: 'reviews', node: 'reader', version: 3, modelProvider: 'local', modelName: 'reader' },
};

it('keeps inspection and confirmation separate', () => {
  const decide = vi.fn(), view = vi.fn();
  render(<ChatPRCard review={proposal} now={0} canConfirm onDecide={decide} onView={view}/>);
  const link = screen.getByRole('link', { name: 'View workflow' });
  expect(link.getAttribute('href')).toBe('#/g/reviews/canvas?sel=node%3Areader');
  fireEvent.click(link);
  expect(decide).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Analyze PR #418' }));
  expect(decide).toHaveBeenCalledWith('confirm');
});

it('links a completed draft to the observed execution', () => {
  render(<ChatPRCard review={{...proposal, state: 'applied', prSummary: 'Check the empty input.', resultId: 'one'}}
    now={0} canConfirm={false} onDecide={vi.fn()} onView={vi.fn()}/>);
  expect(screen.getByText('Check the empty input.')).toBeTruthy();
  expect(screen.getByRole('link', {name: 'View execution'}).getAttribute('href')).toBe('#/g/reviews/runs?execution=one&section=result');
  expect(screen.queryByRole('button')).toBeNull();
});
