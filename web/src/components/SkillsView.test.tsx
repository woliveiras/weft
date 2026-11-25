import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { fakeApi, apiError } from "../test/fakeApi";
import { SkillsView } from "./SkillsView";
import { InstructionManifest } from "./InstructionManifest";
import type { Skill, SkillInput } from "../api/instructions";
const skill: Skill = {
  id: "review",
  title: "Review",
  description: "",
  body: "Review code",
  enabled: true,
  global: false,
  revision: 1,
  actor: "admin",
  at: "2026-10-16",
  deleted: false,
  history: [],
};
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
it("renders omissions and text without executing Markdown/HTML", () => {
  render(
    <InstructionManifest
      snapshot={{
        graphId: "g",
        nodeId: "n",
        budget: 10,
        entries: [
          {
            scope: "project",
            source: "bad.md",
            included: false,
            reason: "unsafe_path",
          },
          {
            scope: "graph",
            source: "review",
            body: "<script>bad()</script>",
            included: true,
            sha256: "abc",
          },
        ],
      }}
    />,
  );
  expect(screen.getByText("Omitted: unsafe_path")).toBeTruthy();
  expect(screen.getByText("<script>bad()</script>")).toBeTruthy();
  expect(document.querySelector("script")).toBeNull();
});
it("creates, versions and deletes through the API client with CAS and retains conflict edits", async () => {
  const routes = fakeApi({
    "GET /api/v1/skills": () => [skill],
    "POST /api/v1/skills": () => skill,
    "PUT /api/v1/skills/review": () => apiError(409, "Revision changed", "conflict"),
    "DELETE /api/v1/skills/review": () => ({...skill,revision:2,deleted:true,enabled:false,history:[skill]}),
  });
  render(<SkillsView role="admin" onChanged={() => {}} />);
  await screen.findByRole('link',{name:'Review'});
  fireEvent.click(screen.getByRole('link',{name:'New skill'}));
  fireEvent.change(screen.getByLabelText('Skill ID'),{target:{value:'review'}});
  fireEvent.change(screen.getByLabelText('Title'),{target:{value:'Review'}});
  fireEvent.change(screen.getByLabelText('Markdown body'),{target:{value:'Review code'}});
  fireEvent.click(screen.getByRole('button',{name:'Save'}));
  await waitFor(()=>expect(routes.find('POST','/api/v1/skills')).toHaveLength(1));
  await waitFor(()=>expect((screen.getByLabelText('Skill ID') as HTMLInputElement).disabled).toBe(true));
  fireEvent.change(screen.getByLabelText('Markdown body'),{target:{value:'Unsaved edit'}});
  fireEvent.click(screen.getByRole('button',{name:'Save'}));
  await screen.findByText('Revision changed');
  expect((screen.getByLabelText('Markdown body') as HTMLTextAreaElement).value).toBe('Unsaved edit');
  expect(routes.find('PUT','/api/v1/skills/review')[0].headers['If-Match']).toBe('1');
  vi.stubGlobal('confirm',()=>false);
  fireEvent.click(screen.getByRole('button',{name:'Delete skill'}));
  expect(routes.find('DELETE','/api/v1/skills/review')).toHaveLength(0);
  vi.stubGlobal('confirm',()=>true);
  fireEvent.click(screen.getByRole('button',{name:'Delete skill'}));
  await screen.findByText('Skill deleted. Revision history is retained.');
  fireEvent.click(screen.getByRole('link',{name:'History'}));
  expect(await screen.findByText('Revision 2 · admin · 2026-10-16 · Deleted')).toBeTruthy();
});
it("associates graph and narrowed agents and inspects saved/execution manifests", async () => {
  const graph={apiVersion:'weft.io/v1',kind:'Graph',metadata:{id:'g',name:'Graph',version:3},spec:{nodes:[{id:'n',prompt:'task'}]}};
  const snapshot={graphId:'g',nodeId:'n',budget:8,entries:[{scope:'graph',source:'review',included:true,body:'Review code'}]};
  const routes=fakeApi({
    'GET /api/v1/skills':()=>[skill],'GET /api/v1/graphs':()=>[{graph}],
    'PUT /api/v1/graphs/g':({body})=>({graph:body}),
    'GET /api/v1/graphs/g/instructions':()=>snapshot,
    'GET /api/v1/graphs/g/executions':()=>[{id:'exec',graphId:'g',nodeId:'n',status:'succeeded',startedAt:'2026-11-19'}],
    'GET /api/v1/executions/exec/instructions':()=>({...snapshot,executionId:'exec'}),
  });
  render(<SkillsView role="admin" route={undefined} onChanged={()=>{}}/>);
  fireEvent.click(screen.getByRole('link',{name:'Workflow associations'}));
  await screen.findByRole('option',{name:'Graph'});
  fireEvent.change(screen.getByLabelText('Workflow'),{target:{value:'g'}});
  fireEvent.click(screen.getByLabelText('Review (review)'));
  fireEvent.click(screen.getByText('n · Inherited skills'));
  fireEvent.click(screen.getByLabelText('Inherit graph skills'));
  fireEvent.click(screen.getByRole('button',{name:'Review associations'}));
  fireEvent.click(screen.getByRole('button',{name:'Save associations'}));
  await waitFor(()=>expect(routes.find('PUT','/api/v1/graphs/g')).toHaveLength(1));
  const req=routes.find('PUT','/api/v1/graphs/g')[0];
  expect(req.headers['If-Match']).toBe('3');
  expect((req.body as typeof graph & {spec:{nodes:{skillIds:string[]}[]}}).spec.nodes[0].skillIds).toEqual([]);
  await screen.findByText('Associations saved in workflow version 3.');
  await waitFor(()=>expect(screen.getByRole('button',{name:'Review associations'}).matches(':disabled')).toBe(true));
  fireEvent.click(screen.getByRole('link',{name:'Inspect instructions'}));
  await screen.findByRole('button',{name:'Inspect saved instructions'});
  await waitFor(()=>expect((screen.getByLabelText('Workflow') as HTMLSelectElement).value).toBe('g'));
  fireEvent.click(screen.getByRole('button',{name:'Inspect saved instructions'}));
  await screen.findByText('Current saved configuration (not an execution)');
  fireEvent.click(screen.getByRole('button',{name:'Load executions'}));
  fireEvent.click(await screen.findByRole('button',{name:'Inspect n · exec'}));
  await screen.findByText('Execution exec');
});
it("keeps catalogue writes hidden and direct forms disabled for viewers", async () => {
  fakeApi({'GET /api/v1/skills':()=>[skill]});
  const {unmount}=render(<SkillsView role="viewer" onChanged={()=>{}}/>);
  await screen.findByRole('link',{name:'Review'});
  expect(screen.queryByRole('link',{name:'New skill'})).toBeNull();unmount();
  render(<SkillsView role="viewer" route={{skillId:'review'}} onChanged={()=>{}}/>);
  const content=await screen.findByRole('form',{name:'Skill content'});
  expect(within(content).getByLabelText('Title').matches(':disabled')).toBe(true);
  expect(screen.queryByRole('button',{name:'Save'})).toBeNull();
});
it("blocks inspection selection during requests and keeps operator associations read-only", async () => {
  let resolve!:(value:unknown)=>void;
  const graph={metadata:{id:'g',name:'Graph',version:1},spec:{nodes:[{id:'n'}]}};
  fakeApi({'GET /api/v1/skills':()=>[skill],'GET /api/v1/graphs':()=>[{graph}],
    'GET /api/v1/graphs/g/instructions':()=>new Promise(r=>{resolve=r;})});
  const {unmount}=render(<SkillsView role="operator" route={{section:'associations',workflow:'g'}} onChanged={()=>{}}/>);
  await screen.findByRole('button',{name:'Review associations'});
  expect(screen.getByRole('button',{name:'Review associations'}).matches(':disabled')).toBe(true);unmount();
  render(<SkillsView role="operator" route={{section:'inspect',workflow:'g'}} onChanged={()=>{}}/>);
  await screen.findByRole('button',{name:'Inspect saved instructions'});
  fireEvent.click(screen.getByRole('button',{name:'Inspect saved instructions'}));
  await waitFor(()=>expect((screen.getByLabelText('Workflow') as HTMLSelectElement).disabled).toBe(true));
  expect((screen.getByLabelText('Agent') as HTMLSelectElement).disabled).toBe(true);
  resolve({graphId:'g',nodeId:'n',budget:100,entries:[]});
  await screen.findByText('Current saved configuration (not an execution)');
  await waitFor(()=>expect((screen.getByLabelText('Workflow') as HTMLSelectElement).disabled).toBe(false));
});
it("shows built-in role skills as read-only even for administrators", async () => {
  const builtin:Skill={...skill,id:'weft-planner',title:'Planner role',actor:'weft',builtin:true};
  const routes=fakeApi({'GET /api/v1/skills':()=>[builtin,skill]});
  render(<SkillsView role="admin" route={{skillId:'weft-planner'}} onChanged={()=>{}}/>);
  expect((await screen.findByRole('note')).textContent).toContain('cannot be edited');
  expect(screen.getByLabelText('Markdown body').matches(':disabled')).toBe(true);
  expect(screen.queryByRole('button',{name:'Save'})).toBeNull();
  expect(screen.queryByRole('button',{name:'Delete skill'})).toBeNull();
  expect(routes.calls.every(call=>call.method==='GET')).toBe(true);
});

it('combines catalogue filters with bounded newest-first pages and explicit retry',async()=>{
  let fail=true;
  const data=Array.from({length:22},(_,i)=>({...skill,id:`skill-${i}`,title:`Skill ${i}`,at:`2026-11-${String(i+1).padStart(2,'0')}`,builtin:i===0,enabled:i!==1}));
  fakeApi({'GET /api/v1/skills':()=>fail?apiError(503,'Catalogue unavailable','unavailable'):data});
  render(<SkillsView role="admin" onChanged={()=>{}}/>);
  await screen.findByText('Catalogue unavailable');
  expect(screen.queryByRole('table')).toBeNull();
  fail=false;fireEvent.click(screen.getByRole('button',{name:'Retry loading skills'}));
  const table=await screen.findByRole('table');
  expect(within(table).getAllByRole('row')).toHaveLength(21);
  expect(within(table).getAllByRole('row')[1].textContent).toContain('Skill 21');
  fireEvent.click(screen.getByRole('button',{name:'Next'}));
  expect(within(table).getAllByRole('row')).toHaveLength(3);
  fireEvent.change(screen.getByLabelText('Skill status'),{target:{value:'disabled'}});
  fireEvent.change(screen.getByLabelText('Search skills'),{target:{value:'Skill 1'}});
  fireEvent.change(screen.getByLabelText('Origin'),{target:{value:'custom'}});
  expect(within(table).getAllByRole('row')).toHaveLength(2);
  fireEvent.change(screen.getByLabelText('Origin'),{target:{value:'builtin'}});
  expect(screen.getByText('No skills match these filters.')).toBeTruthy();
});
it('retains skill edits across detail tabs and rejects invalid UTF-8 byte size before writing',async()=>{
  const fixture=fakeApi({'GET /api/v1/skills':()=>[skill],'GET /api/v1/graphs':()=>[]});
  render(<SkillsView role="admin" onChanged={()=>{}}/>);
  fireEvent.click(await screen.findByRole('link',{name:'Review'}));
  fireEvent.change(screen.getByLabelText('Title'),{target:{value:'Changed title'}});
  fireEvent.click(screen.getByRole('link',{name:'Workflows'}));
  await screen.findByText('No saved workflow uses this skill.');
  fireEvent.click(screen.getByRole('link',{name:'Content'}));
  expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('Changed title');
  expect(screen.getByText('Unsaved changes')).toBeTruthy();
  fireEvent.change(screen.getByLabelText('Markdown body'),{target:{value:'é'.repeat(8193)}});
  fireEvent.click(screen.getByRole('link',{name:'Workflows'}));
  await screen.findByText('No saved workflow uses this skill.');
  fireEvent.click(screen.getByRole('button',{name:'Save'}));
  expect(await screen.findByText('Markdown instructions must fit within 16 KiB of UTF-8 text.')).toBeTruthy();
  expect(screen.getByLabelText('Title')).toHaveProperty('value','Changed title');
  expect(screen.getByLabelText('Markdown body').getAttribute('aria-invalid')).toBe('true');
  await waitFor(()=>expect(document.activeElement).toBe(screen.getByLabelText('Markdown body')));
  expect(fixture.find('PUT','/api/v1/skills/review')).toHaveLength(0);
});
it('shows a failed association save without discarding narrowed selection',async()=>{
  const graph={metadata:{id:'g',name:'Graph',version:3},spec:{nodes:[{id:'n'}]}};
  fakeApi({'GET /api/v1/skills':()=>[skill],'GET /api/v1/graphs':()=>[{graph}],
    'PUT /api/v1/graphs/g':()=>apiError(409,'Workflow changed; reload before saving','conflict')});
  render(<SkillsView role="admin" route={{section:'associations',workflow:'g'}} onChanged={()=>{}}/>);
  fireEvent.click(await screen.findByLabelText('Review (review)'));
  fireEvent.click(screen.getByRole('button',{name:'Review associations'}));
  fireEvent.click(screen.getByRole('button',{name:'Save associations'}));
  await screen.findByText('Workflow changed; reload before saving');
  expect((screen.getByLabelText('Review (review)') as HTMLInputElement).checked).toBe(true);
  expect(screen.getByText('Unsaved associations')).toBeTruthy();
});
it('rejects mismatched execution snapshots rather than showing another workflow as this selection',async()=>{
  const graph={metadata:{id:'g',name:'Graph',version:1},spec:{nodes:[{id:'n'}]}};
  fakeApi({'GET /api/v1/skills':()=>[],'GET /api/v1/graphs':()=>[{graph}],
    'GET /api/v1/executions/other/instructions':()=>({graphId:'other',nodeId:'n',budget:1,entries:[]})});
  render(<SkillsView role="viewer" route={{section:'inspect',workflow:'g',agent:'n',execution:'other'}} onChanged={()=>{}}/>);
  expect(await screen.findByText('This execution does not belong to the selected workflow and agent.')).toBeTruthy();
  expect(screen.queryByLabelText('Instruction manifest')).toBeNull();
});

it('asks once before discarding association edits when switching workflows',async()=>{
  const graphs=['first','second'].map(id=>({graph:{metadata:{id,name:id,version:1},spec:{nodes:[{id:'n'}]}}}));
  fakeApi({'GET /api/v1/skills':()=>[skill],'GET /api/v1/graphs':()=>graphs});
  const confirm=vi.spyOn(window,'confirm').mockReturnValue(false);
  render(<SkillsView role="admin" onChanged={()=>{}}/>);
  fireEvent.click(await screen.findByRole('link',{name:'Workflow associations'}));
  fireEvent.change(await screen.findByLabelText('Workflow'),{target:{value:'first'}});
  fireEvent.click(await screen.findByLabelText('Review (review)'));
  fireEvent.change(screen.getByLabelText('Workflow'),{target:{value:'second'}});
  expect(confirm).toHaveBeenCalledTimes(1);
  expect((screen.getByLabelText('Workflow') as HTMLSelectElement).value).toBe('first');
  expect((screen.getByLabelText('Review (review)') as HTMLInputElement).checked).toBe(true);
  confirm.mockReturnValue(true);
  fireEvent.change(screen.getByLabelText('Workflow'),{target:{value:'second'}});
  expect(confirm).toHaveBeenCalledTimes(2);
  expect((screen.getByLabelText('Workflow') as HTMLSelectElement).value).toBe('second');
  expect((screen.getByLabelText('Review (review)') as HTMLInputElement).checked).toBe(false);
});

it('opens a built-in copy as an editable unsaved draft and preserves it on create failure',async()=>{
  const builtin={...skill,id:'weft-planner',title:'Planner role',builtin:true};
  let fail=true;
  const routes=fakeApi({'GET /api/v1/skills':()=>[builtin],
    'POST /api/v1/skills':({body})=>fail?apiError(409,'ID already exists','conflict'):{...(body as SkillInput),revision:1,actor:'admin',at:'2026-11-19',deleted:false,history:[]}});
  render(<SkillsView role="admin" onChanged={()=>{}}/>);
  fireEvent.click(await screen.findByRole('link',{name:'Planner role'}));
  expect(screen.getByText('Built-in · Read-only')).toBeTruthy();
  fireEvent.click(screen.getByRole('link',{name:'Create editable copy'}));
  expect((screen.getByLabelText('Skill ID') as HTMLInputElement).value).toBe('planner-copy');
  expect(screen.getByLabelText('Skill ID').matches(':disabled')).toBe(false);
  expect((screen.getByLabelText('Markdown body') as HTMLTextAreaElement).value).toBe(builtin.body);
  expect(screen.getByText('Unsaved changes')).toBeTruthy();
  expect(routes.calls.every(call=>call.method==='GET')).toBe(true);
  fireEvent.change(screen.getByLabelText('Title'),{target:{value:'Custom planner'}});
  fireEvent.click(screen.getByRole('button',{name:'Save'}));
  await screen.findByText('ID already exists');
  expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('Custom planner');
  fail=false;fireEvent.click(screen.getByRole('button',{name:'Save'}));
  await screen.findByText('Skill saved as revision 1.');
  expect(routes.find('POST','/api/v1/skills')[1].body).toMatchObject({id:'planner-copy',title:'Custom planner',global:false});
  expect(routes.find('PUT','/api/v1/skills/weft-planner')).toHaveLength(0);
});
it('keeps built-in copy actions hidden for viewers and handles a missing copy source',async()=>{
  const builtin={...skill,id:'weft-planner',builtin:true};
  fakeApi({'GET /api/v1/skills':()=>[builtin]});
  const {unmount}=render(<SkillsView role="viewer" route={{skillId:builtin.id}} onChanged={()=>{}}/>);
  await screen.findByText('Built-in · Read-only');
  expect(screen.queryByRole('link',{name:'Create editable copy'})).toBeNull();unmount();
  render(<SkillsView role="admin" route={{create:true,copyFrom:'missing'}} onChanged={()=>{}}/>);
  await screen.findByRole('heading',{name:'Source skill not found'});
  expect(screen.queryByRole('button',{name:'Save'})).toBeNull();
});

it('identifies missing skill fields after save and reveals invalid Markdown while previewing',async()=>{
 const fixture=fakeApi({'GET /api/v1/skills':()=>[]});
 render(<SkillsView role="admin" onChanged={()=>{}} route={{create:true}}/>);
 await screen.findByLabelText('Skill ID');
 fireEvent.change(screen.getByLabelText('Skill ID'),{target:{value:'new-skill'}});
 expect(screen.queryByRole('alert')).toBeNull();
 fireEvent.click(screen.getByRole('button',{name:'Preview'}));
 fireEvent.click(screen.getByRole('button',{name:'Save'}));
 const title=screen.getByLabelText('Title');const body=screen.getByLabelText('Markdown body');
 expect(title.getAttribute('aria-invalid')).toBe('true');
 expect(body.getAttribute('aria-invalid')).toBe('true');
 expect(document.activeElement).toBe(title);
 expect(fixture.find('PUT','/api/v1/skills/new-skill')).toHaveLength(0);
 fireEvent.change(title,{target:{value:'New skill'}});
 expect(title.getAttribute('aria-invalid')).toBeNull();
 expect(body.getAttribute('aria-invalid')).toBe('true');
});

it('reviews association changes before writing and cancellation retains the draft',async()=>{
 const graph={metadata:{id:'g',name:'Delivery workflow',version:3},spec:{nodes:[{id:'n'}]}};
 const routes=fakeApi({'GET /api/v1/skills':()=>[skill],'GET /api/v1/graphs':()=>[{graph}],
  'PUT /api/v1/graphs/g':({body})=>({graph:body})});
 render(<SkillsView role="admin" route={{section:'associations',workflow:'g'}} onChanged={()=>{}}/>);
 fireEvent.click(await screen.findByLabelText('Review (review)'));
 fireEvent.click(screen.getByRole('button',{name:'Review associations'}));
 const review=screen.getByRole('dialog',{name:'Review workflow associations'});
 expect(within(review).getByText(/Delivery workflow.*version 3/)).toBeTruthy();
 expect(routes.find('PUT','/api/v1/graphs/g')).toHaveLength(0);
 fireEvent.click(within(review).getByRole('button',{name:'Cancel'}));
 expect(screen.getByLabelText('Review (review)')).toHaveProperty('checked',true);
 expect(routes.find('PUT','/api/v1/graphs/g')).toHaveLength(0);
 fireEvent.click(screen.getByRole('button',{name:'Review associations'}));
 fireEvent.click(screen.getByRole('button',{name:'Save associations'}));
 await waitFor(()=>expect(routes.find('PUT','/api/v1/graphs/g')).toHaveLength(1));
 expect(routes.find('PUT','/api/v1/graphs/g')[0].headers['If-Match']).toBe('3');
});
