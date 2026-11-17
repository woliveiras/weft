import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ProviderInfo, ProviderPlugin, ProviderSettings, ProviderTestResult } from '../api/types';
import { apiError, fakeApi } from '../test/fakeApi';
import { ProvidersSection } from './ProvidersSection';

const settings = (over: Partial<ProviderSettings> = {}): ProviderSettings => ({
  revision: 4,
  default: 'mock',
  secrets: { enabled: true, keySource: 'generated' },
  types: [
    { type: 'mock', label: 'Mock', keyRequired: false },
    { type: 'openai', label: 'OpenAI', keyRequired: true, defaultBaseUrl: 'https://api.openai.com/v1' },
  ],
  ...over,
});

const caps = { tools: true, json: 'native' as const, usage: true, modelListing: true, streaming: false };
const plugin = (over: Partial<ProviderPlugin>): ProviderPlugin => ({
  id: 'openai', label: 'OpenAI', builtin: true, local: false, key: 'required', capabilities: caps, enabled: true, instances: [], ...over,
});
const plugins = (over: Partial<Record<string, Partial<ProviderPlugin>>> = {}): ProviderPlugin[] => [
  plugin({ id: 'mock', label: 'Mock', local: true, key: 'none', devOnly: true, enabledBy: 'config', instances: ['mock'], ...over.mock }),
  plugin({ id: 'openai', enabledBy: 'console', instances: ['team'], ...over.openai }),
  plugin({ id: 'ollama', label: 'Ollama', local: true, key: 'none', enabled: false, ...over.ollama }),
];

const mockProvider: ProviderInfo = {
  name: 'mock', type: 'mock', default: true, source: 'config', editable: false, status: 'ready',
  credential: { configured: false, source: 'none' },
};
const managed: ProviderInfo = {
  name: 'team', type: 'openai', defaultModel: 'gpt-x', source: 'managed', editable: true, status: 'ready',
  credential: { configured: true, source: 'stored', updatedAt: '2026-10-01T10:00:00Z' },
};

const okTest: ProviderTestResult = {
  provider: 'team', ok: true, checkedAt: '2026-10-13T10:00:00Z', durationMs: 300, model: 'gpt-x',
  models: { ok: true, count: 2, defaultModelListed: true },
  inference: { ok: true, reply: 'OK', durationMs: 250 },
};

beforeEach(() => {
  vi.stubGlobal('confirm', () => true);
});
afterEach(cleanup);

function card(name: string) {
  return screen.getByRole('article', { name: `Provider ${name}` });
}

describe('ProvidersSection', () => {
  it('lists providers with credential state and role-appropriate actions', async () => {
    fakeApi({
      'GET /api/v1/providers': () => [mockProvider, managed],
      'GET /api/v1/provider-settings': () => settings(),
      'GET /api/v1/provider-plugins': () => plugins(),
    });
    const { rerender } = render(<ProvidersSection canEdit canTest />);
    expect(await screen.findByRole('article', { name: 'Provider team' })).toBeTruthy();
    expect(within(card('team')).getByText(/Stored encrypted in Weft/)).toBeTruthy();
    expect(within(card('mock')).getByText('Default')).toBeTruthy();
    // Config providers are read-only; managed ones are editable.
    expect(within(card('mock')).queryByRole('button', { name: 'Edit' })).toBeNull();
    expect(within(card('team')).getByRole('button', { name: 'Edit' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Add provider' })).toBeTruthy();

    rerender(<ProvidersSection canEdit={false} canTest={false} />);
    expect(screen.queryByRole('button', { name: 'Add provider' })).toBeNull();
    expect(within(card('team')).queryByRole('button', { name: 'Test connection' })).toBeNull();
    expect(within(card('team')).queryByRole('button', { name: 'Edit' })).toBeNull();
  });

  it('adds a provider, sending the key once with the settings revision', async () => {
    const list = [mockProvider];
    const api = fakeApi({
      'GET /api/v1/providers': () => list,
      'GET /api/v1/provider-settings': () => settings(),
      'GET /api/v1/provider-plugins': () => plugins(),
      'POST /api/v1/providers': (c) => {
        list.push({ ...managed, name: (c.body as { name: string }).name });
        return new Response(JSON.stringify(list[list.length - 1]), { status: 201, headers: { 'content-type': 'application/json' } });
      },
    });
    const user = userEvent.setup();
    render(<ProvidersSection canEdit canTest />);
    await user.click(await screen.findByRole('button', { name: 'Add provider' }));
    const dialog = screen.getByRole('dialog', { name: 'Add model provider' });
    await user.type(within(dialog).getByLabelText(/^Name/), 'team');
    await user.type(within(dialog).getByLabelText(/^Default model/), 'gpt-x');
    await user.type(within(dialog).getByLabelText(/^API key/), 'sk-secret-1');
    await user.click(within(dialog).getByRole('button', { name: 'Add provider' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    const [post] = api.find('POST', '/api/v1/providers');
    expect(post.headers['If-Match']).toBe('4');
    expect(post.body).toMatchObject({ name: 'team', type: 'openai', defaultModel: 'gpt-x', apiKey: { value: 'sk-secret-1' } });
    expect(await screen.findByRole('article', { name: 'Provider team' })).toBeTruthy();
    expect(document.body.textContent).not.toContain('sk-secret-1');
  });

  it('offers Vertex AI project and region only for plugins that declare them', async () => {
    const list = [mockProvider];
    const gemini = { type: 'gemini', label: 'Google Gemini', keyRequired: false, settings: ['contextWindow', 'project', 'region'] };
    const api = fakeApi({
      'GET /api/v1/providers': () => list,
      'GET /api/v1/provider-settings': () => settings({ types: [...settings().types, gemini] }),
      'GET /api/v1/provider-plugins': () => [...plugins(), plugin({ id: 'gemini', label: 'Google Gemini', key: 'optional' })],
      'POST /api/v1/providers': (c) => {
        const body = c.body as ProviderInfo;
        list.push({ ...body, source: 'managed', editable: true, status: 'ready', credential: { configured: false, source: 'none' } });
        return new Response(JSON.stringify(list[list.length - 1]), { status: 201, headers: { 'content-type': 'application/json' } });
      },
    });
    const user = userEvent.setup();
    render(<ProvidersSection canEdit canTest />);
    await user.click(await screen.findByRole('button', { name: 'Add provider' }));
    const dialog = screen.getByRole('dialog', { name: 'Add model provider' });
    expect(within(dialog).queryByLabelText(/^Vertex AI project/)).toBeNull();
    await user.selectOptions(within(dialog).getByLabelText(/^Type/), 'gemini');
    await user.type(within(dialog).getByLabelText(/^Name/), 'vertex');
    await user.type(within(dialog).getByLabelText(/^Default model/), 'gemini-x');
    await user.type(within(dialog).getByLabelText(/^Vertex AI project/), 'my-project');
    await user.type(within(dialog).getByLabelText(/^Vertex AI region/), 'europe-west4');
    await user.click(within(dialog).getByRole('button', { name: 'Add provider' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    const [post] = api.find('POST', '/api/v1/providers');
    expect(post.body).toMatchObject({ name: 'vertex', type: 'gemini', project: 'my-project', region: 'europe-west4' });
    expect((post.body as { apiKey?: unknown }).apiKey).toBeUndefined();
    expect(await within(await screen.findByRole('article', { name: 'Provider vertex' })).findByText('my-project · europe-west4')).toBeTruthy();
  });

  it('shows server validation next to the field and keeps the dialog open', async () => {
    fakeApi({
      'GET /api/v1/providers': () => [mockProvider],
      'GET /api/v1/provider-settings': () => settings(),
      'GET /api/v1/provider-plugins': () => plugins(),
      'POST /api/v1/providers': () =>
        apiError(422, 'invalid provider', 'invalid', { errors: [{ path: 'apiKey', message: 'an API key is required' }] }),
    });
    const user = userEvent.setup();
    render(<ProvidersSection canEdit canTest />);
    await user.click(await screen.findByRole('button', { name: 'Add provider' }));
    const dialog = screen.getByRole('dialog');
    await user.type(within(dialog).getByLabelText(/^Name/), 'team');
    await user.click(within(dialog).getByRole('button', { name: 'Add provider' }));
    const alert = await within(dialog).findByRole('alert');
    expect(alert.textContent).toBe('an API key is required');
    expect(alert.closest('label')?.textContent).toContain('API key');
  });

  it('validates locally before sending', async () => {
    const api = fakeApi({
      'GET /api/v1/providers': () => [mockProvider],
      'GET /api/v1/provider-settings': () => settings(),
      'GET /api/v1/provider-plugins': () => plugins(),
    });
    const user = userEvent.setup();
    render(<ProvidersSection canEdit canTest />);
    await user.click(await screen.findByRole('button', { name: 'Add provider' }));
    const dialog = screen.getByRole('dialog');
    await user.click(within(dialog).getByRole('button', { name: 'Add provider' }));
    expect((await within(dialog).findByRole('alert')).textContent).toBe('Enter a name.');
    expect(api.find('POST', '/api/v1/providers')).toHaveLength(0);
  });

  it('edits without resending the stored credential, or clears it on request', async () => {
    const api = fakeApi({
      'GET /api/v1/providers': () => [mockProvider, managed],
      'GET /api/v1/provider-settings': () => settings(),
      'GET /api/v1/provider-plugins': () => plugins(),
      'PUT /api/v1/providers/team': () => managed,
    });
    const user = userEvent.setup();
    render(<ProvidersSection canEdit canTest />);
    await user.click(within(await screen.findByRole('article', { name: 'Provider team' })).getByRole('button', { name: 'Edit' }));
    let dialog = screen.getByRole('dialog', { name: 'Edit team' });
    expect((within(dialog).getByLabelText(/^API key/) as HTMLInputElement).value).toBe('');
    await user.clear(within(dialog).getByLabelText(/^Default model/));
    await user.type(within(dialog).getByLabelText(/^Default model/), 'gpt-y');
    await user.click(within(dialog).getByRole('button', { name: 'Save changes' }));
    await waitFor(() => expect(api.find('PUT', '/api/v1/providers/team')).toHaveLength(1));
    expect(api.find('PUT', '/api/v1/providers/team')[0].body).not.toHaveProperty('apiKey');

    await user.click(within(card('team')).getByRole('button', { name: 'Edit' }));
    dialog = screen.getByRole('dialog', { name: 'Edit team' });
    await user.click(within(dialog).getByLabelText('Remove the stored credential'));
    await user.click(within(dialog).getByRole('button', { name: 'Save changes' }));
    await waitFor(() => expect(api.find('PUT', '/api/v1/providers/team')).toHaveLength(2));
    expect(api.find('PUT', '/api/v1/providers/team')[1].body).toMatchObject({ apiKey: { clear: true } });
  });

  it('explains a concurrent change instead of overwriting it', async () => {
    fakeApi({
      'GET /api/v1/providers': () => [mockProvider, managed],
      'GET /api/v1/provider-settings': () => settings(),
      'GET /api/v1/provider-plugins': () => plugins(),
      'PUT /api/v1/providers/team': () => apiError(409, 'provider settings changed (revision 5, you edited 4); reload', 'conflict'),
    });
    const user = userEvent.setup();
    render(<ProvidersSection canEdit canTest />);
    await user.click(within(await screen.findByRole('article', { name: 'Provider team' })).getByRole('button', { name: 'Edit' }));
    const dialog = screen.getByRole('dialog');
    await user.click(within(dialog).getByRole('button', { name: 'Save changes' }));
    expect((await within(dialog).findByRole('alert')).textContent).toMatch(/changed by someone else/);
  });

  it('runs a connection test and shows inference results and failures', async () => {
    let fail = false;
    fakeApi({
      'GET /api/v1/providers': () => [mockProvider, managed],
      'GET /api/v1/provider-settings': () => settings(),
      'GET /api/v1/provider-plugins': () => plugins(),
      'POST /api/v1/providers/team/test': () =>
        fail ? { ...okTest, ok: false, inference: { ok: false, error: 'llm team: HTTP 401: Incorrect API key provided: [redacted]' } } : okTest,
    });
    const user = userEvent.setup();
    render(<ProvidersSection canEdit canTest />);
    const team = await screen.findByRole('article', { name: 'Provider team' });
    await user.click(within(team).getByRole('button', { name: 'Test connection' }));
    expect((await within(team).findByRole('status')).textContent).toContain('Inference works with gpt-x');
    fail = true;
    await user.click(within(team).getByRole('button', { name: 'Test connection' }));
    await waitFor(() => expect(within(team).getByRole('status').textContent).toContain('Inference failed'));
    expect(within(team).getByRole('status').textContent).toContain('[redacted]');
    expect(within(team).getByText('Failing')).toBeTruthy();
  });

  it('tests unsaved settings from the dialog', async () => {
    const api = fakeApi({
      'GET /api/v1/providers': () => [mockProvider],
      'GET /api/v1/provider-settings': () => settings(),
      'GET /api/v1/provider-plugins': () => plugins(),
      'POST /api/v1/providers/test': () => ({ ...okTest, draft: true }),
    });
    const user = userEvent.setup();
    render(<ProvidersSection canEdit canTest />);
    await user.click(await screen.findByRole('button', { name: 'Add provider' }));
    const dialog = screen.getByRole('dialog');
    await user.type(within(dialog).getByLabelText(/^Name/), 'team');
    await user.type(within(dialog).getByLabelText(/^API key/), 'sk-1');
    await user.click(within(dialog).getByRole('button', { name: 'Test settings' }));
    expect((await within(dialog).findByRole('status')).textContent).toContain('Inference works');
    expect(api.find('POST', '/api/v1/providers/test')[0].body).toMatchObject({ name: 'team', apiKey: { value: 'sk-1' } });
    expect(api.find('POST', '/api/v1/providers')).toHaveLength(0);
  });

  it('reports a provider that cannot be removed', async () => {
    fakeApi({
      'GET /api/v1/providers': () => [mockProvider, managed],
      'GET /api/v1/provider-settings': () => settings(),
      'GET /api/v1/provider-plugins': () => plugins(),
      'DELETE /api/v1/providers/team': () => apiError(409, 'provider "team" is selected by graph "demo" node "writer"', 'conflict'),
    });
    const user = userEvent.setup();
    render(<ProvidersSection canEdit canTest />);
    await user.click(within(await screen.findByRole('article', { name: 'Provider team' })).getByRole('button', { name: 'Remove' }));
    expect(await within(card('team')).findByText(/node "writer"/)).toBeTruthy();
  });

  it('warns when credentials cannot be stored and disables the key field', async () => {
    fakeApi({
      'GET /api/v1/providers': () => [mockProvider],
      'GET /api/v1/provider-settings': () => settings({ secrets: { enabled: false, keySource: 'none' } }),
      'GET /api/v1/provider-plugins': () => plugins(),
    });
    const user = userEvent.setup();
    render(<ProvidersSection canEdit canTest />);
    expect(await screen.findByText(/no encryption key/)).toBeTruthy();
    await user.click(screen.getByRole('button', { name: 'Add provider' }));
    expect((within(screen.getByRole('dialog')).getByLabelText(/^API key/) as HTMLInputElement).disabled).toBe(true);
  });

  it('shows unavailable providers with the server reason', async () => {
    fakeApi({
      'GET /api/v1/providers': () => [
        mockProvider,
        { ...managed, status: 'unavailable', error: 'llm: provider unavailable: team: re-enter the credential' },
      ],
      'GET /api/v1/provider-settings': () => settings(),
      'GET /api/v1/provider-plugins': () => plugins(),
    });
    render(<ProvidersSection canEdit canTest />);
    const team = await screen.findByRole('article', { name: 'Provider team' });
    expect(within(team).getByText('Unavailable')).toBeTruthy();
    expect(within(team).getByText(/re-enter the credential/)).toBeTruthy();
  });

  it('surfaces load failures', async () => {
    fakeApi({
      'GET /api/v1/providers': () => apiError(500, 'internal error', 'internal'),
      'GET /api/v1/provider-settings': () => settings(),
      'GET /api/v1/provider-plugins': () => plugins(),
    });
    render(<ProvidersSection canEdit canTest />);
    expect((await screen.findByRole('alert')).textContent).toContain('Could not load providers: internal error');
  });

  it('lists inference plugins and lets administrators enable them', async () => {
    let state = plugins();
    const api = fakeApi({
      'GET /api/v1/providers': () => [mockProvider, managed],
      'GET /api/v1/provider-settings': () => settings(),
      'GET /api/v1/provider-plugins': () => state,
      'PUT /api/v1/provider-plugins/ollama': () => {
        state = plugins({ ollama: { enabled: true, enabledBy: 'console' } });
        return state[2];
      },
    });
    const user = userEvent.setup();
    const { rerender } = render(<ProvidersSection canEdit canTest />);
    const ollama = await screen.findByRole('listitem', { name: 'Inference plugin Ollama' });
    expect(within(ollama).getByText('Disabled')).toBeTruthy();
    expect(within(ollama).getByText('Local')).toBeTruthy();
    expect(within(ollama).getByText(/Tool calls · JSON \(native\) · Usage · Model listing/)).toBeTruthy();
    // Configuration-provided plugins cannot be switched in the console.
    const mock = screen.getByRole('listitem', { name: 'Inference plugin Mock' });
    expect(within(mock).getByText(/Provided by server configuration/)).toBeTruthy();
    expect(within(mock).queryByRole('button')).toBeNull();

    await user.click(within(ollama).getByRole('button', { name: 'Enable' }));
    await waitFor(() => expect(within(screen.getByRole('listitem', { name: 'Inference plugin Ollama' })).getByText('Enabled')).toBeTruthy());
    const [put] = api.find('PUT', '/api/v1/provider-plugins/ollama');
    expect(put.body).toEqual({ enabled: true });
    expect(put.headers['If-Match']).toBe('4');

    rerender(<ProvidersSection canEdit={false} canTest />);
    expect(within(screen.getByRole('listitem', { name: 'Inference plugin OpenAI' })).queryByRole('button')).toBeNull();
  });

  it('confirms before disabling a plugin in use and keeps it when declined', async () => {
    const confirm = vi.fn(() => false);
    vi.stubGlobal('confirm', confirm);
    const api = fakeApi({
      'GET /api/v1/providers': () => [mockProvider, managed],
      'GET /api/v1/provider-settings': () => settings(),
      'GET /api/v1/provider-plugins': () => plugins(),
      'PUT /api/v1/provider-plugins/openai': () => apiError(409, 'provider settings changed (revision 5, you edited 4); reload', 'conflict'),
    });
    const user = userEvent.setup();
    render(<ProvidersSection canEdit canTest />);
    const openai = await screen.findByRole('listitem', { name: 'Inference plugin OpenAI' });
    await user.click(within(openai).getByRole('button', { name: 'Disable' }));
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining('team'));
    expect(api.find('PUT', '/api/v1/provider-plugins/openai')).toHaveLength(0);

    confirm.mockReturnValue(true);
    await user.click(within(openai).getByRole('button', { name: 'Disable' }));
    expect((await within(openai).findByRole('alert')).textContent).toMatch(/changed by someone else/);
  });

  it('offers only enabled plugins and explains when none is enabled', async () => {
    let state = plugins({ openai: { enabled: false, enabledBy: undefined, instances: [] }, mock: { enabled: false, enabledBy: undefined, instances: [] } });
    fakeApi({
      'GET /api/v1/providers': () => [],
      'GET /api/v1/provider-settings': () => settings({ types: [...settings().types, { type: 'ollama', label: 'Ollama', keyRequired: false }] }),
      'GET /api/v1/provider-plugins': () => state,
    });
    const user = userEvent.setup();
    const { unmount } = render(<ProvidersSection canEdit canTest />);
    expect(await screen.findByText(/Enable an inference plugin above/)).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Add provider' }) as HTMLButtonElement).disabled).toBe(true);
    unmount();

    state = plugins({ openai: { enabled: false }, mock: { enabled: false, enabledBy: undefined }, ollama: { enabled: true } });
    render(<ProvidersSection canEdit canTest />);
    await waitFor(() => expect((screen.getByRole('button', { name: 'Add provider' }) as HTMLButtonElement).disabled).toBe(false));
    await user.click(screen.getByRole('button', { name: 'Add provider' }));
    const options = within(within(screen.getByRole('dialog')).getByRole('combobox', { name: 'Type' })).getAllByRole('option').map((o) => o.textContent);
    expect(options).toEqual(['Ollama']);
  });
});
