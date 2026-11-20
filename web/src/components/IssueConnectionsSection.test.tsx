import {act, cleanup, render, screen, waitFor, within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach, expect, it} from 'vitest';
import type {IssueConnection} from '../api/types';
import {apiError, fakeApi, json} from '../test/fakeApi';
import {IssueConnectionEditor, IssueConnectionsSection, issueConnectionStatus, matchingIssueConnections} from './IssueConnectionsSection';

const connection: IssueConnection = {name:'jira-team',kind:'jira-dc',siteUrl:'https://jira.example/context',projectId:'10001',enabled:true,projectRestricted:true,revision:3,credentialConfigured:true,updatedAt:'2026-11-20T10:00:00Z',updatedBy:'admin'};
afterEach(() => {cleanup(); history.replaceState(null,'','/');});

it('filters by explicit operational prerequisites without equating configuration with a successful identity check', () => {
  const rows=[connection,{...connection,name:'disabled',enabled:false},{...connection,name:'unreviewed',projectRestricted:false},{...connection,name:'missing',credentialConfigured:false}];
  expect(rows.map(issueConnectionStatus)).toEqual(['enabled','disabled','review','review']);
  expect(matchingIssueConnections(rows,' JIRA.EXAMPLE ', 'enabled')).toEqual([connection]);
  expect(matchingIssueConnections(rows,'10001','review').map(c=>c.name)).toEqual(['unreviewed','missing']);
  expect(matchingIssueConnections(rows,'missing-site','')).toEqual([]);
});

it('shows scoped metadata and revision-fenced identity loading, failures and recovery', async () => {
  let finish!: (value: unknown)=>void;
  let current=connection;
  const backend=fakeApi({
    'GET /api/v1/issue-connections':()=>[current],
    'POST /api/v1/issue-connections/jira-team/test':()=>new Promise(resolve=>{finish=resolve;}),
  });
  render(<IssueConnectionsSection role="operator"/>);
  let row=await screen.findByRole('row',{name:/jira-team/});
  expect(row.textContent).toContain('Project ID 10001');
  expect(within(row).getByText('Not checked in this session')).toBeTruthy();
  expect(screen.queryByRole('link',{name:'Edit'})).toBeNull();
  await userEvent.click(within(row).getByRole('button',{name:'Test connection'}));
  expect(within(row).getByRole('button',{name:'Testing…'})).toHaveProperty('disabled',true);
  expect(row.textContent).toContain('Checking identity…');
  expect(backend.find('POST','/api/v1/issue-connections/jira-team/test')[0].headers['If-Match']).toBe('3');
  await act(async()=>finish({ok:false,error:'Jira Data Center returned HTTP 403'}));
  expect(await within(row).findByText('Check failed')).toBeTruthy();
  expect(row.textContent).toContain('HTTP 403');
  await userEvent.click(within(row).getByRole('button',{name:'Test connection'}));
  await act(async()=>finish({ok:true,identity:{key:'bot-key',name:'bot'}}));
  expect(await within(row).findByText('Identity verified')).toBeTruthy();
  expect(row.textContent).toContain('Project permissions still need operator qualification');
  current={...connection,revision:4};
  await userEvent.click(screen.getByRole('button',{name:'Refresh issue connections'}));
  row=await screen.findByRole('row',{name:/jira-team/});
  await within(row).findByText('Not checked in this session');
  expect(row.textContent).not.toContain('Identity verified');
});

it('keeps viewer read-only and recovers connection-list failures', async () => {
  let failed=true;
  const backend=fakeApi({'GET /api/v1/issue-connections':()=>failed?apiError(503,'Storage unavailable','unavailable'):[connection]});
  render(<IssueConnectionsSection role="viewer"/>);
  await screen.findByRole('alert');
  failed=false;
  await userEvent.click(screen.getByRole('button',{name:'Refresh issue connections'}));
  await screen.findByRole('row',{name:/jira-team/});
  expect(screen.queryByRole('link',{name:'Add connection'})).toBeNull();
  expect(screen.queryByRole('button',{name:'Test connection'})).toBeNull();
  expect(backend.calls.every(c=>c.method==='GET')).toBe(true);
});

it('creates a dedicated native connection with explicit restriction review and a write-only PAT', async () => {
  history.replaceState(null,'','#/connections/issues/new');
  const backend=fakeApi({'POST /api/v1/issue-connections':()=>json(connection,201)});
  render(<IssueConnectionEditor role="admin"/>);
  const form=screen.getByRole('form',{name:'Add issue tracker connection'});
  const user=userEvent.setup();
  await user.type(within(form).getByLabelText('Connection name'),'jira-team');
  await user.type(within(form).getByLabelText('Site URL'),connection.siteUrl);
  await user.type(within(form).getByLabelText('Project ID'),'10001');
  await user.type(within(form).getByLabelText('Personal access token'),'synthetic-native-token');
  const restriction=within(form).getByRole('checkbox',{name:/I verified/});
  expect(restriction).toHaveProperty('checked',false);
  await user.click(restriction);
  await user.click(within(form).getByRole('button',{name:'Save connection'}));
  await waitFor(()=>expect(location.hash).toBe('#/connections'));
  expect(backend.find('POST','/api/v1/issue-connections')[0].body).toEqual({name:'jira-team',kind:'jira-dc',siteUrl:connection.siteUrl,projectId:'10001',enabled:true,projectRestricted:true,token:'synthetic-native-token'});
  expect(document.body.textContent).not.toContain('synthetic-native-token');
  expect(within(form).getByLabelText('Personal access token')).toHaveProperty('value','');
});

it('preserves the form on conflict and updates with the reviewed revision without returning or resending the PAT', async () => {
  let fail=true;
  let revision=3;
  const backend=fakeApi({
    'GET /api/v1/issue-connections/jira-team':()=>({...connection,revision}),
    'PUT /api/v1/issue-connections/jira-team':()=>fail?apiError(409,'Connection revision changed; reload before saving','conflict'):{...connection,revision:4},
  });
  render(<IssueConnectionEditor role="admin" name="jira-team"/>);
  const form=await screen.findByRole('form',{name:'Edit issue tracker connection'});
  const user=userEvent.setup();
  expect(within(form).getByLabelText('Connection name')).toHaveProperty('disabled',true);
  expect(within(form).getByLabelText('Personal access token')).toHaveProperty('value','');
  await user.click(within(form).getByRole('checkbox',{name:'Connection enabled'}));
  await user.click(within(form).getByRole('button',{name:'Save connection'}));
  await screen.findByRole('alert');
  expect(within(form).getByLabelText('Project ID')).toHaveProperty('value','10001');
  const call=backend.find('PUT','/api/v1/issue-connections/jira-team')[0];
  expect(call.headers['If-Match']).toBe('3');
  expect(call.body).toEqual({name:'jira-team',kind:'jira-dc',siteUrl:connection.siteUrl,projectId:'10001',enabled:false,projectRestricted:true});
  expect(within(form).getByRole('button',{name:'Save connection'})).toHaveProperty('disabled',true);
  fail=false; revision=4;
  await user.click(within(form).getByRole('button',{name:'Reload saved connection'}));
  const reloaded=await screen.findByRole('form',{name:'Edit issue tracker connection'});
  await waitFor(()=>expect(within(reloaded).getByRole('button',{name:'Save connection'})).toHaveProperty('disabled',false));
  await user.click(within(reloaded).getByRole('checkbox',{name:'Connection enabled'}));
  await user.click(within(reloaded).getByRole('button',{name:'Save connection'}));
  await waitFor(()=>expect(backend.find('PUT','/api/v1/issue-connections/jira-team')).toHaveLength(2));
  expect(backend.find('PUT','/api/v1/issue-connections/jira-team')[1].headers['If-Match']).toBe('4');
});

it('requires replacement credentials and a fresh restriction review when project scope changes', async () => {
  const backend=fakeApi({'GET /api/v1/issue-connections/jira-team':()=>connection,'PUT /api/v1/issue-connections/jira-team':()=>({...connection,revision:4})});
  render(<IssueConnectionEditor role="admin" name="jira-team"/>);
  const form=await screen.findByRole('form',{name:'Edit issue tracker connection'});
  const user=userEvent.setup();
  await user.type(within(form).getByLabelText('Personal access token'),'previous-scope-fixture');
  await user.clear(within(form).getByLabelText('Project ID'));
  await user.type(within(form).getByLabelText('Project ID'),'10002');
  expect(within(form).getByRole('checkbox',{name:/I verified/})).toHaveProperty('checked',false);
  expect(within(form).getByLabelText('Personal access token')).toHaveProperty('value','');
  await user.click(within(form).getByRole('button',{name:'Save connection'}));
  expect(backend.find('PUT','/api/v1/issue-connections/jira-team')).toHaveLength(0);
  await user.type(within(form).getByLabelText('Personal access token'),'replacement-fixture-token');
  await user.click(within(form).getByRole('button',{name:'Save connection'}));
  await waitFor(()=>expect(backend.find('PUT','/api/v1/issue-connections/jira-team')).toHaveLength(1));
  expect(backend.find('PUT','/api/v1/issue-connections/jira-team')[0].body).toMatchObject({projectId:'10002',projectRestricted:false,token:'replacement-fixture-token'});
});

it('requires a fresh account restriction review after replacing the PAT in the same scope', async () => {
  const backend=fakeApi({'GET /api/v1/issue-connections/jira-team':()=>connection,'PUT /api/v1/issue-connections/jira-team':()=>({...connection,revision:4})});
  render(<IssueConnectionEditor role="admin" name="jira-team"/>);
  const form=await screen.findByRole('form',{name:'Edit issue tracker connection'});
  const user=userEvent.setup();
  const restriction=within(form).getByRole('checkbox',{name:/I verified/});
  expect(restriction).toHaveProperty('checked',true);
  await user.type(within(form).getByLabelText('Personal access token'),'different-account-fixture');
  expect(restriction).toHaveProperty('checked',false);
  await user.click(within(form).getByRole('button',{name:'Save connection'}));
  await waitFor(()=>expect(backend.find('PUT','/api/v1/issue-connections/jira-team')).toHaveLength(1));
  expect(backend.find('PUT','/api/v1/issue-connections/jira-team')[0].body).toMatchObject({projectRestricted:false,token:'different-account-fixture',projectId:'10001'});
});

it('does not offer the editor to non-administrators', async () => {
  const backend=fakeApi({});
  render(<IssueConnectionEditor role="operator"/>);
  expect(screen.queryByRole('form')).toBeNull();
  expect(screen.getByText(/An administrator must configure/)).toBeTruthy();
  await waitFor(()=>expect(backend.calls).toHaveLength(0));
});
