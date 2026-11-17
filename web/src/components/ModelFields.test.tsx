import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ModelRef, ProviderInfo } from '../api/types';
import { apiError, fakeApi } from '../test/fakeApi';
import { ModelFields } from './ModelFields';

const providers: ProviderInfo[] = [
  { name: 'mock', type: 'mock', default: true, defaultModel: 'mock-1', status: 'ready' },
  { name: 'team', type: 'openai', defaultModel: 'gpt-x', status: 'ready' },
  { name: 'broken', type: 'openai', status: 'unavailable', error: 'stored credential cannot be decrypted' },
];

afterEach(cleanup);

function setup(model?: ModelRef, inherited?: ModelRef) {
  const api = fakeApi({
    'GET /api/v1/providers': () => providers,
    'GET /api/v1/providers/mock/models': () => [{ id: 'mock-1', provider: 'mock' }],
    'GET /api/v1/providers/team/models': () => [
      { id: 'gpt-x', provider: 'team' },
      { id: 'gpt-y', provider: 'team' },
    ],
  });
  const onChange = vi.fn();
  render(<ModelFields model={model} inherited={inherited} onChange={onChange} />);
  return { api, onChange };
}

describe('ModelFields', () => {
  it('offers configured providers and selects one for the agent', async () => {
    const { onChange } = setup();
    const select = (await screen.findByRole('combobox', { name: /Provider/ })) as HTMLSelectElement;
    await waitFor(() => expect(select.options.length).toBe(4));
    expect(select.options[0].textContent).toBe('Default provider (mock)');
    expect(select.options[3].textContent).toBe('broken — unavailable');
    await userEvent.setup().selectOptions(select, 'team');
    expect(onChange).toHaveBeenCalledWith({ provider: 'team' });
  });

  it('suggests the selected provider models and keeps free text', async () => {
    const { onChange } = setup({ provider: 'team' });
    expect(await screen.findByText('2 models available from team.')).toBeTruthy();
    const options = [...document.querySelectorAll('datalist option')].map((o) => (o as HTMLOptionElement).value);
    expect(options).toEqual(['gpt-x', 'gpt-y']);
    const input = screen.getByRole('combobox', { name: /Model/ });
    await userEvent.setup().type(input, 'z');
    expect(onChange).toHaveBeenLastCalledWith({ provider: 'team', name: 'z' });
  });

  it('removes the model reference when everything is cleared', async () => {
    const { onChange } = setup({ provider: 'team' });
    await userEvent.setup().selectOptions(await screen.findByRole('combobox', { name: /Provider/ }), '');
    expect(onChange).toHaveBeenCalledWith(undefined);
  });

  it('warns about a provider that is not configured without losing the value', async () => {
    setup({ provider: 'gone' });
    expect((await screen.findByRole('alert')).textContent).toMatch(/“gone” is not configured/);
    const select = screen.getByRole('combobox', { name: /Provider/ }) as HTMLSelectElement;
    expect(select.value).toBe('gone');
  });

  it('explains why a selected provider is unavailable and does not query its models', async () => {
    const { api } = setup({ provider: 'broken' });
    expect((await screen.findByRole('alert')).textContent).toMatch(/cannot be decrypted/);
    expect(api.find('GET', '/api/v1/providers/broken/models')).toHaveLength(0);
  });

  it('uses graph defaults as the inherited choice', async () => {
    setup(undefined, { provider: 'team', name: 'gpt-y' });
    const select = (await screen.findByRole('combobox', { name: /Provider/ })) as HTMLSelectElement;
    expect(select.options[0].textContent).toBe('Graph default (team)');
    expect((screen.getByRole('combobox', { name: /Model/ }) as HTMLInputElement).placeholder).toBe('gpt-y');
  });

  it('reports a model listing failure as a hint, not a blocker', async () => {
    fakeApi({
      'GET /api/v1/providers': () => providers,
      'GET /api/v1/providers/team/models': () => apiError(404, 'llm team: HTTP 404', 'not_found'),
    });
    render(<ModelFields model={{ provider: 'team' }} onChange={() => undefined} />);
    expect(await screen.findByText(/Could not list models \(llm team: HTTP 404\); type the model name/)).toBeTruthy();
  });
});
