import {cleanup,render,screen,within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it,vi} from 'vitest';
import {ProjectsPage} from './ProjectsPage';
import {fakeApi,apiError} from '../test/fakeApi';
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.unstubAllGlobals();});
it('requires explicit path review and retains input on registration failure',async()=>{
 const backend=fakeApi({'GET /api/v1/projects':()=>[],'POST /api/v1/projects':()=>apiError(400,'Repository unavailable','invalid')});
 render(<ProjectsPage canEdit mode="register"/>);
 await userEvent.type(screen.getByLabelText('Project name'),'Work');await userEvent.type(screen.getByLabelText('Repository path'),'/missing');
 expect((screen.getByRole('button',{name:'Register project'}) as HTMLButtonElement).disabled).toBe(true);
 await userEvent.click(screen.getByRole('checkbox'));await userEvent.click(screen.getByRole('button',{name:'Register project'}));
 await screen.findByText('Repository unavailable');expect((screen.getByLabelText('Repository path') as HTMLInputElement).value).toBe('/missing');
 expect(backend.find('POST','/api/v1/projects')[0].body).toEqual({name:'Work',path:'/missing'});
});
it('keeps viewer registration disabled',()=>{
 fakeApi({'GET /api/v1/projects':()=>[]});render(<ProjectsPage canEdit={false} mode="register"/>);
 expect(screen.getByLabelText('Repository path').closest('fieldset')?.disabled).toBe(true);
 expect((screen.getByRole('button',{name:'Register project'}) as HTMLButtonElement).disabled).toBe(true);
});

it('reviews a native preference and preserves selection on a stale update',async()=>{
 const backend=fakeApi({'GET /api/v1/projects':()=>[{id:'p',revision:2,name:'Repo',path:'/fixture',head:'abc'}],'PUT /api/v1/projects/p/preferences':()=>apiError(409,'Project changed; refresh projects','conflict')});
 render(<ProjectsPage canEdit nativeProfile/>);
 await screen.findByText('Repo');await userEvent.click(screen.getByText('Configure preference'));await userEvent.selectOptions(screen.getByLabelText('Preferred execution environment'),'local');await userEvent.click(screen.getByRole('button',{name:'Save environment preference'}));await screen.findByText('Project changed; refresh projects');
 expect(backend.find('PUT','/api/v1/projects/p/preferences')[0].body).toEqual({executionMode:'local'});expect(backend.find('PUT','/api/v1/projects/p/preferences')[0].headers['If-Match']).toBe('2');expect((screen.getByLabelText('Preferred execution environment') as HTMLSelectElement).value).toBe('local');
});


it('combines project search and environment filters while keeping registration off the list', async () => {
 const backend = fakeApi({
  'GET /api/v1/projects': () => [
   { id:'p', revision:1, name:'Application', path:'/srv/app', head:'abc', executionMode:'sandbox', repositoryUrl:'https://example.com/team/app.git' },
   { id:'q', revision:1, name:'Tools', path:'/srv/tools', head:'def' },
  ],
 });
 render(<ProjectsPage canEdit />);
 const table = await screen.findByRole('table', {name:'Registered projects'});
 expect(within(table).getAllByRole('row')).toHaveLength(3);
 expect(screen.queryByRole('form')).toBeNull();
 expect(screen.getByRole('link', {name:'Add project'}).getAttribute('href')).toBe('#/projects/new');
 const search = screen.getByRole('searchbox', {name:'Search projects'});
 await userEvent.type(search, ' TEAM/APP ');
 await userEvent.selectOptions(screen.getByRole('combobox', {name:'Execution environment'}), 'sandbox');
 expect(screen.getByText('1 of 2 projects')).toBeTruthy();
 expect(within(table).queryByText('Tools')).toBeNull();
 await userEvent.selectOptions(screen.getByRole('combobox', {name:'Execution environment'}), 'workflow');
 expect(screen.getByText('No projects match these filters.')).toBeTruthy();
 await userEvent.click(screen.getByRole('button', {name:'Clear filters'}));
 expect(document.activeElement).toBe(search);
 expect(screen.getByText('2 of 2 projects')).toBeTruthy();
 expect(backend.calls.every(call => call.method === 'GET')).toBe(true);
});

it('reports list failures, retries and preserves viewer access without registration controls', async () => {
 let failed = true;
 fakeApi({'GET /api/v1/projects': () => failed ? apiError(503,'Projects unavailable','unavailable') : []});
 render(<ProjectsPage canEdit={false} />);
 expect(await screen.findByText('Projects unavailable')).toBeTruthy();
 expect(screen.queryByText('No projects registered yet.')).toBeNull();
 expect(screen.queryByRole('link', {name:'Add project'})).toBeNull();
 failed = false;
 await userEvent.click(screen.getByRole('button', {name:'Refresh projects'}));
 expect(await screen.findByText('No projects registered yet.')).toBeTruthy();
});

it('requires a fresh path review after the repository path changes', async () => {
 fakeApi({'GET /api/v1/projects': () => []});
 render(<ProjectsPage canEdit mode="register" />);
 await userEvent.type(screen.getByLabelText('Project name'), 'Project');
 await userEvent.type(screen.getByLabelText('Repository path'), '/repo');
 await userEvent.click(screen.getByRole('checkbox'));
 const submit = screen.getByRole<HTMLButtonElement>('button', {name:'Register project'});
 expect(submit.disabled).toBe(false);
 await userEvent.type(screen.getByLabelText('Repository path'), '-changed');
 expect(submit.disabled).toBe(true);
});

it('opens the selected project preference and keeps other projects outside its scope',async()=>{
  const projects=['first','second'].map(id=>({id,name:'Same name',revision:1,path:'/fixture/'+id,head:'abc'}));
  fakeApi({'GET /api/v1/projects':()=>projects});
  render(<ProjectsPage canEdit selectedId="second"/>);
  const table=await screen.findByRole('table',{name:'Registered projects'});
  expect(within(table).queryByText('first')).toBeNull();
  expect(within(table).getByText('second')).toBeTruthy();
  expect(within(table).getByRole('region',{name:'Execution preference for Same name'}).closest('details')?.open).toBe(true);
  expect(screen.getByRole('link',{name:'Show all projects'}).getAttribute('href')).toBe('#/projects');
});
