import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { EnvironmentView, Graph } from '../api/types';
import { EventStreamProvider } from '../hooks/useEventStream';
import { apiError, fakeApi } from '../test/fakeApi';
import { EnvironmentSection, environmentUsers } from './EnvironmentSection';

class FakeEventSource {
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  addEventListener() {}
  close() {}
}

const graph = (env?: Graph['spec']['environment']): Graph => ({
  apiVersion: 'weft.io/v1',
  kind: 'Graph',
  metadata: { id: 'g', name: 'G', version: 2 },
  spec: {
    environment: env,
    nodes: [
      { id: 'builder', isolation: { mode: 'docker' } },
      { id: 'own-image', isolation: { mode: 'docker', image: 'alpine' } },
      { id: 'local' },
    ],
  },
});

const view = (over: Partial<EnvironmentView> = {}): EnvironmentView => ({
  graph: 'g', state: 'absent', defined: true, currentHash: 'abc123', upToDate: false, ...over,
});

function renderSection(props: Partial<Parameters<typeof EnvironmentSection>[0]> = {}) {
  const env = { image: 'golang:1.26', setup: ['go mod download'] };
  const onChange = vi.fn();
  render(
    <EventStreamProvider token="t">
      <EnvironmentSection
        graph={graph(env)}
        savedGraph={graph(env)}
        dirty={false}
        running={false}
        canBuild
        canReset
        onChange={onChange}
        {...props}
      />
    </EventStreamProvider>,
  );
  return { onChange, status: () => screen.getByRole('region', { name: 'Environment status' }) };
}

beforeEach(() => {
  vi.stubGlobal('EventSource', FakeEventSource);
  vi.stubGlobal('confirm', vi.fn(() => true));
});
afterEach(cleanup);

describe('EnvironmentSection', () => {
  it('lists the agents that use the environment', () => {
    expect(environmentUsers(graph())).toEqual(['builder']);
  });

  it('builds a saved definition and shows the result', async () => {
    const user = userEvent.setup();
    const api = fakeApi({
      'GET /api/v1/graphs/g/environment': () => view(),
      'POST /api/v1/graphs/g/environment/build': () =>
        view({ state: 'ready', upToDate: true, hash: 'abc123', image: 'weft-env/g:abc123', finishedAt: '2026-10-14T10:00:00Z', actor: 'admin', log: '==> Environment ready' }),
    });
    const { status } = renderSection();
    expect(await within(status()).findByText('Not built')).toBeTruthy();
    expect(within(status()).getByText('Used by: builder.')).toBeTruthy();
    await user.click(within(status()).getByRole('button', { name: 'Build' }));
    expect(api.find('POST', '/api/v1/graphs/g/environment/build')[0].body).toEqual({ force: false });
    expect(await within(status()).findByText('Ready')).toBeTruthy();
    expect(within(status()).getByText('weft-env/g:abc123')).toBeTruthy();
    // An up-to-date environment offers a forced rebuild.
    await user.click(within(status()).getByRole('button', { name: 'Rebuild' }));
    expect(api.find('POST', '/api/v1/graphs/g/environment/build')[1].body).toEqual({ force: true });
  });

  it('shows failures with the build log', async () => {
    fakeApi({
      'GET /api/v1/graphs/g/environment': () =>
        view({ state: 'failed', hash: 'abc123', error: 'setup: exit status 1: go: not found', log: '$ go mod download\nsh: go: not found' }),
    });
    const { status } = renderSection();
    expect(await within(status()).findByText('Build failed')).toBeTruthy();
    expect(within(status()).getByText('setup: exit status 1: go: not found')).toBeTruthy();
    expect(within(status()).getByLabelText('Environment build log').textContent).toContain('sh: go: not found');
    expect(within(status()).getByRole('button', { name: 'Build again' })).toBeTruthy();
  });

  it('warns when the image is outdated or the definition is unsaved', async () => {
    fakeApi({ 'GET /api/v1/graphs/g/environment': () => view({ state: 'ready', hash: 'old', upToDate: false, image: 'weft-env/g:old' }) });
    const edited = graph({ image: 'golang:1.26', setup: ['go mod download', 'go vet ./...'] });
    const { status } = renderSection({ graph: edited, dirty: true });
    expect(await within(status()).findByText('Outdated')).toBeTruthy();
    expect(within(status()).getByText(/Unsaved environment changes/)).toBeTruthy();
    expect((within(status()).getByRole('button', { name: 'Build' }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('applies roles and refuses reset while the graph runs', async () => {
    const api = fakeApi({
      'GET /api/v1/graphs/g/environment': () => view({ state: 'ready', upToDate: true, hash: 'abc123', image: 'weft-env/g:abc123' }),
      'POST /api/v1/graphs/g/environment/reset': () => apiError(409, 'graph g is running; stop it before resetting its environment', 'conflict'),
    });
    const { status } = renderSection({ canBuild: false, canReset: true, running: true });
    await within(status()).findByText('Ready');
    expect(within(status()).queryByRole('button', { name: /Build|Rebuild/ })).toBeNull();
    const reset = within(status()).getByRole('button', { name: 'Reset' }) as HTMLButtonElement;
    expect(reset.disabled).toBe(true);
    expect(within(status()).getByText('Stop the graph to reset its environment.')).toBeTruthy();
    cleanup();

    renderSection({ canBuild: false, canReset: false });
    await waitFor(() => expect(api.find('GET', '/api/v1/graphs/g/environment').length).toBeGreaterThan(1));
    expect(screen.queryByRole('button', { name: 'Reset' })).toBeNull();
  });

  it('edits the definition in the draft', async () => {
    const user = userEvent.setup();
    fakeApi({ 'GET /api/v1/graphs/g/environment': () => view({ defined: false }) });
    const onChange = vi.fn();
    render(
      <EventStreamProvider token="t">
        <EnvironmentSection graph={graph()} savedGraph={graph()} dirty={false} running={false} canBuild canReset onChange={onChange} />
      </EventStreamProvider>,
    );
    await user.selectOptions(screen.getByLabelText('Source'), 'image');
    const apply = (n: number) => onChange.mock.calls[n][0](graph()) as Graph;
    expect(apply(0).spec.environment).toEqual({ image: '' });
  });
  it('connects repository acceptance to the draft without saving or building', async () => {
    const user = userEvent.setup();
    const env = { image: 'alpine', origin: { source: 'weft', path: '.weft/environment.yaml', sha256: 'hash', files: { '.weft/environment.yaml': 'hash' } } };
    const client = fakeApi({
      'GET /api/v1/graphs/g/environment/discovery': () => ({ source: 'weft', status: 'review_required', candidates: [{ path: '.weft/environment.yaml', sha256: 'hash' }] }),
      'POST /api/v1/graphs/g/environment/resolve': () => env,
    });
    const { onChange } = renderSection({ graph: graph(), savedGraph: graph(), canAccept: true });
    await user.click(screen.getByRole('button', { name: 'Discover environment' }));
    await user.selectOptions(await screen.findByLabelText('Repository definition'), '.weft/environment.yaml');
    await user.click(screen.getByRole('button', { name: 'Review environment' }));
    await user.click(await screen.findByRole('button', { name: 'Accept into draft' }));
    expect(onChange.mock.calls[0][0](graph()).spec.environment).toEqual(env);
    expect(client.calls).toHaveLength(2);
  });

  it('does not offer repository acceptance to a build-only operator', () => {
    renderSection({ graph: graph(), savedGraph: graph(), canBuild: true, canAccept: false });
    expect(screen.queryByRole('button', { name: 'Discover environment' })).toBeNull();
  });

});


it('explains reuse, writable cache isolation and shared reset retention', async () => {
 fakeApi({ 'GET /api/v1/graphs/g/environment': () => view({ state: 'ready', upToDate: true, reuseKey: 'compatible', reused: true }) });
 const { status } = renderSection();
 expect(await within(status()).findByText(/Reused compatible prepared image/)).toBeTruthy();
 expect(within(status()).getByText(/Workspaces and writable caches remain isolated/)).toBeTruthy();
 expect(within(status()).getByText(/Reset preserves shared snapshots/)).toBeTruthy();
});

it('offers reviewed egress and preserves the explicit host list when editing it', async () => {
  fakeApi({ 'GET /api/v1/graphs/g/environment': () => view() });
  const env = { image: 'alpine', network: { run: 'egress' as const, allowedHosts: ['example.com'] } };
  const { onChange } = renderSection({ graph: graph(env), savedGraph: graph(env) });
  expect((screen.getByLabelText('Network for agents') as HTMLSelectElement).value).toBe('egress');
  expect((screen.getByLabelText('Allowed hosts (one exact DNS name per line)') as HTMLTextAreaElement).value).toBe('example.com');
  await userEvent.selectOptions(screen.getByLabelText('Network for agents'), 'none');
  expect(onChange.mock.calls.at(-1)?.[0](graph(env)).spec.environment?.network).toEqual({ run: 'none', allowedHosts: undefined });
});

it('edits preparation hosts independently from runtime hosts and clears only the preparation list', async () => {
  fakeApi({ 'GET /api/v1/graphs/g/environment': () => view() });
  const env = { image: 'alpine', network: { setup: 'egress' as const, setupAllowedHosts: ['registry.npmjs.org'], run: 'egress' as const, allowedHosts: ['example.com'] } };
  const { onChange } = renderSection({ graph: graph(env), savedGraph: graph(env) });
  expect((screen.getByLabelText('Network during build and setup') as HTMLSelectElement).value).toBe('egress');
  const field = screen.getByLabelText('Build and setup hosts (one exact DNS name per line)');
  expect((field as HTMLTextAreaElement).value).toBe('registry.npmjs.org');
  await userEvent.selectOptions(screen.getByLabelText('Network during build and setup'), 'none');
  expect(onChange.mock.calls.at(-1)?.[0](graph(env)).spec.environment?.network).toEqual({ ...env.network, setup: 'none', setupAllowedHosts: undefined });
});
