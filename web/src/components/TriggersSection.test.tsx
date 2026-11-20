import { act, cleanup, render, screen, waitFor, within } from '@testing-library/react';
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

it('selects a native DC connection, snapshots its site/project and keeps SCM delivery independent', async () => {
  const native={name:'jira-dc',kind:'jira-dc',siteUrl:'https://jira.example/context',projectId:'10001',enabled:true,credentialConfigured:true,projectRestricted:true,revision:3};
  const backend=fakeApi({
    'GET /api/v1/triggers':()=>[],
    'GET /api/v1/trigger-policy':()=>({inputBytes:16384,outputTokens:512,seconds:10,moneyMicros:10000,currency:'USD'}),
    'GET /api/v1/scm/connections':()=>[{name:'bitbucket',kind:'bitbucket-dc',repository:'TEAM/app',tokenConfigured:true}],
    'GET /api/v1/issue-connections':()=>[native,{...native,name:'unreviewed',projectRestricted:false}],
    'POST /api/v1/triggers':c=>({...c.body as object,id:'native-trigger',state:'active'}),
  });
  const user=userEvent.setup();
  render(<TriggersSection graph={graph} role="admin"/>);
  await user.click(await screen.findByRole('button',{name:'Add issue trigger'}));
  const form=screen.getByRole('form',{name:'Add issue trigger'});
  await user.type(within(form).getByLabelText('Name'),'Native issues');
  await user.type(within(form).getByLabelText('MCP connection'),'discarded-mcp');
  await user.type(within(form).getByLabelText('Repository'),'discarded/repo');
  await user.selectOptions(within(form).getByLabelText('Tracker'),'jira-dc');
  const picker=await within(form).findByLabelText('Issue tracker connection');
  await waitFor(()=>expect(within(picker).getByRole('option',{name:/jira-dc/})).toBeTruthy());
  expect(within(picker).getByRole('option',{name:/unreviewed/})).toHaveProperty('disabled',true);
  await user.selectOptions(picker,'jira-dc');
  expect(within(form).getByLabelText('Reviewed site')).toHaveProperty('value',native.siteUrl);
  expect(within(form).getByLabelText('Reviewed project ID')).toHaveProperty('value','10001');
  expect(within(form).queryByLabelText('MCP connection')).toBeNull();
  await user.selectOptions(within(form).getByLabelText('Source control hand-off'),'bitbucket');
  await user.click(within(form).getByRole('button',{name:'Save trigger'}));
  await waitFor(()=>expect(backend.find('POST','/api/v1/triggers')).toHaveLength(1));
  expect(backend.find('POST','/api/v1/triggers')[0].body).toMatchObject({source:{kind:'jira-dc',connectionId:'jira-dc',siteUrl:native.siteUrl,projectId:'10001'},scmConnection:'bitbucket'});
  const body=backend.find('POST','/api/v1/triggers')[0].body as {source:Record<string,unknown>};
  expect(body.source).not.toHaveProperty('repository');
  expect(body.source).not.toHaveProperty('cloudId');
});

it('blocks saving on native connection load failure and permits scoped recovery without assuming the saved scope', async () => {
  let fail=true;
  const nativeTrigger={...paused,currentTask:undefined,source:{kind:'jira-dc',connectionId:'native',siteUrl:'https://old.example',projectId:'10001'}};
  const backend=fakeApi({
    'GET /api/v1/triggers':()=>[nativeTrigger],
    'GET /api/v1/scm/connections':()=>[],
    'GET /api/v1/trigger-policy':()=>({}),
    'GET /api/v1/issue-connections':()=>fail?apiError(503,'Native registry unavailable','unavailable'):[{name:'native',kind:'jira-dc',siteUrl:'https://new.example',projectId:'10002',enabled:true,credentialConfigured:true,projectRestricted:true,revision:4}],
  });
  render(<TriggersSection graph={graph} role="admin"/>);
  await userEvent.click(await screen.findByRole('button',{name:'Edit'}));
  const form=screen.getByRole('form',{name:'Edit issue trigger'});
  await within(form).findByRole('alert');
  expect(within(form).getByRole('button',{name:'Save trigger'})).toHaveProperty('disabled',true);
  fail=false;
  await userEvent.click(within(form).getByRole('button',{name:'Refresh issue connections'}));
  const adopt=await within(form).findByRole('button',{name:'Use current connection scope'});
  expect(within(form).getByLabelText('Reviewed site')).toHaveProperty('value','https://old.example');
  expect(within(form).getByRole('button',{name:'Save trigger'})).toHaveProperty('disabled',true);
  await userEvent.click(adopt);
  expect(within(form).getByLabelText('Reviewed site')).toHaveProperty('value','https://new.example');
  expect(within(form).getByLabelText('Reviewed project ID')).toHaveProperty('value','10002');
  expect(within(form).getByRole('button',{name:'Save trigger'})).toHaveProperty('disabled',false);
  expect(backend.calls.every(c=>c.method==='GET')).toBe(true);
});

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

  it('shows each access error on its own hold', async () => {
    fakeApi({
      'GET /api/v1/triggers': () => [{
        ...paused, holds: [
          { generation: 'gen-1', key: 'APP-7', reason: 'incomplete', round: 1, accessError: 'The project rejected the connection.' },
          { generation: 'gen-2', key: 'APP-8', reason: 'incomplete', round: 1, accessError: 'The project API is unavailable.' },
        ],
      }],
      'GET /api/v1/scm/connections': () => [],
    });
    render(<TriggersSection graph={graph} role="viewer" />);
    const holds = await screen.findByRole('list', { name: 'Held issues' });
    const [first, second] = within(holds).getAllByRole('listitem');
    expect(first.textContent).toContain('APP-7');
    expect(first.textContent).toContain('The project rejected the connection.');
    expect(second.textContent).toContain('APP-8');
    expect(second.textContent).toContain('The project API is unavailable.');
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

  it('removes each retry when it expires while the page stays open', async () => {
    vi.useFakeTimers();
    try {
      vi.setSystemTime(new Date('2026-11-12T10:45:00Z'));
      const first = '2026-11-12T10:45:01Z';
      const second = '2026-11-12T10:45:02Z';
      const api = fakeApi({
        'GET /api/v1/triggers': () => [{
          ...paused, holds: [
            { generation: 'gen-1', key: 'APP-7', reason: 'tracker_unavailable', round: 1, retryAccessAt: first },
            { generation: 'gen-2', key: 'APP-8', reason: 'incomplete', round: 1, retryCommentAt: second },
          ],
        }],
        'GET /api/v1/scm/connections': () => [],
      });
      await act(async () => { render(<TriggersSection graph={graph} role="viewer" />); });
      const holds = screen.getByRole('list', { name: 'Held issues' });
      const [early, late] = within(holds).getAllByRole('listitem');
      expect(early.textContent).toContain(`Retry after ${new Date(first).toLocaleString()}`);
      expect(late.textContent).toContain(`Retry after ${new Date(second).toLocaleString()}`);

      await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
      expect(early.textContent).not.toContain('Retry after');
      expect(late.textContent).toContain('Retry after');

      await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
      expect(late.textContent).not.toContain('Retry after');
      expect(api.find('GET', '/api/v1/triggers')).toHaveLength(1);
    } finally {
      vi.useRealTimers();
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

it('shows the retained task launch cause on a paused trigger',async()=>{
 const cause='Task environment build failed. The container image build command failed.';
 fakeApi({'GET /api/v1/triggers':()=>[{...paused,currentTask:undefined,pausedReason:cause}],'GET /api/v1/scm/connections':()=>[]});
 render(<TriggersSection graph={graph} role="viewer"/>);
 expect((await screen.findByRole('status')).textContent).toBe(cause);
 expect(screen.queryByRole('button',{name:'Resume and record consent'})).toBeNull();
});

it('shows the blocked delivery summary and links the original task',async()=>{
 fakeApi({
  'GET /api/v1/triggers':()=>[{...paused,currentTask:undefined,currentIssue:undefined,lastFailedTask:'tsk_blocked',pausedReason:'Review delivery for acme/app#7. Task: https://weft.example/#/tasks/tsk_blocked Agent summary: Dependency registry unavailable.'}],
  'GET /api/v1/scm/connections':()=>[],
 });
 render(<TriggersSection graph={graph} role="admin"/>);
 const card=await screen.findByRole('article',{name:'Trigger Ready'});
 expect(within(card).getByRole('status').textContent).toContain('Agent summary: Dependency registry unavailable.');
 expect(within(card).getByRole('link',{name:'Open failed task'}).getAttribute('href')).toBe('#/tasks/tsk_blocked');
 expect(card.textContent).not.toContain('succeeded');
});

it('keeps edits and cancellation disabled until a pending trigger save finishes', async () => {
  let finish!: (value: unknown)=>void;
  const backend=fakeApi({
    'GET /api/v1/triggers':()=>[paused],
    'GET /api/v1/scm/connections':()=>[],
    'PUT /api/v1/triggers/trg_1':()=>new Promise(resolve=>{finish=resolve;}),
  });
  const user=userEvent.setup();
  render(<TriggersSection graph={graph} role="admin"/>);
  await user.click(await screen.findByRole('button',{name:/^Edit$/}));
  const form=screen.getByRole('form',{name:'Edit issue trigger'});
  await user.click(within(form).getByRole('button',{name:'Save trigger'}));
  const cancel=within(form).getByRole('button',{name:'Cancel'});
  expect(cancel.closest('fieldset')?.disabled).toBe(true);
  await user.click(cancel);
  await user.type(within(form).getByLabelText('Name'),'ignored');
  expect(within(form).getByLabelText('Name')).toHaveProperty('value','Ready');
  expect(screen.getByRole('form',{name:'Edit issue trigger'})).toBeTruthy();
  expect(backend.find('PUT','/api/v1/triggers/trg_1')).toHaveLength(1);
  await act(async()=>finish(paused));
  await waitFor(()=>expect(screen.queryByRole('form')).toBeNull());
});
