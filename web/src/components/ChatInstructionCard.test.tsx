import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { ChatInstructionCard } from './ChatInstructionCard';

afterEach(cleanup);
it('shows the local change and links to its selected workflow node', () => {
  const decide=vi.fn();
  render(<ChatInstructionCard now={0} canConfirm onDecide={decide} onView={vi.fn()}
    review={{id:'op',digest:'digest',state:'awaiting_confirmation',expiresAt:'2027-01-01T00:00:00Z',
      instructions:{graph:'release',node:'reader',before:'Review notes.',after:'Review tests.',version:2,catalogId:'shared',catalogVersion:1}}}/>);
  expect(screen.getByText('Review notes.')).toBeTruthy();
  expect(screen.getByText('Review tests.')).toBeTruthy();
  expect(screen.getByRole('link',{name:'View workflow'}).getAttribute('href')).toBe('#/g/release/canvas?sel=node%3Areader');
  fireEvent.click(screen.getByRole('button',{name:'Save workflow instructions'}));
  expect(decide).toHaveBeenCalledWith('confirm');
});
