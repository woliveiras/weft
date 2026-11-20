import {cleanup,render,screen,waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it,vi} from 'vitest';
import {apiError,fakeApi} from '../test/fakeApi';
import {LifecycleMappingPicker} from './LifecycleMappingPicker';
const connection={name:'native',kind:'jira-dc' as const,siteUrl:'https://jira.example',projectId:'10001',revision:3,enabled:true,projectRestricted:true,credentialConfigured:true,updatedAt:'2026-11-20T12:00:00Z',updatedBy:'admin'};
const mapping={name:'review-step',connection:'native',connectionRevision:3,siteUrl:connection.siteUrl,projectId:'10001',enabled:true,revision:2,issueTypeId:'10002',fromStatusId:'10',targetStatusId:'20'};
afterEach(cleanup);
it('selects only current enabled scoped definition revisions and preserves the other stage',async()=>{
 fakeApi({'GET /api/v1/issue-workflow-mappings':()=>[mapping,{...mapping,name:'old',connectionRevision:2},{...mapping,name:'foreign',projectId:'999'},{...mapping,name:'disabled',enabled:false}]});
 const change=vi.fn();const user=userEvent.setup();
 render(<LifecycleMappingPicker connection={connection} value={{workStarted:{name:'working',revision:1}}} onChange={change}/>);
 const select=screen.getByLabelText('When a pull request is delivered');
 await waitFor(()=>expect(select).not.toHaveProperty('disabled',true));
 expect(screen.queryByRole('option',{name:/old|foreign|disabled/})).toBeNull();
 await user.selectOptions(select,'review-step@2');
 expect(change).toHaveBeenLastCalledWith({workStarted:{name:'working',revision:1},prDelivered:{name:'review-step',revision:2}});
 expect(screen.getByRole('option',{name:/working.*review required/})).toHaveProperty('disabled',true);
 expect(screen.getByRole('link',{name:'Manage mappings and synchronization'}).getAttribute('href')).toBe('#/connections/issues/c/native/mappings');
});
it('shows load failure and retries without silently changing a pin',async()=>{
 let fail=true;fakeApi({'GET /api/v1/issue-workflow-mappings':()=>fail?apiError(503,'Catalogue unavailable','unavailable'):[mapping]});
 const change=vi.fn();const user=userEvent.setup();render(<LifecycleMappingPicker connection={connection} value={{prDelivered:{name:'review-step',revision:2}}} onChange={change}/>);
 await screen.findByText('Catalogue unavailable');expect(screen.getByLabelText('When a pull request is delivered')).toHaveProperty('disabled',true);expect(change).not.toHaveBeenCalled();
 fail=false;await user.click(screen.getByRole('button',{name:'Retry loading mappings'}));await waitFor(()=>expect(screen.getByLabelText('When a pull request is delivered')).not.toHaveProperty('disabled',true));
 await user.selectOptions(screen.getByLabelText('When a pull request is delivered'),'');expect(change).toHaveBeenLastCalledWith(undefined);
});
it('locks pins while a task is admitted',async()=>{
 fakeApi({'GET /api/v1/issue-workflow-mappings':()=>[mapping]});render(<LifecycleMappingPicker connection={connection} disabled onChange={vi.fn()}/>);
 expect(await screen.findByText('Let the admitted task finish before changing its mappings.')).toBeTruthy();expect(screen.getByLabelText('When runtime starts').matches(':disabled')).toBe(true);
});
