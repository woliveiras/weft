import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../api/client';
import type { EnvironmentView } from '../api/types';
import { fakeApi } from '../test/fakeApi';
import { EnvironmentBlocker, environmentIssue } from './EnvironmentBlocker';

afterEach(cleanup);

const env = (over: Partial<EnvironmentView>): EnvironmentView => ({ graph: 'g', state: 'absent', defined: true, upToDate: false, ...over });
const status = { graphId: 'g', state: 'running', desiredState: 'running', nodes: [], channels: [] };

function renderBlocker(canBuild = true) {
  const onStarted = vi.fn();
  const onDismiss = vi.fn();
  render(<EnvironmentBlocker graphId="g" message="environment not ready: it has not been built yet" canBuild={canBuild} onStarted={onStarted} onDismiss={onDismiss} />);
  return { onStarted, onDismiss };
}

describe('environmentIssue', () => {
  it('extracts the spec.environment issue of a refused start only', () => {
    const refused = new ApiError(422, 'invalid', 'invalid', { errors: [{ path: 'spec.environment', message: 'not ready' }] });
    expect(environmentIssue(refused)).toBe('not ready');
    expect(environmentIssue(new ApiError(422, 'x', 'invalid', { errors: [{ path: 'spec.nodes[0].model.provider', message: 'm' }] }))).toBeUndefined();
    expect(environmentIssue(new ApiError(409, 'x', 'conflict'))).toBeUndefined();
    expect(environmentIssue(new Error('x'))).toBeUndefined();
  });
});

describe('EnvironmentBlocker', () => {
  it('builds and starts when the environment becomes ready', async () => {
    const user = userEvent.setup();
    let polls = 0;
    const api = fakeApi({
      'POST /api/v1/graphs/g/environment/build': () => env({ state: 'building' }),
      'GET /api/v1/graphs/g/environment': () => (++polls > 1 ? env({ state: 'ready', upToDate: true }) : env({ state: 'building' })),
      'POST /api/v1/graphs/g/start': () => status,
    });
    const { onStarted, onDismiss } = renderBlocker();
    expect(screen.getByText(/has not been built yet/)).toBeTruthy();
    await user.click(screen.getByRole('button', { name: 'Build and start when ready' }));
    expect(await screen.findByText(/The graph starts as soon as it is ready/)).toBeTruthy();
    await waitFor(() => expect(api.find('POST', '/api/v1/graphs/g/start')).toHaveLength(1), { timeout: 9000 });
    expect(onStarted).toHaveBeenCalledWith(status);
    expect(onDismiss).toHaveBeenCalled();
  }, 12000);

  it('only builds when asked to, and reports build failures', async () => {
    const user = userEvent.setup();
    const api = fakeApi({
      'POST /api/v1/graphs/g/environment/build': () => env({ state: 'ready', upToDate: true }),
    });
    renderBlocker();
    await user.click(screen.getByRole('button', { name: 'Build environment' }));
    expect(await screen.findByText('Environment ready. You can start the graph.')).toBeTruthy();
    expect(api.find('POST', '/api/v1/graphs/g/start')).toHaveLength(0);
    cleanup();

    fakeApi({
      'POST /api/v1/graphs/g/environment/build': () => env({ state: 'building' }),
      'GET /api/v1/graphs/g/environment': () => env({ state: 'failed', error: 'setup: exit status 1' }),
    });
    renderBlocker();
    await user.click(screen.getByRole('button', { name: 'Build and start when ready' }));
    expect(await screen.findByText(/The environment build failed: setup: exit status 1/, {}, { timeout: 6000 })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Build and start when ready' })).toBeTruthy();
  }, 12000);

  it('tells viewers to ask an operator', () => {
    renderBlocker(false);
    expect(screen.queryByRole('button', { name: /Build/ })).toBeNull();
    expect(screen.getByText('Ask an operator to build the environment.')).toBeTruthy();
  });
});
