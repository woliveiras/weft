import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { ChatManualLink } from './ChatManualLink';

afterEach(cleanup);

it('uses the known local page and remembers the originating link', () => {
  const onView = vi.fn();
  render(<ChatManualLink destination="budgets" message="reply-1" onView={onView}/>);
  const link = screen.getByRole('link', { name: 'Open Budgets' });
  expect(link.getAttribute('href')).toBe('#/models/budgets');
  fireEvent.click(link);
  expect(onView).toHaveBeenCalledWith(link.id);
});

it('ignores unrecognized destinations in a stored message', () => {
  render(<ChatManualLink destination="https://example.org" message="reply-1" onView={vi.fn()}/>);
  expect(screen.queryByRole('link')).toBeNull();
});
