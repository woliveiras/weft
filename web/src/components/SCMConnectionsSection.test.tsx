import { act, cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { apiError, fakeApi } from '../test/fakeApi';
import { SCMConnectionsSection } from './SCMConnectionsSection';

const gh = { name: 'gh', kind: 'github', repository: 'acme/app', tokenConfigured: true };

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('SCMConnectionsSection', () => {
  it('adds a connection sending the token once, tests and removes it', async () => {
    let list = [gh];
    const api = fakeApi({
      'GET /api/v1/scm/connections': () => list,
      'POST /api/v1/scm/connections': (c) => {
        list = [...list, { ...(c.body as typeof gh), tokenConfigured: true }];
        return new Response(JSON.stringify(list[1]), { status: 201, headers: { 'content-type': 'application/json' } });
      },
      'POST /api/v1/scm/connections/gh/test': () => ({ ok: true, identity: { user: 'bot', repository: 'acme/app', defaultBranch: 'main', canPush: true } }),
      'DELETE /api/v1/scm/connections/gh': () => new Response(null, { status: 204 }),
    });
    vi.stubGlobal('confirm', () => true);
    const user = userEvent.setup();
    render(<SCMConnectionsSection role="admin" />);
    const card = await screen.findByRole('row', { name: /gh/ });
    expect(within(card).getByRole('cell', { name: 'Required' })).toBeTruthy();
    await user.click(within(card).getByRole('button', { name: 'Test connection' }));
    expect((await within(card).findByRole('status')).textContent).toBe('Works as bot; default branch main; can push.');
    await user.click(screen.getByRole('button', { name: 'Add source control connection' }));
    const form = screen.getByRole('form', { name: 'Add source control connection' });
    await user.type(within(form).getByLabelText('Name'), 'team');
    await user.type(within(form).getByLabelText('Repository'), 'acme/other');
    await user.click(within(form).getByRole('checkbox', { name: /approval/ }));
    await user.type(within(form).getByLabelText('Token'), 'secret-token');
    await user.click(within(form).getByRole('button', { name: 'Save connection' }));
    await waitFor(() => expect(api.find('POST', '/api/v1/scm/connections')).toHaveLength(1));
    expect(api.find('POST', '/api/v1/scm/connections')[0].body).toEqual({ name: 'team', kind: 'github', repository: 'acme/other', requirePullRequestApproval: false, token: 'secret-token' });
    expect(await screen.findByRole('row', { name: /team/ })).toBeTruthy();
    expect(document.body.textContent).not.toContain('secret-token');
    await user.click(within(screen.getByRole('row', { name: /gh/ })).getByRole('button', { name: 'Remove' }));
    await waitFor(() => expect(api.find('DELETE', '/api/v1/scm/connections/gh')).toHaveLength(1));
  });

  it('shows validation errors and keeps viewers read-only', async () => {
    fakeApi({
      'GET /api/v1/scm/connections': () => [gh],
      'POST /api/v1/scm/connections': () => apiError(422, 'invalid SCM connection: repository: use owner/name', 'invalid'),
    });
    const user = userEvent.setup();
    const view = render(<SCMConnectionsSection role="admin" />);
    await user.click(await screen.findByRole('button', { name: 'Add source control connection' }));
    await user.click(screen.getByRole('button', { name: 'Save connection' }));
    expect(await screen.findByText(/use owner\/name/)).toBeTruthy();
    view.unmount();
    render(<SCMConnectionsSection role="viewer" />);
    const card = await screen.findByRole('row', { name: /gh/ });
    expect(within(card).queryByRole('button')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Add source control connection' })).toBeNull();
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
    await userEvent.click(testButton);
    expect(await screen.findByText('Testing connection…')).toBeTruthy();
    expect(testButton.disabled).toBe(true);
    await act(async () => finishTest(apiError(503, 'Service unavailable', 'unavailable')));
    expect(await screen.findByText('Test could not run: Service unavailable')).toBeTruthy();
    expect(testButton.disabled).toBe(false);
    expect(backend.find('POST', '/api/v1/scm/connections/gh/test')).toHaveLength(1);
  });
});
