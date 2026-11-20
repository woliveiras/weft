import {cleanup,render,screen,waitFor,within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it} from 'vitest';
import {fakeApi,apiError} from '../test/fakeApi';
import {JiraCloudProfileEditor,JiraCloudProfilesSection} from './JiraCloudProfilesSection';
import {IssueWorkflowMappingsPage} from './IssueWorkflowMappingsPage';
import {api} from '../api/client';
import {parseRoute,routeHash} from '../App';
import {ConnectionsPage} from './InstallationPages';
import type {Info} from '../api/types';
const input={name:'cloud-team',mcpConnection:'atl',mcpRevision:4,cloudId:'11111111-2222-3333-4444-555555555555',siteUrl:'https://api.atlassian.com/ex/jira/11111111-2222-3333-4444-555555555555',projectId:'10001',enabled:true,projectRestricted:true};
const profile={...input,revision:2,updatedAt:'2026-11-20T12:00:00Z',updatedBy:'admin'};
const connection={id:'atl',name:'Atlassian',revision:4,enabled:true};
afterEach(()=>{cleanup();history.replaceState(null,'','/');});
it('round trips Cloud destinations separately from native DC',()=>{
 for(const suffix of ['new','c/cloud-team','c/cloud-team/transitions','c/cloud-team/mappings','c/cloud-team/mappings/new','c/cloud-team/mappings/c/review']){
  const hash=`#/connections/jira-cloud/${suffix}`;expect(routeHash(parseRoute(hash))).toBe(hash);expect(parseRoute(hash)).toMatchObject({issueEdition:'jira'});
 }
 expect(parseRoute('#/connections/issues/c/cloud-team')).not.toHaveProperty('issueEdition');
});
it('lists factual profile authority and preserves viewer read-only navigation',async()=>{
 fakeApi({'GET /api/v1/jira-cloud-profiles':()=>[profile]});
 render(<JiraCloudProfilesSection role="viewer"/>);
 expect(await screen.findByText('cloud-team')).toBeTruthy();
 expect(screen.getByText('MCP revision 4')).toBeTruthy();expect(screen.queryByRole('link',{name:'Edit'})).toBeNull();
 expect(screen.getByRole('link',{name:'Workflow mappings'}).getAttribute('href')).toBe('#/connections/jira-cloud/c/cloud-team/mappings');
});
it('requires review before saving, resets account confirmation after scope edits and keeps CAS conflicts',async()=>{
 const backend=fakeApi({'GET /api/v1/mcp/connections':()=>[connection],'GET /api/v1/jira-cloud-profiles/cloud-team':()=>profile,'POST /api/v1/jira-cloud-profiles/preview':()=>profile,'PUT /api/v1/jira-cloud-profiles/cloud-team':()=>apiError(409,'Profile changed','conflict')});
 render(<JiraCloudProfileEditor name="cloud-team" role="admin"/>);const user=userEvent.setup();
 const form=await screen.findByRole('form',{name:'Jira Cloud workflow profile'});
 expect(within(form).getByRole('button',{name:'Save profile'})).toHaveProperty('disabled',true);
 const project=within(form).getByLabelText('Project ID');await user.clear(project);await user.type(project,'10002');
 expect(within(form).getByRole('checkbox',{name:/I verified/})).toHaveProperty('checked',false);
 await user.click(within(form).getByRole('checkbox',{name:/I verified/}));await user.click(within(form).getByRole('button',{name:'Review profile'}));
 await screen.findByText('Profile reviewed. Save to persist this configuration.');await user.click(within(form).getByRole('button',{name:'Save profile'}));
 await screen.findByText('Profile changed');expect(project).toHaveProperty('value','10002');expect(within(form).getByRole('button',{name:'Save profile'})).toHaveProperty('disabled',true);
 const call=backend.find('PUT','/api/v1/jira-cloud-profiles/cloud-team')[0];expect(call.headers['If-Match']).toBe('2');expect(call.body).toMatchObject({projectId:'10002',mcpRevision:4});expect(call.body).not.toHaveProperty('token');
});
it('can disable an unchanged profile without previewing its unavailable authority',async()=>{
 const backend=fakeApi({'GET /api/v1/mcp/connections':()=>[],'GET /api/v1/jira-cloud-profiles/cloud-team':()=>profile,'PUT /api/v1/jira-cloud-profiles/cloud-team':()=>({...profile,enabled:false,revision:3})});
 render(<JiraCloudProfileEditor name="cloud-team" role="admin"/>);const user=userEvent.setup();const form=await screen.findByRole('form',{name:'Jira Cloud workflow profile'});
 await user.click(within(form).getByRole('checkbox',{name:'Profile enabled'}));await user.click(within(form).getByRole('button',{name:'Save profile'}));
 await waitFor(()=>expect(backend.find('PUT','/api/v1/jira-cloud-profiles/cloud-team')).toHaveLength(1));expect(backend.find('POST','/api/v1/jira-cloud-profiles/preview')).toHaveLength(0);
});
it('allows offline disabling when the MCP catalogue itself fails',async()=>{
 const backend=fakeApi({'GET /api/v1/mcp/connections':()=>apiError(503,'MCP catalogue unavailable','unavailable'),'GET /api/v1/jira-cloud-profiles/cloud-team':()=>profile,'PUT /api/v1/jira-cloud-profiles/cloud-team':()=>({...profile,enabled:false,revision:3})});
 render(<JiraCloudProfileEditor name="cloud-team" role="admin"/>);const user=userEvent.setup();const form=await screen.findByRole('form',{name:'Jira Cloud workflow profile'});
 await screen.findByText('MCP catalogue unavailable');await user.click(within(form).getByRole('checkbox',{name:'Profile enabled'}));await user.click(within(form).getByRole('button',{name:'Save profile'}));
 await waitFor(()=>expect(backend.find('PUT','/api/v1/jira-cloud-profiles/cloud-team')).toHaveLength(1));expect(backend.find('PUT','/api/v1/jira-cloud-profiles/cloud-team')[0].headers['If-Match']).toBe('2');expect(backend.find('POST','/api/v1/jira-cloud-profiles/preview')).toHaveLength(0);
});
it('discards same-name same-revision discovery when navigating between Jira editions',async()=>{
 const native={...profile,kind:'jira-dc',siteUrl:'https://jira.example',credentialConfigured:true};
 const discovery={connection:'cloud-team',revision:2,discovery:{site:native.siteUrl,projectId:'10001',issueId:'10007',issueKey:'APP-7',issueTypeId:'10002',current:{id:'10',name:'DC status'},updated:'2026-11-20T12:00:00Z',transitions:[]}};
 fakeApi({'GET /api/v1/issue-connections/cloud-team':()=>native,'POST /api/v1/issue-connections/cloud-team/transitions':()=>discovery,'GET /api/v1/jira-cloud-profiles/cloud-team':()=>profile});
 const info={identity:{role:'admin'}} as Info;
 const ui=render(<ConnectionsPage info={info} issueConnectionID="cloud-team" issueTransitions/>);const user=userEvent.setup();
 await user.type(await screen.findByLabelText('Issue key'),'APP-7');await user.click(screen.getByRole('button',{name:'Discover transitions'}));await screen.findByRole('heading',{name:'APP-7 · DC status'});
 ui.rerender(<ConnectionsPage info={info} issueEdition="jira" issueConnectionID="cloud-team" issueTransitions/>);
 await screen.findByText(/https:\/\/api.atlassian.com\/ex\/jira/);expect(screen.queryByRole('heading',{name:'APP-7 · DC status'})).toBeNull();expect(screen.getByLabelText('Issue key')).toHaveProperty('value','');
});
it('uses Cloud mapping and discovery routes through the shared mapping form',async()=>{
 const discovery={connection:'cloud-team',revision:2,discovery:{site:input.siteUrl,projectId:'10001',issueId:'10007',issueKey:'APP-7',issueTypeId:'10002',current:{id:'10',name:'Ready'},updated:'2026-11-20T12:00:00Z',transitions:[{id:'31',name:'Review',to:{id:'20',name:'Review'},fields:{}}]}};
 const backend=fakeApi({'GET /api/v1/jira-cloud-profiles/cloud-team':()=>profile,'POST /api/v1/jira-cloud-profiles/cloud-team/transitions':()=>discovery,'POST /api/v1/jira-cloud-workflow-mappings':()=>({})});
 render(<IssueWorkflowMappingsPage edition="jira" name="cloud-team" route={{create:true}} role="admin"/>);const user=userEvent.setup();const form=await screen.findByRole('form',{name:'Workflow mapping configuration'});
 await user.type(within(form).getByLabelText('Mapping name'),'review');await user.type(within(form).getByLabelText('Sample issue key'),'APP-7');await user.click(within(form).getByRole('button',{name:'Discover available transitions'}));
 await user.selectOptions(await within(form).findByLabelText('Transition'),'31');await user.click(within(form).getByRole('button',{name:'Save mapping'}));
 await waitFor(()=>expect(backend.find('POST','/api/v1/jira-cloud-workflow-mappings')).toHaveLength(1));expect(backend.find('POST','/api/v1/jira-cloud-profiles/cloud-team/transitions')[0].headers['If-Match']).toBe('2');expect(backend.calls.some(c=>c.path.startsWith('/api/v1/issue-'))).toBe(false);
});
it('sends independent client CAS headers and explicit Cloud lifecycle history filter',async()=>{
 const backend=fakeApi({'PUT /api/v1/jira-cloud-profiles/cloud-team':()=>profile,'PUT /api/v1/jira-cloud-workflow-mappings/review':()=>({}),'GET /api/v1/lifecycle-syncs':()=>({items:[],next:''})});
 await api.saveJiraCloudProfile(input,2);expect(backend.calls[0].headers['If-Match']).toBe('2');
 const fetcher=backend.fetch;await api.lifecycleSyncs('cloud-team',undefined,'jira');expect(String(fetcher.mock.calls.at(-1)?.[0])).toContain('kind=jira');
});
