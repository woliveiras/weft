import {act,cleanup,render,screen,waitFor,within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it} from 'vitest';
import {apiError,fakeApi} from '../test/fakeApi';
import {IssueLifecycleSyncs} from './IssueLifecycleSyncs';
import type {LifecycleSync} from '../api/types';
const job:LifecycleSync={id:'a'.repeat(64),triggerId:'trg_1',taskId:'tsk_7',stage:'pr_delivered',evidence:'evt-pr-opened',issueId:'10007',issueTypeId:'10002',issueKey:'APP-7',source:{kind:'jira-dc',connectionId:'native',siteUrl:'https://jira.example',projectId:'10001'},connectionRevision:3,mapping:{name:'review',revision:2},consentScope:'scope',actor:'admin',state:'unknown',createdAt:'2026-11-20T12:00:00Z',updatedAt:'2026-11-20T12:00:00Z',revision:4};
const intent={operationId:`lifecycle:${job.id}`,connection:'native',connectionRevision:3,mappingName:'review',mappingRevision:2,issueKey:'APP-7',issueId:'10007',issueTypeId:'10002',fromStatusId:'10',transitionId:'31',targetStatusId:'20'};
afterEach(cleanup);
it('reconciles uncertainty with the reviewed CAS revision, loading state and no retry',async()=>{
 let result=job;let finish:(value:LifecycleSync)=>void=()=>{};
 const backend=fakeApi({[`GET /api/v1/lifecycle-syncs/${job.id}`]:()=>({sync:job,transition:{state:'unknown',intent}}),'GET /api/v1/lifecycle-syncs':()=>({items:[result],next:''}),[`POST /api/v1/lifecycle-syncs/${job.id}/reconcile`]:()=>new Promise<LifecycleSync>(resolve=>{finish=v=>{result=v;resolve(v);};})});
 const user=userEvent.setup();render(<IssueLifecycleSyncs connection="native" role="operator"/>);
 await screen.findByText('APP-7');await user.click(screen.getByText('Review recovery'));
 expect(screen.queryByRole('button',{name:'Retry preflight'})).toBeNull();
 await screen.findByText(/Intended status 10 → 20/);expect(screen.getByRole('link',{name:'Open APP-7 in Jira'}).getAttribute('href')).toBe('https://jira.example/browse/APP-7');
 await user.click(screen.getByRole('button',{name:'Reconcile by reading'}));expect(screen.getByRole('button',{name:'Checking…'})).toHaveProperty('disabled',true);
 expect(backend.find('POST',`/api/v1/lifecycle-syncs/${job.id}/reconcile`)[0]).toMatchObject({body:{confirm:true},headers:{'If-Match':'4'}});
 await act(async()=>finish({...job,state:'target_observed',revision:5}));await within(screen.getByRole('region',{name:'Synchronization table'})).findByText('target observed');
 expect(backend.find('POST',`/api/v1/lifecycle-syncs/${job.id}/retry`)).toHaveLength(0);
});
it('preserves conflict until refresh and only admins retry known preflight failure',async()=>{
 let conflict=true;
 const backend=fakeApi({[`GET /api/v1/lifecycle-syncs/${job.id}`]:()=>({sync:job,transition:{state:'unknown',intent}}),'GET /api/v1/lifecycle-syncs':()=>({items:[{...job,state:'failed',error:'Preflight needs review.'}],next:''}),[`POST /api/v1/lifecycle-syncs/${job.id}/retry`]:()=>conflict?apiError(409,'Changed revision','conflict'):{...job,state:'queued',revision:5}});
 const user=userEvent.setup();render(<IssueLifecycleSyncs connection="native" role="admin"/>);
 await screen.findByText('APP-7');await user.click(screen.getByText('Review recovery'));await screen.findByText(/Intended status/);await user.click(screen.getByRole('button',{name:'Retry preflight'}));
 await screen.findByText(/The synchronization changed/);expect(screen.getByRole('button',{name:'Retry preflight'})).toHaveProperty('disabled',true);
 conflict=false;await user.click(screen.getByRole('button',{name:'Refresh synchronization'}));await waitFor(()=>expect(screen.getByRole('button',{name:'Retry preflight'})).not.toHaveProperty('disabled',true));
 await user.click(screen.getByRole('button',{name:'Retry preflight'}));await screen.findByText('Preflight retry queued. Task and PR delivery are unchanged.');
 expect(backend.find('POST',`/api/v1/lifecycle-syncs/${job.id}/retry`)).toHaveLength(2);
});
it('supports bounded pages, local filters, errors and viewer-only inspection',async()=>{
 let second=false;let fail=false;
 const backend=fakeApi({[`GET /api/v1/lifecycle-syncs/${job.id}`]:()=>({sync:job,transition:{state:'unknown',intent}}),'GET /api/v1/lifecycle-syncs':()=>{if(fail)return apiError(503,'History unavailable','unavailable');return {items:[second?{...job,id:'b'.repeat(64),issueKey:'APP-8'}:job],next:second?'':'a'.repeat(64)};}});
 const user=userEvent.setup();render(<IssueLifecycleSyncs connection="native" role="viewer"/>);
 await screen.findByText('APP-7');await user.click(screen.getByText('Review recovery'));expect(screen.queryByRole('button',{name:'Reconcile by reading'})).toBeNull();
 await user.type(screen.getByRole('searchbox'),'unmatched');await screen.findByText('No matching synchronizations on this page.');await user.clear(screen.getByRole('searchbox'));
 second=true;await user.click(screen.getByRole('button',{name:'Next page'}));await screen.findByText('APP-8');expect(screen.getByRole('button',{name:'Next page'})).toHaveProperty('disabled',true);
 expect(String(backend.fetch.mock.calls.at(-1)?.[0])).toContain(`after=${'a'.repeat(64)}`);
 fail=true;await user.click(screen.getByRole('button',{name:'Refresh synchronization'}));await screen.findByText('History unavailable');expect(screen.queryByRole('region',{name:'Synchronization table'})).toBeNull();
 expect(within(screen.getByRole('region',{name:'Tracker synchronization'})).queryByRole('button',{name:'Retry preflight'})).toBeNull();
});

it('does not infer a missing historical destination and still permits admin preflight recovery',async()=>{
 fakeApi({'GET /api/v1/lifecycle-syncs':()=>({items:[{...job,state:'failed'}],next:''}),[`GET /api/v1/lifecycle-syncs/${job.id}`]:()=>({sync:job})});
 const user=userEvent.setup();render(<IssueLifecycleSyncs connection="native" role="admin"/>);
 await screen.findByText('APP-7');await user.click(screen.getByText('Review recovery'));
 await screen.findByText(/No dispatch intent was retained/);expect(screen.queryByText(/Intended status/)).toBeNull();
 expect(screen.getByRole('button',{name:'Retry preflight'})).toHaveProperty('disabled',false);
});
it('keeps recovery disabled when retained evidence cannot be loaded',async()=>{
 fakeApi({'GET /api/v1/lifecycle-syncs':()=>({items:[job],next:''}),[`GET /api/v1/lifecycle-syncs/${job.id}`]:()=>apiError(503,'Intent unavailable','unavailable')});
 const user=userEvent.setup();render(<IssueLifecycleSyncs connection="native" role="operator"/>);
 await screen.findByText('APP-7');await user.click(screen.getByText('Review recovery'));await screen.findByText('Intent unavailable');
 expect(screen.getByRole('button',{name:'Reconcile by reading'})).toHaveProperty('disabled',true);
 expect(screen.getByRole('button',{name:'Reload retained intent'})).toBeTruthy();
});
