import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import type { ChatGuidanceReview } from '../api/chat';
import { ChatGuidanceCard } from './ChatGuidanceCard';

afterEach(cleanup);
const review: ChatGuidanceReview={id:'op',digest:'review',expiresAt:'2027-01-01T00:00:00Z',state:'awaiting_confirmation',
  guidance:{graph:'work',node:'reader',replica:'any',run:'run-1',version:3,text:'Focus on tests.'}};

it('shows the run, replica and guidance before queuing it',()=>{
  const decide=vi.fn();
  render(<ChatGuidanceCard review={review} now={0} canConfirm onDecide={decide}/>);
  expect(screen.getByText('run-1')).toBeTruthy();
  expect(screen.getByText('any')).toBeTruthy();
  expect(screen.getByText('Focus on tests.')).toBeTruthy();
  fireEvent.click(screen.getByRole('button',{name:'Send guidance'}));
  expect(decide).toHaveBeenCalledWith('confirm');
});

it('shows a runtime pickup as a receipt',()=>{
  render(<ChatGuidanceCard review={{...review,state:'consumed'}} now={0} canConfirm onDecide={vi.fn()}/>);
  expect(screen.getByRole('status').textContent).toContain('receipt records pickup only');
  expect(screen.queryByRole('button')).toBeNull();
});
