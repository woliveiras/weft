import {cleanup,render,screen,waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it,vi} from 'vitest';
import {RepositoryImport} from './RepositoryImport';
import {fakeApi,apiError} from '../test/fakeApi';
const url='https://example.com/source.git';
const ready={id:'imp_a',url,ref:'HEAD',state:'ready',message:'Snapshot imported',revision:2,attempt:'attempt_a',projectId:'prj_a',commit:'abc'};
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.unstubAllGlobals();});
it('requires explicit confirmation and sends only public import fields',async()=>{
 const backend=fakeApi({'GET /api/v1/repository-imports':()=>[],'POST /api/v1/repository-imports':()=>ready});
 render(<RepositoryImport canEdit initialURL={url} onProject={()=>{}}/>);
 expect((screen.getByRole('button',{name:'Import repository'}) as HTMLButtonElement).disabled).toBe(true);
 await userEvent.click(screen.getByRole('checkbox'));
 await userEvent.click(screen.getByRole('button',{name:'Import repository'}));
 await screen.findByText('ready: Snapshot imported');
 expect(backend.find('POST','/api/v1/repository-imports')[0].body).toEqual({url,ref:'HEAD',confirmed:true,retry:false});
 expect(backend.calls.filter(c=>c.method!=='GET')).toHaveLength(1);
});
it('restores persisted failure and retries with its revision without deleting data',async()=>{
 const failed={...ready,state:'failed',message:'Failed; partial files retained'};
 const backend=fakeApi({'GET /api/v1/repository-imports':()=>[failed],'POST /api/v1/repository-imports':()=>({...ready,state:'fetching',revision:3})});
 render(<RepositoryImport canEdit initialURL={url} onProject={()=>{}}/>);
 await screen.findByText('failed: Failed; partial files retained');
 expect((screen.getByRole('button',{name:'Retry import'}) as HTMLButtonElement).disabled).toBe(true);
 await userEvent.click(screen.getByRole('checkbox'));await userEvent.click(screen.getByRole('button',{name:'Retry import'}));
 await waitFor(()=>expect(backend.find('POST','/api/v1/repository-imports')[0].body).toEqual({url,ref:'HEAD',confirmed:true,retry:true,revision:2}));
});
it('keeps import inputs and shows errors without presenting success',async()=>{
 fakeApi({'GET /api/v1/repository-imports':()=>[],'POST /api/v1/repository-imports':()=>apiError(409,'Imports at capacity','conflict')});
 render(<RepositoryImport canEdit initialURL={url} onProject={()=>{}}/>);
 await userEvent.click(screen.getByRole('checkbox'));await userEvent.click(screen.getByRole('button',{name:'Import repository'}));
 await screen.findByText('Imports at capacity');
 expect((screen.getByRole('textbox',{name:'Clone URL'}) as HTMLInputElement).value).toBe(url);
 expect(screen.queryByText('Snapshot imported')).toBeNull();
});
it('preserves viewer restrictions and selects only an existing imported project',async()=>{
 const onProject=vi.fn();const project={id:'prj_a',name:'Imported',path:'/managed',head:'abc'};
 fakeApi({'GET /api/v1/repository-imports':()=>[ready],'GET /api/v1/projects':()=>[project]});
 const view=render(<RepositoryImport canEdit={false} initialURL={url} onProject={onProject}/>);
 await screen.findByText('ready: Snapshot imported');
 expect((screen.getByRole('button',{name:'Use imported project'}) as HTMLButtonElement).disabled).toBe(true);
 view.rerender(<RepositoryImport canEdit={false} canSelect initialURL={url} onProject={onProject}/>);
 await userEvent.click(screen.getByRole('button',{name:'Use imported project'}));
 await waitFor(()=>expect(onProject).toHaveBeenCalledWith(project));
 expect((screen.getByRole('button',{name:'Import repository'}) as HTMLButtonElement).disabled).toBe(true);
});
it('uses an explicit per-attempt token and clears it after a failed request',async()=>{
 const backend=fakeApi({'GET /api/v1/repository-imports':()=>[],'POST /api/v1/repository-imports':()=>apiError(409,'Retry later','conflict')});
 render(<RepositoryImport canEdit initialURL={url} onProject={()=>{}}/>);
 await userEvent.selectOptions(screen.getByRole('combobox',{name:'Git authentication'}),'token');
 await userEvent.type(screen.getByLabelText('Git username'),'fixture');await userEvent.type(screen.getByLabelText('Git access token'),'synthetic-token');
 await userEvent.click(screen.getByRole('checkbox'));await userEvent.click(screen.getByRole('button',{name:'Import repository'}));
 await screen.findByText('Retry later');
 expect((screen.getByLabelText('Git access token') as HTMLInputElement).value).toBe('');
 expect(backend.find('POST','/api/v1/repository-imports')[0].body).toMatchObject({authentication:{mode:'token',username:'fixture',token:'synthetic-token'}});
 expect(localStorage.getItem('synthetic-token')).toBeNull();
});
it('offers existing host credentials only for the native profile with explicit confirmation',async()=>{
 fakeApi({'GET /api/v1/repository-imports':()=>[]});
 const view=render(<RepositoryImport canEdit onProject={()=>{}}/>);
 expect(screen.queryByRole('option',{name:'Use local Git credential helper'})).toBeNull();
 view.rerender(<RepositoryImport canEdit nativeProfile onProject={()=>{}}/>);
 await userEvent.selectOptions(screen.getByRole('combobox',{name:'Git authentication'}),'local_git');
 expect(screen.getByText(/Helpers may execute local commands/)).toBeTruthy();
 expect((screen.getByRole('button',{name:'Import repository'}) as HTMLButtonElement).disabled).toBe(true);
});
