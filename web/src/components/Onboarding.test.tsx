import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { Onboarding } from './Onboarding';
import { progressKey } from './onboardingModel';

const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), {status, headers:{'Content-Type':'application/json'}});
const identity = { name: 'operator', role: 'operator' as const };
const show = () => fireEvent.click(screen.getByRole('button', { name: 'Getting started' }));
const topic = (title: string) => fireEvent.click(screen.getByRole('button', { name: new RegExp(title) }));
afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

it('persists review and skip across unmount, preserves completion on reopening, and confirms restart', () => {
  let view = render(<Onboarding identity={identity} onCreated={vi.fn()}/>);
  show(); fireEvent.click(screen.getByText('Mark reviewed and continue'));
  fireEvent.click(screen.getByText('Skip for now'));
  view.unmount();
  view = render(<Onboarding identity={identity} onCreated={vi.fn()}/>);
  show(); expect(screen.getByRole('heading', {name:'Choose a model'})).toBeTruthy();
  expect(screen.getByText('1/6 topics reviewed')).toBeTruthy();
  topic('Understand the workspace');
  for(let i=0;i<6;i++) fireEvent.click(screen.getByText('Mark reviewed and continue'));
  expect(screen.getByText('6/6 topics reviewed · Guide complete')).toBeTruthy();
  fireEvent.click(screen.getByText('Restart guide')); fireEvent.click(screen.getByText('Keep progress'));
  expect(screen.getByText('6/6 topics reviewed · Guide complete')).toBeTruthy();
  fireEvent.click(screen.getByText('Restart guide')); fireEvent.click(screen.getByText('Confirm restart'));
  expect(screen.getByText('0/6 topics reviewed')).toBeTruthy();
});

it('reports storage failure without claiming persistence', () => {
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {throw new Error('denied');});
  render(<Onboarding identity={identity} onCreated={vi.fn()}/>); show();
  fireEvent.click(screen.getByText('Mark reviewed and continue'));
  expect(screen.getByText(/Progress could not be saved/)).toBeTruthy();
});
it('reports corrupt progress and lets the user deliberately replace it', () => {
  localStorage.setItem(progressKey(identity.name, identity.role), '{}');
  render(<Onboarding identity={identity} onCreated={vi.fn()}/>); show();
  expect(screen.getByText(/Saved guide progress is invalid/)).toBeTruthy();
  fireEvent.click(screen.getByText('Restart guide')); fireEvent.click(screen.getByText('Confirm restart'));
  expect(JSON.parse(localStorage.getItem(progressKey(identity.name, identity.role))!).reviewed).toEqual([]);
});

it('integrates prerequisite reads with API errors and retry without marking topics reviewed', async () => {
  const fetcher = vi.fn().mockResolvedValueOnce(json({error:'unavailable'}, 503))
    .mockResolvedValueOnce(json([]));
  vi.stubGlobal('fetch', fetcher);
  render(<Onboarding identity={identity} onCreated={vi.fn()}/>); show(); topic('Choose a model');
  await screen.findByText(/Could not check prerequisites/);
  fireEvent.click(screen.getByText('Refresh prerequisites'));
  await screen.findByText(/No model providers configured/);
  expect(screen.getByText('0/6 topics reviewed')).toBeTruthy();
  expect(fetcher.mock.calls.every(([url]) => String(url).endsWith('/providers'))).toBe(true);
});
it('validates the example before creation, handles failure, and never starts it', async () => {
  const fetcher = vi.fn().mockResolvedValueOnce(json({valid:false}))
    .mockResolvedValueOnce(json({valid:true}))
    .mockResolvedValueOnce(json({error:'denied'},403))
    .mockResolvedValueOnce(json({valid:true}))
    .mockImplementationOnce((_url, options) => Promise.resolve(json({graph:JSON.parse(options.body)})));
  vi.stubGlobal('fetch', fetcher);
  const created = vi.fn();
  render(<Onboarding identity={identity} onCreated={created}/>); show(); topic('Create and review');
  fireEvent.click(screen.getByText('Create echo example'));
  await screen.findByText(/Example validation failed/);
  expect(fetcher).toHaveBeenCalledTimes(1);
  fireEvent.click(screen.getByText('Create echo example'));
  await screen.findByText(/denied/);
  expect(created).not.toHaveBeenCalled();
  fireEvent.click(screen.getByText('Create echo example'));
  await waitFor(() => expect(created).toHaveBeenCalledTimes(1));
  expect(fetcher.mock.calls.some(([url]) => /start|actions/.test(String(url)))).toBe(false);
  const writes = fetcher.mock.calls.filter(([url]) => String(url).endsWith('/graphs'));
  expect(JSON.parse(writes[0][1].body).metadata.id).toBe(JSON.parse(writes[1][1].body).metadata.id);
});
it('allows viewers to review help but never create examples', () => {
  render(<Onboarding identity={{name:'viewer', role:'viewer'}} onCreated={vi.fn()}/>); show(); topic('Create and review');
  expect((screen.getByText('Create echo example') as HTMLButtonElement).disabled).toBe(true);
});
