import { act, cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { apiError, fakeApi, json } from '../test/fakeApi';
import { SCMConnectionCreate, SCMConnectionsSection, scmCreateLink } from './SCMConnectionsSection';

const kinds = [
  { id: 'github', name: 'GitHub', repositoryHint: 'owner/name', apiUrlHint: 'https://api.github.com', credentialHelp: 'Repository token' },
  { id: 'bitbucket-cloud', name: 'Bitbucket Cloud', repositoryHint: 'workspace/repository-slug', apiUrlHint: 'https://api.bitbucket.org/2.0', credentialHelp: 'User OAuth access token' },
  { id: 'bitbucket-dc', name: 'Bitbucket Data Center', repositoryHint: 'PROJECT/repository-slug', apiUrlHint: 'https://bitbucket.example.com/context', apiUrlRequired: true, credentialHelp: 'HTTP access token sent as Bearer' },
];
const gh = { name: 'gh', kind: 'github', repository: 'acme/app', tokenConfigured: true };

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  history.replaceState(null, '', '/');
});

describe('SCMConnectionsSection', () => {
  it('combines case-insensitive search and test status and clears filters without changing connections', async () => {
    const backend = fakeApi({
      'GET /api/v1/scm/connections': () => [gh, { ...gh, name: 'other', kind: 'bitbucket-dc', repository: 'team/other' }],
      'POST /api/v1/scm/connections/gh/test': () => ({ ok: true, identity: { user: 'bot', canPush: true } }),
    });
    render(<SCMConnectionsSection role="admin" />);
    const row = await screen.findByRole('row', { name: /acme\/app/ });
    await userEvent.click(within(row).getByRole('button', { name: 'Test connection' }));
    await within(row).findByText('Connected');
    const search = screen.getByRole('searchbox', { name: 'Search connections' });
    const status = screen.getByRole('combobox', { name: 'Test status' });
    await userEvent.type(search, ' ACME/APP ');
    await userEvent.selectOptions(status, 'connected');
    expect(screen.getByText('1 of 2 connections')).toBeTruthy();
    expect(screen.queryByRole('row', { name: /team\/other/ })).toBeNull();
    await userEvent.selectOptions(status, 'failed');
    expect(screen.getByText('No connections match these filters.')).toBeTruthy();
    await userEvent.click(screen.getByRole('button', { name: 'Clear filters' }));
    expect(document.activeElement).toBe(search);
    expect(screen.getByText('2 of 2 connections')).toBeTruthy();
    await userEvent.type(search, 'BITBUCKET DATA CENTER');
    expect(screen.getByRole('row', { name: /team\/other/ })).toBeTruthy();
    expect(screen.queryByRole('row', { name: /acme\/app/ })).toBeNull();
    expect(backend.find('POST', '/api/v1/scm/connections/gh/test')).toHaveLength(1);
    expect(backend.calls.filter(call => call.method !== 'GET')).toHaveLength(1);
  });
  it('reports a connected repository and allows admins to remove it', async () => {
    const api = fakeApi({
      'GET /api/v1/scm/connections': () => [gh],
      'POST /api/v1/scm/connections/gh/test': () => ({ ok: true, identity: { user: 'bot', repository: 'acme/app', defaultBranch: 'main', canPush: true } }),
      'DELETE /api/v1/scm/connections/gh': () => new Response(null, { status: 204 }),
    });
    vi.stubGlobal('confirm', () => true);
    const user = userEvent.setup();
    render(<SCMConnectionsSection role="admin" />);
    const row = await screen.findByRole('row', { name: /gh/ });
    expect(within(row).getByRole('cell', { name: 'Required' })).toBeTruthy();
    expect(within(row).getByText('Not tested')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Add connection' }).getAttribute('href')).toBe(scmCreateLink);
    await user.click(within(row).getByRole('button', { name: 'Test connection' }));
    expect(await within(row).findByText('Connected')).toBeTruthy();
    expect(within(row).getByText('Signed in as bot. Default branch is main. Can push.')).toBeTruthy();
    await user.click(within(row).getByRole('button', { name: 'Remove' }));
    await waitFor(() => expect(api.find('DELETE', '/api/v1/scm/connections/gh')).toHaveLength(1));
  });

  it('saves the dedicated form with the token once and returns to Connections', async () => {
    history.replaceState(null, '', scmCreateLink);
    const api = fakeApi({
      'GET /api/v1/scm/kinds': () => kinds,
      'POST /api/v1/scm/connections': () => json({ name: 'team', tokenConfigured: true }, 201),
    });
    const user = userEvent.setup();
    render(<SCMConnectionCreate role="admin" />);
    const form = screen.getByRole('form', { name: 'Add source control connection' });
    await user.type(within(form).getByLabelText('Name'), 'team');
    await user.type(within(form).getByLabelText('Repository'), 'acme/other');
    await user.click(within(form).getByRole('checkbox', { name: /approval/ }));
    await user.type(within(form).getByLabelText('Token'), 'secret-token');
    await user.click(within(form).getByRole('button', { name: 'Save connection' }));
    await waitFor(() => expect(location.hash).toBe('#/connections'));
    expect(api.find('POST', '/api/v1/scm/connections')).toHaveLength(1);
    expect(api.find('POST', '/api/v1/scm/connections')[0].body).toEqual({ name: 'team', kind: 'github', repository: 'acme/other', requirePullRequestApproval: false, token: 'secret-token' });
    expect(document.body.textContent).not.toContain('secret-token');
  });

  it('shows validation errors and keeps viewers read-only', async () => {
    fakeApi({
      'GET /api/v1/scm/connections': () => [gh],
      'GET /api/v1/scm/kinds': () => kinds,
      'POST /api/v1/scm/connections': () => apiError(422, 'invalid SCM connection: repository: use owner/name', 'invalid'),
    });
    const user = userEvent.setup();
    const view = render(<SCMConnectionCreate role="admin" />);
    await user.type(screen.getByLabelText('Name'), 'keep-draft');
    await user.type(screen.getByLabelText('Repository'), 'invalid');
    await user.click(screen.getByRole('button', { name: 'Save connection' }));
    expect(await screen.findByText(/use owner\/name/)).toBeTruthy();
    expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('keep-draft');
    view.rerender(<SCMConnectionCreate role="viewer" />);
    expect(screen.queryByRole('form')).toBeNull();
    view.unmount();
    render(<SCMConnectionsSection role="viewer" />);
    const card = await screen.findByRole('row', { name: /gh/ });
    expect(within(card).queryByRole('button')).toBeNull();
    expect(screen.queryByRole('link', { name: 'Add connection' })).toBeNull();
  });

  it('separates registry errors from empty results and shows a pending connection test', async () => {
    let listUnavailable = true;
    let finishTest!: (response: Response) => void;
    const backend = fakeApi({
      'GET /api/v1/scm/connections': () => listUnavailable ? apiError(503, 'Registry unavailable', 'unavailable') : [gh],
      'POST /api/v1/scm/connections/gh/test': () => new Promise<Response>(resolve => { finishTest = resolve; }),
    });
    render(<SCMConnectionsSection role="operator" />);
    expect((await screen.findByRole('alert')).textContent).toContain('Registry unavailable');
    expect(screen.queryByText('No source control connection yet.')).toBeNull();
    listUnavailable = false;
    await userEvent.click(screen.getByRole('button', { name: 'Reload source control connections' }));
    const testButton = await screen.findByRole<HTMLButtonElement>('button', { name: 'Test connection' });
    const row = screen.getByRole('row', { name: /acme\/app/ });
    await userEvent.click(testButton);
    expect(await within(row).findByText('Checking connection')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Testing…' })).toBe(testButton);
    expect(testButton.disabled).toBe(true);
    await act(async () => finishTest(apiError(503, 'Service unavailable', 'unavailable')));
    expect(await within(row).findByText('Connection failed')).toBeTruthy();
    expect(screen.getByText('Service unavailable')).toBeTruthy();
    expect(testButton.disabled).toBe(false);
    await userEvent.click(testButton);
    await act(async () => finishTest(json({ ok: true, identity: { user: 'bot', canPush: false } })));
    expect(await within(row).findByText('Limited access')).toBeTruthy();
    expect(screen.getByText(/Cannot push/)).toBeTruthy();
    expect(within(row).queryByText('Connection failed')).toBeNull();
    expect(backend.find('POST', '/api/v1/scm/connections/gh/test')).toHaveLength(2);
  });
});

it('separates edition settings and credentials and submits the chosen Cloud contract', async () => {
  const backend = fakeApi({
    'GET /api/v1/scm/kinds': () => kinds,
    'POST /api/v1/scm/connections': () => json({ name: 'bb' }, 201),
  });
  render(<SCMConnectionCreate role="admin" />);
  await screen.findByRole('option', { name: 'Bitbucket Cloud' });
  await userEvent.type(screen.getByLabelText('Token'), 'old-service-credential');
  await userEvent.type(screen.getByLabelText('API URL'), 'https://old.example');
  await userEvent.selectOptions(screen.getByLabelText('Kind'), 'bitbucket-dc');
  expect((screen.getByLabelText('Token') as HTMLInputElement).value).toBe('');
  expect((screen.getByLabelText('API URL') as HTMLInputElement).required).toBe(true);
  expect((screen.getByLabelText('API URL') as HTMLInputElement).value).toBe('');
  expect(screen.getByPlaceholderText('PROJECT/repository-slug')).toBeTruthy();
  expect(screen.getByRole('note').textContent).toContain('Bearer');
  await userEvent.selectOptions(screen.getByLabelText('Kind'), 'bitbucket-cloud');
  expect((screen.getByLabelText('API URL') as HTMLInputElement).required).toBe(false);
  expect((screen.getByLabelText('Authentication') as HTMLSelectElement).value).toBe('api-token');
  await userEvent.type(screen.getByLabelText('Name'), 'bb');
  await userEvent.type(screen.getByLabelText('Repository'), 'team/app');
  await userEvent.type(screen.getByLabelText('Atlassian account email'), 'owner@example.com');
  await userEvent.type(screen.getByLabelText('API token'), 'cloud-fixture-token');
  await userEvent.click(screen.getByRole('button', { name: 'Save connection' }));
  expect(backend.find('POST', '/api/v1/scm/connections')[0].body).toEqual({ name: 'bb', kind: 'bitbucket-cloud', repository: 'team/app', authType: 'api-token', username: 'owner@example.com', token: 'cloud-fixture-token' });
});

it('clears account and credential on authentication switches and sends OAuth explicitly without an email', async () => {
  const backend = fakeApi({
    'GET /api/v1/scm/kinds': () => kinds,
    'POST /api/v1/scm/connections': () => json({ name: 'bb' }, 201),
  });
  render(<SCMConnectionCreate role="admin" />);
  await screen.findByRole('option', { name: 'Bitbucket Cloud' });
  await userEvent.selectOptions(screen.getByLabelText('Kind'), 'bitbucket-cloud');
  await userEvent.type(screen.getByLabelText('Atlassian account email'), 'old@example.com');
  await userEvent.type(screen.getByLabelText('API token'), 'api-fixture');
  await userEvent.selectOptions(screen.getByLabelText('Authentication'), 'bearer');
  expect(screen.queryByLabelText('Atlassian account email')).toBeNull();
  expect((screen.getByLabelText('Token') as HTMLInputElement).value).toBe('');
  expect(screen.getByRole('note').textContent).toContain('does not obtain or renew');
  await userEvent.type(screen.getByLabelText('Token'), 'oauth-fixture');
  await userEvent.selectOptions(screen.getByLabelText('Authentication'), 'api-token');
  expect((screen.getByLabelText('Atlassian account email') as HTMLInputElement).value).toBe('');
  expect((screen.getByLabelText('API token') as HTMLInputElement).value).toBe('');
  await userEvent.selectOptions(screen.getByLabelText('Authentication'), 'bearer');
  await userEvent.type(screen.getByLabelText('Name'), 'bb');
  await userEvent.type(screen.getByLabelText('Repository'), 'team/app');
  await userEvent.type(screen.getByLabelText('Token'), 'oauth-fixture');
  await userEvent.click(screen.getByRole('button', { name: 'Save connection' }));
  expect(backend.find('POST', '/api/v1/scm/connections')[0].body).toEqual({ name: 'bb', kind: 'bitbucket-cloud', repository: 'team/app', authType: 'bearer', token: 'oauth-fixture' });
});

it('validates the API-token email, explains token creation and preserves the draft after a failed save', async () => {
  let fail = true;
  const backend = fakeApi({
    'GET /api/v1/scm/kinds': () => kinds,
    'POST /api/v1/scm/connections': () => fail ? apiError(503, 'Connection storage unavailable', 'unavailable') : json({ name: 'bb' }, 201),
  });
  render(<SCMConnectionCreate role="admin" />);
  await screen.findByRole('option', { name: 'Bitbucket Cloud' });
  await userEvent.selectOptions(screen.getByLabelText('Kind'), 'bitbucket-cloud');
  await userEvent.click(screen.getByText('Create a Bitbucket API token'));
  expect(screen.getByRole('link', { name: 'Atlassian account API tokens' }).getAttribute('href')).toBe('https://id.atlassian.com/manage-profile/security/api-tokens');
  expect(screen.getByText('read:repository:bitbucket')).toBeTruthy();
  await userEvent.type(screen.getByLabelText('Name'), 'bb');
  await userEvent.type(screen.getByLabelText('Repository'), 'team/app');
  await userEvent.type(screen.getByLabelText('API token'), 'synthetic-private-token');
  await userEvent.click(screen.getByRole('button', { name: 'Save connection' }));
  expect(backend.find('POST', '/api/v1/scm/connections')).toHaveLength(0);
  const email = screen.getByLabelText('Atlassian account email') as HTMLInputElement;
  await userEvent.type(email, 'not-an-email');
  await userEvent.click(screen.getByRole('button', { name: 'Save connection' }));
  expect(backend.find('POST', '/api/v1/scm/connections')).toHaveLength(0);
  await userEvent.clear(email);
  await userEvent.type(email, 'owner@example.com');
  await userEvent.click(screen.getByRole('button', { name: 'Save connection' }));
  expect((await screen.findByRole('alert')).textContent).toContain('Connection storage unavailable');
  expect(email.value).toBe('owner@example.com');
  expect((screen.getByLabelText('API token') as HTMLInputElement).value).toBe('synthetic-private-token');
  expect(screen.getByRole('button', { name: 'Save connection' }).getAttribute('disabled')).toBeNull();
  expect(document.body.textContent).not.toContain('synthetic-private-token');
  fail = false;
  await userEvent.click(screen.getByRole('button', { name: 'Save connection' }));
  await waitFor(() => expect(location.hash).toBe('#/connections'));
  expect(backend.find('POST', '/api/v1/scm/connections')).toHaveLength(2);
});

it('blocks submission while service discovery fails and offers a retry', async () => {
  let fail = true;
  fakeApi({ 'GET /api/v1/scm/kinds': () => fail ? apiError(503, 'Unavailable', 'unavailable') : kinds });
  render(<SCMConnectionCreate role="admin" />);
  await screen.findByRole('alert');
  expect((screen.getByRole('button', { name: 'Save connection' }) as HTMLButtonElement).disabled).toBe(true);
  fail = false;
  await userEvent.click(screen.getByRole('button', { name: 'Retry loading services' }));
  await waitFor(() => expect((screen.getByRole('button', { name: 'Save connection' }) as HTMLButtonElement).disabled).toBe(false));
});

it('shows loading and an empty adapter catalogue without enabling save', async () => {
  let finish!: (response: Response) => void;
  fakeApi({ 'GET /api/v1/scm/kinds': () => new Promise<Response>(resolve => { finish = resolve; }) });
  render(<SCMConnectionCreate role="admin" />);
  expect(screen.getByRole('status').textContent).toContain('Loading source control services');
  expect((screen.getByLabelText('Kind') as HTMLSelectElement).disabled).toBe(true);
  await act(async () => finish(json([])));
  expect(await screen.findByText('No source control adapter is available.')).toBeTruthy();
  expect((screen.getByRole('button', { name: 'Save connection' }) as HTMLButtonElement).disabled).toBe(true);
});

it('requires an administrator and the displayed connection revision to enable Data Center line comments', async () => {
  let enabled = false;
  const revision = '2026-11-28T20:00:00Z';
  const row = () => ({...gh,kind:'bitbucket-dc',inlinePublication:enabled,updatedAt:revision});
  const backend = fakeApi({
    'GET /api/v1/scm/connections': () => [row()],
    'PUT /api/v1/scm/connections/gh/inline-publication': call => {
      expect(call.body).toEqual({enabled:true,revision});
      enabled = true;
      return row();
    },
  });
  const view=render(<SCMConnectionsSection role="operator"/>);
  await screen.findByText('Line comment publication disabled');
  expect(screen.queryByRole('button',{name:'Enable line comments'})).toBeNull();
  view.rerender(<SCMConnectionsSection role="admin"/>);
  const toggle=screen.getByRole('button',{name:'Enable line comments'});
  vi.stubGlobal('confirm',()=>false);
  await userEvent.click(toggle);
  expect(backend.calls.filter(c=>c.method==='PUT')).toHaveLength(0);
  vi.stubGlobal('confirm',()=>true);
  await userEvent.click(toggle);
  expect(await screen.findByRole('button',{name:'Disable line comments'})).toBeTruthy();
});
