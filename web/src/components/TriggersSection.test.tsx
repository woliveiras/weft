import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Graph, Trigger } from '../api/types';
import { apiError, fakeApi } from '../test/fakeApi';
import { TriggersSection } from './TriggersSection';

const graph = { metadata: { id: 'factory', name: 'Factory', version: 3 }, spec: { nodes: [{ id: 'planner', name: 'Planner' }, { id: 'integrator', name: 'Review handoff' }] } } as unknown as Graph;
const paused: Trigger = {
  id: 'trg_1', name: 'Ready', workflow: 'factory', nodeId: 'planner', source: { kind: 'jira', connectionId: 'atl', cloudId: 'x' }, label: 'weft',
  intervalMinutes: 15, authorizationMode: 'workspace', state: 'paused', pausedReason: 'The workflow changed (version 3, consented to 2).',
  consent: { actor: 'alice', workflowVersion: 2, authorizationMode: 'workspace', recordedAt: '2026-11-01T10:00:00Z' },
  currentTask: 'tsk_9', currentIssue: { source: 'jira', key: 'APP-9', title: 'Export', labels: [] },
  skipped: [{ key: 'APP-2', reason: 'is blocked' }], notes: ['APP-9 has no In Progress transition; its status was left unchanged.'],
};

afterEach(cleanup);

describe('TriggersSection', () => {
  it('shows a paused trigger with its reason, work and notes, and resumes it', async () => {
    let list = [paused];
    const api = fakeApi({
      'GET /api/v1/triggers': () => list,
      'GET /api/v1/scm/connections': () => [],
      'POST /api/v1/triggers/trg_1/resume': () => { list = [{ ...paused, state: 'active', pausedReason: undefined }]; return list[0]; },
      'GET /api/v1/triggers/trg_1/matches': () => ({ busy: 'tsk_9', waiting: [{ key: 'APP-3', title: 'Three' }], skipped: [] }),
    });
    const user = userEvent.setup();
    render(<TriggersSection graph={graph} role="admin" />);
    const card = await screen.findByRole('article', { name: 'Trigger Ready' });
    expect(within(card).getByRole('status').textContent).toContain('workflow changed');
    expect(card.textContent).toContain('Runs as alice for workflow version 2');
    expect(card.textContent).toContain('Working on APP-9');
    expect(within(card).getByRole('link', { name: 'open task' }).getAttribute('href')).toBe('#/tasks/tsk_9');
    expect(within(card).getByRole('list', { name: 'Trigger notes' }).textContent).toContain('no In Progress transition');
    await user.click(within(card).getByRole('button', { name: 'Preview matching issues' }));
    expect((await within(card).findByRole('group', { name: 'Matching issues' })).textContent).toContain('Waiting for task tsk_9');
    await user.click(within(card).getByRole('button', { name: 'Resume and record consent' }));
    await waitFor(() => expect(api.find('POST', '/api/v1/triggers/trg_1/resume')).toHaveLength(1));
    expect(await within(screen.getByRole('article', { name: 'Trigger Ready' })).findByText('Active')).toBeTruthy();
  });

  it('creates a trigger with the consent summary and reports validation', async () => {
    let fail = true;
    const api = fakeApi({
      'GET /api/v1/triggers': () => [],
      'GET /api/v1/scm/connections': () => [{ name: 'gh', kind: 'github', repository: 'acme/app', tokenConfigured: true }],
      'POST /api/v1/triggers': (c) => (fail ? apiError(422, 'invalid trigger: label: use 1-50 letters', 'invalid') : { ...(c.body as object), id: 'trg_2', state: 'active', consent: { actor: 'admin', workflowVersion: 3, authorizationMode: 'ask', recordedAt: '' } }),
    });
    const user = userEvent.setup();
    render(<TriggersSection graph={graph} role="admin" />);
    await user.click(await screen.findByRole('button', { name: 'Add issue trigger' }));
    const form = screen.getByRole('form', { name: 'Add issue trigger' });
    expect(form.textContent).toContain('for workflow version 3');
    await user.type(within(form).getByLabelText('Name'), 'Ready');
    await user.type(within(form).getByLabelText('MCP connection'), 'gh-mcp');
    await user.type(within(form).getByLabelText('Repository'), 'acme/app');
    await user.selectOptions(within(form).getByLabelText('Result agent'), 'integrator');
    await user.selectOptions(within(form).getByLabelText('Source control hand-off'), 'gh');
    await user.click(within(form).getByRole('button', { name: 'Save trigger' }));
    expect(await screen.findByText(/use 1-50 letters/)).toBeTruthy();
    fail = false;
    await user.click(within(form).getByRole('button', { name: 'Save trigger' }));
    await waitFor(() => expect(api.find('POST', '/api/v1/triggers')).toHaveLength(2));
    expect(api.find('POST', '/api/v1/triggers')[1].body).toEqual({
      name: 'Ready', workflow: 'factory', nodeId: 'planner', resultNodeId: 'integrator', label: 'weft:todo', intervalMinutes: 15, authorizationMode: 'ask',
      source: { kind: 'github', connectionId: 'gh-mcp', repository: 'acme/app' }, scmConnection: 'gh',
    });
  });

  it.each([
    ['label_failed', 'current labels'],
    ['label_applied', 'readiness comment has not been published'],
  ])('requires confirmation to force resolve %s', async (publication, warning) => {
    const confirmation = vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValueOnce(true);
    try {
      let list: Trigger[] = [{
        ...paused, holds: [{ generation: 'gen-1', key: 'APP-7', reason: 'incomplete', message: 'Waiting', round: 1, snapshotEdits: 2, publication }],
      }];
      const path = '/api/v1/triggers/trg_1/resolve-hold';
      const api = fakeApi({
        'GET /api/v1/triggers': () => list,
        'GET /api/v1/scm/connections': () => [],
        [`POST ${path}`]: () => { list = [paused]; return paused; },
      });
      const user = userEvent.setup();
      render(<TriggersSection graph={graph} role="operator" />);
      const force = await screen.findByRole('button', { name: 'Force resolve hold' });
      expect(screen.getByRole('list', { name: 'Held issues' }).textContent).toContain('edits 2 of 3');
      await user.click(force);
      expect(confirmation).toHaveBeenCalledWith(expect.stringContaining(warning));
      expect(api.find('POST', path)).toHaveLength(0);
      await user.click(force);
      await waitFor(() => expect(api.find('POST', path)).toHaveLength(1));
      expect(api.find('POST', path)[0].body).toEqual({ key: 'APP-7', generation: 'gen-1', force: true });
      await waitFor(() => expect(screen.queryByRole('list', { name: 'Held issues' })).toBeNull());
    } finally {
      confirmation.mockRestore();
    }
  });

  it('lets an admin resume access checks for a hold on an active trigger', async () => {
    const accessError = 'Check the Jira connection and read:project:jira.';
    const active: Trigger = { ...paused, state: 'active', pausedReason: undefined };
    let list: Trigger[] = [{
      ...active, lastError: accessError,
      holds: [{ generation: 'gen-1', key: 'APP-7', reason: 'incomplete', message: 'Waiting', round: 1, accessError }],
    }];
    const path = '/api/v1/triggers/trg_1/resume';
    const api = fakeApi({
      'GET /api/v1/triggers': () => list,
      'GET /api/v1/scm/connections': () => [],
      [`POST ${path}`]: () => { list = [active]; return active; },
    });
    const user = userEvent.setup();
    render(<TriggersSection graph={graph} role="admin" />);
    const resume = await screen.findByRole('button', { name: 'Resume and record consent' });
    expect(screen.getByText(`Last run failed: ${accessError}`)).toBeTruthy();
    await user.click(resume);
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Resume and record consent' })).toBeNull());
    expect(api.find('POST', path)).toHaveLength(1);
  });

  it('formats the latest future retry and omits expired deadlines', async () => {
    const clock = vi.spyOn(Date, 'now').mockReturnValue(Date.parse('2026-11-12T10:45:00Z'));
    try {
      const deadline = '2026-11-12T11:00:00Z';
      fakeApi({
        'GET /api/v1/triggers': () => [{
          ...paused, holds: [{
            generation: 'gen-1', key: 'APP-7', reason: 'claim_recheck_unavailable', round: 1,
            retryAt: '2026-11-12T10:30:00Z', retryLabelAt: '2026-11-12T10:50:00Z', retryAccessAt: deadline,
          }, {
            generation: 'gen-2', key: 'APP-8', reason: 'tracker_unavailable', round: 1,
            retryAt: '2026-11-12T10:30:00Z', retryCommentAt: '2026-11-12T10:45:00Z',
          }],
        }],
        'GET /api/v1/scm/connections': () => [],
      });
      render(<TriggersSection graph={graph} role="viewer" />);
      const holds = await screen.findByRole('list', { name: 'Held issues' });
      const [pending, expired] = within(holds).getAllByRole('listitem');
      expect(pending.textContent).toContain(`Retry after ${new Date(deadline).toLocaleString()}`);
      expect(pending.querySelector('time')?.getAttribute('datetime')).toBe(deadline);
      expect(expired.textContent).not.toContain('Retry after');
      expect(expired.querySelector('time')).toBeNull();
    } finally {
      clock.mockRestore();
    }
  });

  it('keeps viewers read-only', async () => {
    fakeApi({ 'GET /api/v1/triggers': () => [paused], 'GET /api/v1/scm/connections': () => [] });
    render(<TriggersSection graph={graph} role="viewer" />);
    const card = await screen.findByRole('article', { name: 'Trigger Ready' });
    expect(within(card).queryByRole('button')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Add issue trigger' })).toBeNull();
  });
});
