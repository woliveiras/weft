import { cleanup, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type { ReadyChecks } from '../api/types';
import { fakeApi } from '../test/fakeApi';
import { IntegrationsView } from './IntegrationsView';

afterEach(cleanup);

function renderWith(ready: ReadyChecks) {
  fakeApi({
    'GET /healthz': () => ({ status: 'ok' }),
    'GET /readyz': () => ready,
    'GET /api/v1/tools': () => [],
    'GET /api/v1/providers': () => [],
    'GET /api/v1/provider-settings': () => ({ revision: 1, default: 'mock', secrets: { enabled: true, keySource: 'generated' }, types: [] }),
  });
  render(<IntegrationsView />);
  return screen.findByRole('article', { name: 'Sandbox runtime' });
}

describe('IntegrationsView sandbox runtime', () => {
  it('reports a reachable dedicated daemon', async () => {
    const card = await renderWith({ storage: 'ok', queue: 'ok', sandbox: 'ok' });
    expect(await within(card).findByText(/never on the host/)).toBeTruthy();
    expect(within(card).getByText('Connected')).toBeTruthy();
  });

  it('explains what an unconfigured daemon means for agents', async () => {
    const card = await renderWith({ storage: 'ok', queue: 'ok', sandbox: 'not configured' });
    expect(await within(card).findByText(/Agents with docker isolation cannot start/)).toBeTruthy();
    expect(within(card).getByText('Check needed')).toBeTruthy();
  });

  it('shows the daemon error without failing the other services', async () => {
    const error = 'docker daemon tcp://sandbox-daemon:2376 unreachable: Cannot connect';
    const card = await renderWith({ storage: 'ok', queue: 'ok', sandbox: error });
    expect(await within(card).findByText(error)).toBeTruthy();
    expect(within(card).getByText('Unavailable')).toBeTruthy();
    expect(screen.queryByText('One or more dependencies did not pass the readiness check.')).toBeNull();
  });
});
