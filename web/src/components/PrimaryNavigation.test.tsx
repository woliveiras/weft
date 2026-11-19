import { cleanup, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { NAVIGATION_COMPACT_KEY, PrimaryNavigation } from './PrimaryNavigation';

beforeEach(()=>localStorage.removeItem(NAVIGATION_COMPACT_KEY));
afterEach(()=>{cleanup();vi.restoreAllMocks();});
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

it('toggles a persistent icon rail while keeping destinations, focus and approval counts accessible', async()=>{
  const user=userEvent.setup(); const navigate=vi.fn(); const view=render(<PrimaryNavigation active="graphs" pending={3} onNavigate={navigate}/>);
  const toggle=screen.getByRole('button',{name:'Collapse navigation'});
  toggle.focus(); await user.keyboard('{Enter}');
  expect(screen.getByRole('button',{name:'Expand navigation'})).toBe(document.activeElement);
  expect(toggle.getAttribute('aria-expanded')).toBe('false');
  expect(localStorage.getItem(NAVIGATION_COMPACT_KEY)).toBe('true');
  expect(screen.getByRole('navigation').className).toContain('is-compact');
  const workflows=screen.getByRole('button',{name:'Workflows'}); expect(workflows.getAttribute('aria-current')).toBe('page');
  await user.hover(workflows); expect(screen.getByRole('tooltip').textContent).toBe('Workflows');
  await user.keyboard('{Escape}'); expect(screen.queryByRole('tooltip')).toBeNull();
  await user.click(screen.getByRole('button',{name:'Approvals 3'})); expect(navigate).toHaveBeenCalledWith('approvals');
  view.unmount(); render(<PrimaryNavigation active="graphs" pending={0} onNavigate={navigate}/>);
  expect(screen.getByRole('button',{name:'Expand navigation'})).toBeTruthy();
  await user.click(screen.getByRole('button',{name:'Expand navigation'}));
  expect(screen.getByRole('navigation').className).not.toContain('is-compact');
  expect(localStorage.getItem(NAVIGATION_COMPACT_KEY)).toBe('false');
});
it('still toggles when browser storage cannot be read or written',async()=>{
  vi.spyOn(Storage.prototype,'getItem').mockImplementation(()=>{throw new DOMException('Blocked','SecurityError');});
  vi.spyOn(Storage.prototype,'setItem').mockImplementation(()=>{throw new DOMException('Full','QuotaExceededError');});
  render(<PrimaryNavigation active="graphs" pending={0} onNavigate={()=>undefined}/>);
  expect(screen.getByRole('status').textContent).toContain('could not be read');
  await userEvent.click(screen.getByRole('button',{name:'Collapse navigation'}));
  expect(screen.getByRole('navigation').className).toContain('is-compact');
  expect(screen.getByRole('status').textContent).toContain('could not be saved');
});
