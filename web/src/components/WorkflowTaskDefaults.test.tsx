import {useState} from 'react';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
import type {Graph} from '../api/types';
import {apiError,fakeApi} from '../test/fakeApi';
import {WorkflowTaskDefaults} from './WorkflowTaskDefaults';

afterEach(()=>{cleanup();vi.unstubAllGlobals();});
const initial:Graph={apiVersion:'weft.io/v1',kind:'Graph',metadata:{id:'flow',name:'Flow',version:4},spec:{nodes:[{id:'worker',name:'Worker'},{id:'reviewer',name:'Reviewer'},{id:'off',disabled:true}],channels:[{id:'requests',to:['worker'],types:['work.request']},{id:'internal',from:'worker',to:['reviewer']}]}};
function Editor({graph=initial,editable=true}:{graph?:Graph;editable?:boolean}) {
 const [draft,setDraft]=useState(graph);
 return <><WorkflowTaskDefaults graph={draft} onChange={update=>setDraft(update)} editable={editable}/><output data-testid="draft">{JSON.stringify(draft)}</output></>;
}
it('edits reusable choices in the graph draft without publishing, granting or running',async()=>{
 const backend=fakeApi({'GET /api/v1/projects':()=>[{id:'p',name:'Repository',head:'abcdef',path:'/repo'}],'GET /api/v1/scm/connections':()=>[{name:'team-scm',repository:'team/repo'}]});
 render(<Editor/>);
 await screen.findByRole('option',{name:/Repository/});
 fireEvent.change(screen.getByLabelText('Default input channel'),{target:{value:'requests'}});
 expect((screen.getByLabelText('Default entry agent') as HTMLSelectElement).value).toBe('worker');
 expect((screen.getByLabelText('Default message type') as HTMLInputElement).value).toBe('work.request');
 fireEvent.change(screen.getByLabelText('Default project'),{target:{value:'p'}});
 fireEvent.change(screen.getByLabelText('Default source control connection'),{target:{value:'team-scm'}});
 const draft=JSON.parse(screen.getByTestId('draft').textContent!);
 expect(draft.spec.task).toEqual({nodeId:'worker',channelId:'requests',messageType:'work.request',projectId:'p',scmConnection:'team-scm'});
 expect(draft.metadata.version).toBe(4);
 expect(draft.spec.nodes).toEqual(initial.spec.nodes);
 fireEvent.change(screen.getByLabelText('Default entry agent'),{target:{value:'reviewer'}});
 expect(screen.queryByLabelText('Default message type')).toBeNull();
 expect(JSON.parse(screen.getByTestId('draft').textContent!).spec.task.channelId).toBeUndefined();
 expect(screen.queryByRole('option',{name:'off'})).toBeNull();
 expect(backend.calls.every(call=>call.method==='GET')).toBe(true);
});
it('distinguishes loading and failed discovery, preserves saved unavailable defaults and retries',async()=>{
 let fail=true;
 fakeApi({'GET /api/v1/projects':()=>fail?apiError(503,'projects offline','unavailable'):[],'GET /api/v1/scm/connections':()=>fail?apiError(503,'connections offline','unavailable'):[{name:'saved',repository:'team/repo'}]});
 render(<Editor graph={{...initial,spec:{...initial.spec,task:{projectId:'missing',scmConnection:'saved'}}}}/>);
 expect(screen.getByText('Loading projects…')).not.toBeNull();
 await screen.findByText(/Could not load projects/);
 expect((screen.getByLabelText('Default project') as HTMLSelectElement).value).toBe('missing');
 expect((screen.getByLabelText('Default source control connection') as HTMLSelectElement).value).toBe('saved');
 fail=false;fireEvent.click(screen.getByRole('button',{name:'Retry resource discovery'}));
 await waitFor(()=>expect(screen.queryByText(/Could not load projects/)).toBeNull());
 await screen.findByRole('option',{name:'saved · team/repo'});
 expect((screen.getByLabelText('Default project') as HTMLSelectElement).value).toBe('missing');
});
it('lets non-administrators inspect the defaults without editing them',async()=>{
 fakeApi({'GET /api/v1/projects':()=>[],'GET /api/v1/scm/connections':()=>[]});
 render(<Editor editable={false}/>);
 await waitFor(()=>expect(screen.queryByText('Loading projects…')).toBeNull());
 for(const select of screen.getAllByRole('combobox'))expect((select as HTMLSelectElement).disabled).toBe(true);
 expect(screen.getByText('An administrator can change these workflow defaults.')).not.toBeNull();
});
