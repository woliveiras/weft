import { cleanup, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it } from 'vitest';
import { fakeApi, apiError } from '../test/fakeApi';
import { NetworkPolicySection } from './NetworkPolicySection';
import type { NetworkPolicyView } from '../api/types';

const current: NetworkPolicyView = { revision: 2, source: 'installation', network: { setupNetworks: ['none', 'egress'], runNetworks: ['none'], egressHosts: ['example.com'] }, usage: [{ graph: 'project', state: 'running', hosts: ['example.com'], blocked: false }] };
afterEach(cleanup);

it('shows policy and host usage to viewers without write controls', async () => {
  const backend = fakeApi({ 'GET /api/v1/network-policy': () => current });
  render(<NetworkPolicySection canManage={false} />);
  expect(await screen.findByText('project')).toBeTruthy();
  expect(screen.getByText(/Administrator access/)).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Review network policy' })).toBeNull();
  expect((screen.getByRole('textbox') as HTMLTextAreaElement).disabled).toBe(true);
  expect(backend.calls).toHaveLength(1);
});

it('previews the draft, confirms the reviewed revision and refreshes persisted settings', async () => {
  const user = userEvent.setup();
  let saved = false;
  const backend = fakeApi({
    'GET /api/v1/network-policy': () => saved ? { ...current, revision: 3, network: { ...current.network, egressHosts: [] } } : current,
    'POST /api/v1/network-policy/preview': ({ body }) => ({ ...current, ...(body as object), usage: [{ ...current.usage![0], blocked: true, reason: 'host removed' }] }),
    'PUT /api/v1/network-policy': ({ body }) => { saved = true; return { ...current, ...(body as object), revision: 3 }; },
  });
  render(<NetworkPolicySection canManage />);
  await user.clear(await screen.findByRole('textbox'));
  await user.click(screen.getByRole('button', { name: 'Review network policy' }));
  const confirmation = await screen.findByRole('region', { name: 'Confirm network policy' });
  expect(within(confirmation).getByText(/host removed/)).toBeTruthy();
  expect(backend.find('PUT', '/api/v1/network-policy')).toHaveLength(0);
  await user.click(within(confirmation).getByRole('button', { name: 'Confirm network policy' }));
  expect(await screen.findByRole('status')).toHaveProperty('textContent', expect.stringContaining('Network policy saved'));
  expect(backend.find('PUT', '/api/v1/network-policy')[0].body).toEqual({ revision: 2, network: { ...current.network, egressHosts: [] } });
});

it('reports conflicts and invalidates confirmation instead of resubmitting a stale revision', async () => {
  const user = userEvent.setup();
  fakeApi({
    'GET /api/v1/network-policy': () => current,
    'POST /api/v1/network-policy/preview': () => current,
    'PUT /api/v1/network-policy': () => apiError(409, 'revision changed', 'conflict'),
  });
  render(<NetworkPolicySection canManage />);
  await screen.findByRole('textbox');
  await user.click(screen.getByRole('button', { name: 'Review network policy' }));
  await user.click(await screen.findByRole('button', { name: 'Confirm network policy' }));
  expect(await screen.findByText(/Save could not be confirmed/)).toBeTruthy();
  expect(screen.queryByRole('region', { name: 'Confirm network policy' })).toBeNull();
});

it('discards pending approval on role change and handles loading failures', async () => {
  const user = userEvent.setup();
  fakeApi({ 'GET /api/v1/network-policy': () => current, 'POST /api/v1/network-policy/preview': () => current });
  const view = render(<NetworkPolicySection canManage />);
  await screen.findByRole('textbox');
  await user.click(screen.getByRole('button', { name: 'Review network policy' }));
  await screen.findByRole('region', { name: 'Confirm network policy' });
  view.rerender(<NetworkPolicySection canManage={false} />);
  expect(screen.queryByRole('region', { name: 'Confirm network policy' })).toBeNull();
  view.unmount();
  fakeApi({ 'GET /api/v1/network-policy': () => apiError(500, 'storage unavailable', 'internal') });
  render(<NetworkPolicySection canManage />);
  expect(await screen.findByText('storage unavailable')).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Review network policy' })).toBeNull();
});

it('keeps invalid drafts editable and can cancel a preview without writing', async () => {
  const user = userEvent.setup();
  let invalid = true;
  const backend = fakeApi({
    'GET /api/v1/network-policy': () => current,
    'POST /api/v1/network-policy/preview': () => invalid ? apiError(400, 'invalid host', 'invalid') : current,
  });
  render(<NetworkPolicySection canManage />);
  const hosts = await screen.findByRole('textbox');
  await user.clear(hosts);
  await user.type(hosts, '*.example.com');
  await user.click(screen.getByRole('button', { name: 'Review network policy' }));
  expect(await screen.findByText(/Preview failed: invalid host/)).toBeTruthy();
  expect((hosts as HTMLTextAreaElement).disabled).toBe(false);
  invalid = false;
  await user.clear(hosts);
  await user.type(hosts, 'example.com');
  await user.click(screen.getByRole('button', { name: 'Review network policy' }));
  await user.click(await screen.findByRole('button', { name: 'Cancel network changes' }));
  expect(screen.queryByRole('region', { name: 'Confirm network policy' })).toBeNull();
  expect(backend.find('PUT', '/api/v1/network-policy')).toHaveLength(0);
});
