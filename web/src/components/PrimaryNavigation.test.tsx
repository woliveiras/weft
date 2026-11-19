import { cleanup, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import { PrimaryNavigation } from './PrimaryNavigation';

afterEach(cleanup);
it('groups destinations with projects available', async () => {
  const navigate = vi.fn();
  render(<PrimaryNavigation active="graphs" pending={2} onNavigate={navigate}/>);
  expect((screen.getByRole('button', {name:'Tasks'}) as HTMLButtonElement).disabled).toBe(false);
  expect((screen.getByRole('button', {name:'Projects'}) as HTMLButtonElement).disabled).toBe(false);
  expect(screen.getByRole('button', {name:'Workflows'}).getAttribute('aria-current')).toBe('page');
  await userEvent.click(screen.getByRole('button',{name:'Projects'}));
  expect(navigate).toHaveBeenCalledWith('projects');
  const work = screen.getByRole('region', {name:'Work'});
  expect(within(work).queryByRole('button', {name:'Projects'})).toBeNull();
  expect(within(screen.getByRole('region', {name:'Configuration'})).getByRole('button', {name:'Projects'})).toBeTruthy();
  await userEvent.click(within(work).getByRole('button', {name:'Activity'}));
  expect(navigate).toHaveBeenCalledWith('activity');
  await userEvent.click(within(work).getByRole('button', {name:'Approvals 2'}));
  expect(navigate).toHaveBeenCalledWith('approvals');
  expect(within(work).queryByRole('button', {name:'Models'})).toBeNull();
});
it('keeps configuration visible across route changes', () => {
  const navigate = vi.fn();
  const {rerender} = render(<PrimaryNavigation active="connections" pending={0} onNavigate={navigate}/>);
  expect(screen.getByRole('heading', {name:'Configuration'})).toBeTruthy();
  expect(screen.queryByRole('button', {name:'Configuration'})).toBeNull();
  expect(screen.getByRole('button', {name:'Connections'}).getAttribute('aria-current')).toBe('page');
  rerender(<PrimaryNavigation active="models" pending={0} onNavigate={navigate}/>);
  expect(screen.getByRole('button', {name:'Models'}).getAttribute('aria-current')).toBe('page');
  expect(screen.getByRole('button', {name:'Connections'})).toBeTruthy();
});
it('omits an empty count and sends exact configuration destinations', async () => {
  const navigate = vi.fn();
  render(<PrimaryNavigation active="approvals" pending={0} onNavigate={navigate}/>);
  expect(screen.getByRole('button', {name:'Approvals'}).getAttribute('aria-current')).toBe('page');
  for (const [label, destination] of [['Projects','projects'],['Workflows','graphs'],['Agents','agents'],['Connections','connections'],['Models','models'],['Skills & instructions','skills'],['Administration','administration']]) {
    await userEvent.click(screen.getByRole('button', {name:label}));
    expect(navigate).toHaveBeenLastCalledWith(destination);
  }
});
