import {cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
import {fakeApi,apiError} from '../test/fakeApi';
import {AgentsPage} from './AgentsPage';
import {AgentInstance} from './AgentBinding';
import type {Agent} from '../api/agents';
import type {GraphNode,GraphSummary} from '../api/types';

const agent:Agent={id:'reviewer',versions:[
 {version:1,definition:{name:'Reviewer',prompt:'Old instructions',parameters:[{name:'repository'}]},createdAt:'',createdBy:'admin'},
 {version:2,definition:{name:'Release reviewer',prompt:'New instructions',readOnly:true,parameters:[{name:'repository'}]},createdAt:'',createdBy:'admin'},
]};
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.unstubAllGlobals();});
function catalog(onDraftState=vi.fn()) {
 return render(<AgentsPage id="reviewer" workflows={[]} workflowsLoading={false} onReloadWorkflows={async()=>{}} canEdit onDraftState={onDraftState}/>);
}
it('keeps unpublished edits on a refused version change and failed publication',async()=>{
 const routes=fakeApi({'GET /api/v1/agents/reviewer':()=>agent,'PUT /api/v1/agents/reviewer':()=>apiError(409,'A newer version was published','conflict')});
 const draft=vi.fn();catalog(draft);
 const name=await screen.findByLabelText('Name');
 fireEvent.change(name,{target:{value:'My unpublished name'}});
 fireEvent.click(screen.getByRole('button',{name:'Instructions'}));
 fireEvent.change(screen.getByLabelText('Agent instructions'),{target:{value:'My unpublished instructions'}});
 const confirm=vi.spyOn(window,'confirm').mockReturnValue(false);
 fireEvent.change(screen.getByLabelText('Version'),{target:{value:'1'}});
 expect(confirm).toHaveBeenCalledWith('Discard unpublished agent changes?');
 expect(screen.getByLabelText('Version')).toHaveProperty('value','2');
 expect(screen.getByLabelText('Agent instructions')).toHaveProperty('value','My unpublished instructions');
 const unload=new Event('beforeunload',{cancelable:true});window.dispatchEvent(unload);expect(unload.defaultPrevented).toBe(true);
 fireEvent.click(screen.getByRole('button',{name:'Publish'}));
 await screen.findByText('A newer version was published');
 expect(screen.getByLabelText('Agent instructions')).toHaveProperty('value','My unpublished instructions');
 expect(routes.find('PUT','/api/v1/agents/reviewer')[0].headers['If-Match']).toBe('2');
 await waitFor(()=>expect(draft).toHaveBeenLastCalledWith({dirty:true,busy:false}));
 confirm.mockReturnValue(true);fireEvent.change(screen.getByLabelText('Version'),{target:{value:'1'}});
 fireEvent.click(screen.getByRole('button',{name:'Instructions'}));
 await waitFor(()=>expect(screen.getByLabelText('Agent instructions')).toHaveProperty('value','Old instructions'));
 expect(screen.getByRole('note').textContent).toContain('based on version 1');
 expect(screen.queryByText('Unpublished changes')).toBeNull();
});
it('keeps the pinned binding through catalog failures and retries without adopting another version',async()=>{
 let available=false;
 fakeApi({'GET /api/v1/agents/reviewer':()=>available?agent:apiError(503,'Agent catalog unavailable','unavailable')});
 const node:GraphNode={id:'n',agent:{id:'reviewer',version:1,parameters:{repository:'team/project'}}};
 const summary={graph:{metadata:{id:'workflow'},spec:{nodes:[node]}},record:{version:1}} as GraphSummary;
 const update=vi.fn();render(<AgentInstance node={node} summary={summary} canEdit={false} onUpdate={update}/>);
 await screen.findByText('Agent catalog unavailable');
 expect(screen.getByLabelText('Agent version')).toHaveProperty('value','1');
 expect(screen.getByLabelText('Agent version')).toHaveProperty('disabled',true);
 available=true;fireEvent.click(screen.getByRole('button',{name:'Retry loading definition'}));
 await screen.findByRole('option',{name:'Version 2'});
 expect(screen.getByLabelText('Agent version')).toHaveProperty('disabled',true);
 expect(screen.getByLabelText('Agent version')).toHaveProperty('value','1');
 expect(update).not.toHaveBeenCalled();
});
it('publishes all tab edits once without adopting them in workflows',async()=>{
 const routes=fakeApi({'GET /api/v1/agents/reviewer':()=>agent,'PUT /api/v1/agents/reviewer':({body})=>({...agent,versions:[...agent.versions,{version:3,definition:(body as {definition:unknown}).definition,createdAt:'',createdBy:'admin'}]})});
 catalog();fireEvent.change(await screen.findByLabelText('Name'),{target:{value:'Published name'}});
 fireEvent.click(screen.getByRole('button',{name:'Instructions'}));
 fireEvent.change(screen.getByLabelText('Agent instructions'),{target:{value:'Published guidance'}});
 expect(screen.getByText(/Workflows keep their pinned versions/)).toBeTruthy();
 fireEvent.click(screen.getByRole('button',{name:'Publish'}));
 await waitFor(()=>expect(screen.getByLabelText('Version')).toHaveProperty('value','3'));
 expect(routes.find('PUT','/api/v1/agents/reviewer')[0].body).toMatchObject({id:'reviewer',definition:{name:'Published name',prompt:'Published guidance'}});
 expect(routes.calls.filter(call=>call.method!=='GET')).toHaveLength(1);
 expect(screen.queryByText('Unpublished changes')).toBeNull();
});
it('locks a new agent identifier until its publication finishes',async()=>{
 let finish!:(value:Agent)=>void;
 fakeApi({'POST /api/v1/agents':()=>new Promise<Agent>(resolve=>{finish=resolve;})});
 const draft=vi.fn();render(<AgentsPage create workflows={[]} workflowsLoading={false} onReloadWorkflows={async()=>{}} canEdit onDraftState={draft}/>);
 const id=screen.getByLabelText('Agent ID');
 fireEvent.change(id,{target:{value:'new-reviewer'}});
 fireEvent.change(screen.getByLabelText('Name'),{target:{value:'New reviewer'}});
 fireEvent.click(screen.getByRole('button',{name:'Publish'}));
 await screen.findByText('Publishing…');
 expect(id).toHaveProperty('readOnly',true);
 expect(draft).toHaveBeenLastCalledWith({dirty:true,busy:true});
 finish({id:'new-reviewer',versions:[{version:1,definition:{name:'New reviewer'},createdAt:'',createdBy:'admin'}]});
 await screen.findByRole('link',{name:'Open New reviewer'});
 await waitFor(()=>expect(draft).toHaveBeenLastCalledWith({dirty:false,busy:false}));
});
it('previews a selected version and only changes the workflow binding on explicit adoption',async()=>{
 fakeApi({'GET /api/v1/agents/reviewer':()=>agent});
 const node:GraphNode={id:'n',name:'Reviewer',prompt:'Old instructions',agent:{id:'reviewer',version:1,parameters:{repository:'team/project'}},localInstructions:'Local scope',loop:{mode:'event'}};
 const summary={graph:{metadata:{id:'workflow',name:'Workflow'},spec:{nodes:[node]}},record:{version:1}} as GraphSummary;
 const update=vi.fn();render(<AgentInstance node={node} summary={summary} canEdit onUpdate={update}/>);
 await screen.findByRole('option',{name:'Version 2'});
 fireEvent.change(screen.getByLabelText('Agent version'),{target:{value:'2'}});
 expect(update).not.toHaveBeenCalled();
 expect(screen.getByRole('region',{name:'Version adoption review'})).toBeTruthy();
 expect(screen.getByText(/Updated sections:.*name/)).toBeTruthy();
 fireEvent.click(screen.getByRole('button',{name:'Cancel version change'}));
 expect(screen.getByLabelText('Agent version')).toHaveProperty('value','1');expect(update).not.toHaveBeenCalled();
 fireEvent.change(screen.getByLabelText('Agent version'),{target:{value:'2'}});
 fireEvent.click(screen.getByRole('button',{name:'Use this version'}));
 expect(update).toHaveBeenCalledTimes(1);
 expect(update.mock.calls[0][0]).toMatchObject({agent:{id:'reviewer',version:2,parameters:{repository:'team/project'}},localInstructions:'Local scope',loop:{mode:'event'},workspace:{readOnly:true}});
 expect(within(screen.getByRole('region',{name:'Agent definition'})).getByText(/This workflow/)).toBeTruthy();
});
