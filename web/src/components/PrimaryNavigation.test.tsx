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
  await userEvent.click(within(work).getByRole('button', {name:'Approvals 2'}));
  expect(navigate).toHaveBeenCalledWith('approvals');
  expect(within(work).queryByRole('button', {name:'Models'})).toBeNull();
});
it('supports keyboard disclosure and reveals the destination after route changes', async () => {
  const navigate = vi.fn();
  const {rerender} = render(<PrimaryNavigation active="connections" pending={0} onNavigate={navigate}/>);
  const toggle = screen.getByRole('button', {name:'Configuration'});
  toggle.focus();
  await userEvent.keyboard('{Enter}');
  expect(toggle.getAttribute('aria-expanded')).toBe('false');
  expect(screen.queryByRole('button', {name:'Connections'})).toBeNull();
  expect(navigate).not.toHaveBeenCalled();
  await userEvent.keyboard(' ');
  expect(toggle.getAttribute('aria-expanded')).toBe('true');
  await userEvent.click(toggle);
  rerender(<PrimaryNavigation active="models" pending={0} onNavigate={navigate}/>);
  expect(screen.getByRole('button', {name:'Models'}).getAttribute('aria-current')).toBe('page');
  expect(toggle.getAttribute('aria-expanded')).toBe('true');
});
it('omits an empty count and sends exact configuration destinations', async () => {
  const navigate = vi.fn();
  render(<PrimaryNavigation active="approvals" pending={0} onNavigate={navigate}/>);
  expect(screen.getByRole('button', {name:'Approvals'}).getAttribute('aria-current')).toBe('page');
  for (const [label, destination] of [['Workflows','graphs'],['Connections','connections'],['Models','models'],['Skills & instructions','skills'],['Administration','administration']]) {
    await userEvent.click(screen.getByRole('button', {name:label}));
    expect(navigate).toHaveBeenLastCalledWith(destination);
  }
});
