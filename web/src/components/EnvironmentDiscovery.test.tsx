import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { apiError, fakeApi } from '../test/fakeApi';
import { EnvironmentDiscovery } from './EnvironmentDiscovery';

const discoveryPath = '/api/v1/graphs/g/environment/discovery';
const resolvePath = '/api/v1/graphs/g/environment/resolve';
const candidate = { path: 'compose.yaml', sha256: 'hash', services: ['dev', 'db'] };
afterEach(cleanup);

describe('EnvironmentDiscovery', () => {
  it('requires explicit file and service selection, reviews, then accepts only into the draft', async () => {
    const user = userEvent.setup();
    const env = { image: 'node:22', origin: { source: 'compose', path: candidate.path, sha256: 'hash', service: 'dev', files: { 'compose.yaml': 'hash' } } };
    const client = fakeApi({
      [`GET ${discoveryPath}`]: () => ({ source: 'compose', status: 'review_required', candidates: [candidate] }),
      [`POST ${resolvePath}`]: () => env,
    });
    const onAccept = vi.fn();
    render(<EnvironmentDiscovery graphId="g" disabled={false} onAccept={onAccept} />);
    await user.click(screen.getByRole('button', { name: 'Discover environment' }));
    await user.selectOptions(await screen.findByLabelText('Repository definition'), 'compose.yaml');
    expect((screen.getByRole('button', { name: 'Review environment' }) as HTMLButtonElement).disabled).toBe(true);
    await user.selectOptions(screen.getByLabelText('Development service'), 'dev');
    await user.click(screen.getByRole('button', { name: 'Review environment' }));
    expect((await screen.findByLabelText('Environment proposal')).textContent).toContain('node:22');
    expect(onAccept).not.toHaveBeenCalled();
    expect(client.find('POST', resolvePath)[0].body).toEqual({ source: 'compose', path: 'compose.yaml', sha256: 'hash', service: 'dev' });
    await user.click(screen.getByRole('button', { name: 'Accept into draft' }));
    expect(onAccept).toHaveBeenCalledWith(env);
    expect(client.calls).toHaveLength(2);
  });

  it('invalidates a preview when the selection changes', async () => {
    const user = userEvent.setup();
    fakeApi({
      [`GET ${discoveryPath}`]: () => ({ source: 'compose', status: 'review_required', candidates: [candidate] }),
      [`POST ${resolvePath}`]: () => ({ image: 'node:22' }),
    });
    render(<EnvironmentDiscovery graphId="g" disabled={false} onAccept={vi.fn()} />);
    await user.click(screen.getByRole('button', { name: 'Discover environment' }));
    await user.selectOptions(await screen.findByLabelText('Repository definition'), 'compose.yaml');
    await user.selectOptions(screen.getByLabelText('Development service'), 'dev');
    await user.click(screen.getByRole('button', { name: 'Review environment' }));
    await screen.findByRole('button', { name: 'Accept into draft' });
    await user.selectOptions(screen.getByLabelText('Development service'), 'db');
    expect(screen.queryByRole('button', { name: 'Accept into draft' })).toBeNull();
  });

  it('shows validation errors and never enables acceptance after failure', async () => {
    const user = userEvent.setup();
    const onAccept = vi.fn();
    fakeApi({
      [`GET ${discoveryPath}`]: () => ({ source: 'dockerfile', status: 'review_required', candidates: [{ path: 'Dockerfile', sha256: 'hash' }] }),
      [`POST ${resolvePath}`]: () => apiError(400, 'Selected file changed; discover again', 'invalid'),
    });
    render(<EnvironmentDiscovery graphId="g" disabled={false} onAccept={onAccept} />);
    await user.click(screen.getByRole('button', { name: 'Discover environment' }));
    await user.selectOptions(await screen.findByLabelText('Repository definition'), 'Dockerfile');
    await user.click(screen.getByRole('button', { name: 'Review environment' }));
    expect(await screen.findByText(/Selected file changed/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Accept into draft' })).toBeNull();
    expect(onAccept).not.toHaveBeenCalled();
  });

  it('offers analysis for absent definitions without claiming it already ran', async () => {
    fakeApi({ [`GET ${discoveryPath}`]: () => ({ source: 'none', status: 'proposal_required', candidates: [] }), 'GET /api/v1/graphs/g/environment/proposal': () => null });
    render(<EnvironmentDiscovery graphId="g" disabled={false} onAccept={vi.fn()} />);
    await userEvent.setup().click(screen.getByRole('button', { name: 'Discover environment' }));
    expect(await screen.findByRole('button', { name: 'Analyze repository' })).toBeTruthy();
  });

  it('blocks invalid candidates and discovery when an explicit definition exists', async () => {
    fakeApi({ [`GET ${discoveryPath}`]: () => ({ source: 'weft', status: 'invalid', candidates: [{ path: '.weft/environment.yaml', sha256: '', error: 'Invalid YAML' }] }) });
    const { rerender } = render(<EnvironmentDiscovery graphId="g" disabled={false} onAccept={vi.fn()} />);
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Discover environment' }));
    await user.selectOptions(await screen.findByLabelText('Repository definition'), '.weft/environment.yaml');
    expect(screen.getByText('Invalid YAML')).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Review environment' }) as HTMLButtonElement).disabled).toBe(true);
    rerender(<EnvironmentDiscovery graphId="g" disabled onAccept={vi.fn()} />);
    expect((screen.getByRole('button', { name: 'Discover environment' }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('shows unavailable checkout failures without a stale preview', async () => {
    fakeApi({ [`GET ${discoveryPath}`]: () => apiError(400, 'Workspace is unavailable', 'invalid') });
    render(<EnvironmentDiscovery graphId="g" disabled={false} onAccept={vi.fn()} />);
    await userEvent.setup().click(screen.getByRole('button', { name: 'Discover environment' }));
    expect(await screen.findByText(/Workspace is unavailable/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Accept into draft' })).toBeNull();
  });
});

it('reviews a named build stage and invalidates acceptance when the stage changes', async () => {
  const user = userEvent.setup();
  const env = { dockerfile: 'FROM alpine AS build\nFROM alpine AS runtime', target: 'build' };
  const client = fakeApi({
    [`GET ${discoveryPath}`]: () => ({ source: 'dockerfile', status: 'review_required', candidates: [{ path: 'Dockerfile', sha256: 'hash', targets: ['build', 'runtime'] }] }),
    [`POST ${resolvePath}`]: () => env,
  });
  const onAccept = vi.fn();
  render(<EnvironmentDiscovery graphId="g" disabled={false} onAccept={onAccept} />);
  await user.click(screen.getByRole('button', { name: 'Discover environment' }));
  await user.selectOptions(await screen.findByLabelText('Repository definition'), 'Dockerfile');
  await user.selectOptions(screen.getByLabelText('Build stage'), 'build');
  await user.click(screen.getByRole('button', { name: 'Review environment' }));
  await screen.findByRole('button', { name: 'Accept into draft' });
  expect(client.find('POST', resolvePath)[0].body).toEqual({ source: 'dockerfile', path: 'Dockerfile', sha256: 'hash', target: 'build' });
  expect(screen.getByLabelText('Environment proposal').textContent).toContain('"target": "build"');
  await user.selectOptions(screen.getByLabelText('Build stage'), 'runtime');
  expect(screen.queryByRole('button', { name: 'Accept into draft' })).toBeNull();
  expect(onAccept).not.toHaveBeenCalled();
  await user.selectOptions(screen.getByLabelText('Build stage'), '');
  await user.click(screen.getByRole('button', { name: 'Review environment' }));
  await screen.findByRole('button', { name: 'Accept into draft' });
  expect(client.find('POST', resolvePath)[1].body).toEqual({ source: 'dockerfile', path: 'Dockerfile', sha256: 'hash' });
  await user.click(screen.getByRole('button', { name: 'Accept into draft' }));
  expect(onAccept).toHaveBeenCalledWith(env);
});

it('requires a fresh review of auxiliary services before accepting a Compose environment', async () => {
  const user = userEvent.setup();
  const env = { image: 'alpine', companions: [{ name: 'cache', image: 'redis:7' }] };
  fakeApi({ [`GET ${discoveryPath}`]: () => ({ source: 'compose', status: 'review_required', candidates: [candidate] }), [`POST ${resolvePath}`]: () => env });
  const onAccept = vi.fn();
  render(<EnvironmentDiscovery graphId="g" disabled={false} onAccept={onAccept} />);
  await user.click(screen.getByRole('button', { name: 'Discover environment' }));
  await user.selectOptions(await screen.findByLabelText('Repository definition'), 'compose.yaml');
  await user.selectOptions(screen.getByLabelText('Development service'), 'dev');
  await user.click(screen.getByRole('button', { name: 'Review environment' }));
  const accept = await screen.findByRole('button', { name: 'Accept into draft' });
  expect(accept).toHaveProperty('disabled', true);
  expect(screen.getByRole('region', { name: 'Review auxiliary services' }).textContent).toContain('redis:7');
  await user.click(screen.getByRole('checkbox'));
  expect(accept).toHaveProperty('disabled', false);
  await user.selectOptions(screen.getByLabelText('Development service'), 'db');
  await user.click(screen.getByRole('button', { name: 'Review environment' }));
  expect(await screen.findByRole('button', { name: 'Accept into draft' })).toHaveProperty('disabled', true);
  await user.click(screen.getByRole('checkbox'));
  await user.click(screen.getByRole('button', { name: 'Accept into draft' }));
  expect(onAccept).toHaveBeenCalledWith(env);
});
