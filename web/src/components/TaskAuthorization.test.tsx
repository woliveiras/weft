import {render,screen,fireEvent,waitFor,cleanup} from '@testing-library/react';
import {afterEach,beforeEach,describe,it,expect,vi} from 'vitest';
import {TaskAuthorization} from './TaskAuthorization';
import {api} from '../api/client';
vi.mock('../api/client',async importOriginal=>({...(await importOriginal<typeof import('../api/client')>()),api:{taskAuthorization:vi.fn(),saveTaskAuthorization:vi.fn()}}));
afterEach(cleanup);
beforeEach(()=>{vi.clearAllMocks();vi.mocked(api.taskAuthorization).mockResolvedValue({scope:'reviewed',capabilities:[{id:'worker',isolation:'process',grants:[{name:'shell.exec'}]}]});vi.mocked(api.saveTaskAuthorization).mockResolvedValue({});});
describe('Task authorization',()=>{
 it('requires an explicit confirmation and preserves scope and revision',async()=>{
 render(<TaskAuthorization taskId="tsk_1" canEdit onChanged={()=>{}}/>);
 await screen.findByText('Isolation: process');
 fireEvent.change(screen.getByLabelText('Authorization mode'),{target:{value:'full'}});
 expect(screen.getByText(/Native commands can access/)).toBeTruthy();
 expect((screen.getByRole('button',{name:'Confirm authorization'}) as HTMLButtonElement).disabled).toBe(true);
 fireEvent.click(screen.getByRole('checkbox'));fireEvent.click(screen.getByRole('button',{name:'Confirm authorization'}));
 await waitFor(()=>expect(api.saveTaskAuthorization).toHaveBeenCalledWith('tsk_1','full','reviewed',0,false,0));
 });
 it('shows failure and does not grant controls to a reader',async()=>{
 vi.mocked(api.taskAuthorization).mockRejectedValue(new Error('Offline'));
 render(<TaskAuthorization taskId="tsk_1" canEdit={false} onChanged={()=>{}}/>);
 await screen.findByText('Offline');expect(screen.queryByRole('checkbox')).toBeNull();expect(api.saveTaskAuthorization).not.toHaveBeenCalled();
 expect(screen.getByText('Could not load reviewed agent details.')).toBeTruthy();
 expect(screen.queryByText('Reviewed agents, tools and execution boundaries')).toBeNull();
 });
 it('explains a successful response with no reviewed agents',async()=>{
 vi.mocked(api.taskAuthorization).mockResolvedValue({scope:'reviewed',capabilities:[]});
 render(<TaskAuthorization taskId="tsk_1" canEdit={false} onChanged={()=>{}}/>);
 await screen.findByText('No reviewed agent details are available for this task.');
 expect(screen.queryByText('Reviewed agents, tools and execution boundaries')).toBeNull();
 });
 it('keeps a failed save visible and never reports a change',async()=>{
 const changed=vi.fn();vi.mocked(api.saveTaskAuthorization).mockRejectedValue(new Error('Stale scope'));
 render(<TaskAuthorization taskId="tsk_1" canEdit onChanged={changed}/>);await screen.findByText('Isolation: process');
 fireEvent.click(screen.getByRole('checkbox'));fireEvent.click(screen.getByRole('button',{name:'Confirm authorization'}));await screen.findByText('Stale scope');expect(changed).not.toHaveBeenCalled();
 });
});
it('keeps project preferences as suggestions and prevents full access while running',async()=>{
 vi.mocked(api.taskAuthorization).mockResolvedValue({scope:'reviewed',projectId:'p',preference:{mode:'full',revision:1},record:{taskId:'tsk_1',mode:'full',scope:'reviewed',actor:'admin',policyVersion:1,revision:3}});
 render(<TaskAuthorization taskId="tsk_1" canEdit running onChanged={()=>{}}/>);
 await screen.findByText(/Project suggestion: full/);
 expect((screen.getByLabelText('Authorization mode') as HTMLSelectElement).value).toBe('ask');
 expect((screen.getByRole('option',{name:'Full access'}) as HTMLOptionElement).disabled).toBe(true);
 expect(screen.queryByLabelText('Remember this mode as a project suggestion')).toBeNull();
});
it('does not claim Ask mode or offer live expansion on a legacy task',async()=>{
 render(<TaskAuthorization taskId="tsk_1" canEdit running onChanged={()=>{}}/>);
 await screen.findByText(/Existing workflow approvals/);expect(screen.queryByRole('checkbox')).toBeNull();
});
it('identifies stale saved consent before another start',async()=>{
 vi.mocked(api.taskAuthorization).mockResolvedValue({scope:'new-scope',record:{taskId:'tsk_1',mode:'full',scope:'old-scope',actor:'admin',policyVersion:1,revision:1}});
 render(<TaskAuthorization taskId="tsk_1" canEdit onChanged={()=>{}}/>);await screen.findByText(/Saved authorization is stale/);
});

it('explains that process and Kubernetes use native shell under Workspace access', async () => {
 render(<TaskAuthorization taskId="tsk_1" canEdit onChanged={()=>{}}/>);
 await screen.findByText('Isolation: process');
 fireEvent.change(screen.getByLabelText('Authorization mode'), {target:{value:'workspace'}});
 expect(screen.getByText(/Process and Kubernetes isolation both use native shell/)).toBeTruthy();
 expect(screen.getByText(/Workspace access refuses native shell/)).toBeTruthy();
});

it('explains incomplete setup without requesting or offering authorization',async()=>{
 const configure=vi.fn();
 render(<TaskAuthorization taskId="tsk_1" canEdit onChanged={()=>{}} configurationIssue="Task setup is incomplete: workflow version, entry agent." onConfigure={configure}/>);
 expect(screen.getByText(/select the workflow version and entry agent/)).toBeTruthy();
 fireEvent.click(screen.getByRole('button',{name:'Review task settings'}));expect(configure).toHaveBeenCalledOnce();
 expect(api.taskAuthorization).not.toHaveBeenCalled();expect(screen.queryByRole('checkbox')).toBeNull();
});

it('discards a previously displayed review after refresh fails and offers recovery',async()=>{
 const {ApiError}=await import('../api/client');const configure=vi.fn();
 render(<TaskAuthorization taskId="tsk_1" canEdit onChanged={()=>{}} onConfigure={configure}/>);
 await screen.findByText('Isolation: process');fireEvent.click(screen.getByRole('checkbox'));
 vi.mocked(api.taskAuthorization).mockRejectedValue(new ApiError(409,'storage: conflict','conflict'));
 fireEvent.click(screen.getByRole('button',{name:'Refresh authorization'}));
 await screen.findByText(/Authorization review is out of date/);
 expect(screen.queryByText('Isolation: process')).toBeNull();expect(screen.queryByRole('checkbox')).toBeNull();
 expect(screen.queryByRole('button',{name:'Confirm authorization'})).toBeNull();
 fireEvent.click(screen.getByRole('button',{name:'Review task settings'}));expect(configure).toHaveBeenCalledOnce();
 expect(api.saveTaskAuthorization).not.toHaveBeenCalled();
});

it('withholds consent during refresh and resets confirmation before a successful retry',async()=>{
 let finish!:(value:Awaited<ReturnType<typeof api.taskAuthorization>>)=>void;
 render(<TaskAuthorization taskId="tsk_1" canEdit onChanged={()=>{}}/>);
 await screen.findByText('Isolation: process');fireEvent.click(screen.getByRole('checkbox'));
 vi.mocked(api.taskAuthorization).mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;}));
 fireEvent.click(screen.getByRole('button',{name:'Refresh authorization'}));
 await screen.findByText(/Current mode: loading/);
 expect(screen.queryByRole('checkbox')).toBeNull();expect(screen.queryByText('Isolation: process')).toBeNull();
 finish({scope:'new-scope',capabilities:[{id:'worker',isolation:'docker',grants:[]}]});
 await screen.findByText('Isolation: docker');
 expect((screen.getByRole('checkbox') as HTMLInputElement).checked).toBe(false);
 expect((screen.getByRole('button',{name:'Confirm authorization'}) as HTMLButtonElement).disabled).toBe(true);
 expect(api.saveTaskAuthorization).not.toHaveBeenCalled();
});
