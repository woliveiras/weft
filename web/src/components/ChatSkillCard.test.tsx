import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { ChatSkillCard } from './ChatSkillCard';

afterEach(cleanup);
it('shows the existing audience and changed content before saving the revision', () => {
  const decide = vi.fn();
  const before = {id:'review-changes',title:'Review',body:'Check labels.',description:'',enabled:true,global:true};
  render(<ChatSkillCard review={{id:'op',digest:'digest',state:'awaiting_confirmation',expiresAt:'2027-01-01',
    skill:{before,after:{...before,body:'Review tests.'},revision:2}}} now={0} canConfirm onDecide={decide} onView={vi.fn()}/>);
  expect(screen.getByText('Check labels.')).toBeTruthy();
  expect(screen.getByText('Review tests.')).toBeTruthy();
  expect(screen.getByText(/Enabled · Installation defaults/)).toBeTruthy();
  expect(screen.getByRole('link', {name:'View skill'}).getAttribute('href')).toBe('#/skills/s/review-changes/content');
  fireEvent.click(screen.getByRole('button', {name:'Save skill revision'}));
  expect(decide).toHaveBeenCalledWith('confirm');
});
