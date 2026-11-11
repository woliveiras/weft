import {cleanup,render,screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it,vi} from 'vitest';
import {ProjectsPage} from './ProjectsPage';
import {fakeApi,apiError} from '../test/fakeApi';
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.unstubAllGlobals();});
it('requires explicit path review and retains input on registration failure',async()=>{
 const backend=fakeApi({'GET /api/v1/projects':()=>[],'POST /api/v1/projects':()=>apiError(400,'Repository unavailable','invalid')});
 render(<ProjectsPage canEdit/>);
 await userEvent.type(screen.getByLabelText('Project name'),'Work');await userEvent.type(screen.getByLabelText('Repository path'),'/missing');
 expect((screen.getByRole('button',{name:'Register project'}) as HTMLButtonElement).disabled).toBe(true);
 await userEvent.click(screen.getByRole('checkbox'));await userEvent.click(screen.getByRole('button',{name:'Register project'}));
 await screen.findByText('Repository unavailable');expect((screen.getByLabelText('Repository path') as HTMLInputElement).value).toBe('/missing');
 expect(backend.find('POST','/api/v1/projects')[0].body).toEqual({name:'Work',path:'/missing'});
});
it('keeps viewer registration disabled',()=>{
 fakeApi({'GET /api/v1/projects':()=>[]});render(<ProjectsPage canEdit={false}/>);
 expect(screen.getByLabelText('Repository path').closest('fieldset')?.disabled).toBe(true);
 expect((screen.getByRole('button',{name:'Register project'}) as HTMLButtonElement).disabled).toBe(true);
});

it('reviews a native preference and preserves selection on a stale update',async()=>{
 const backend=fakeApi({'GET /api/v1/projects':()=>[{id:'p',revision:2,name:'Repo',path:'/fixture',head:'abc'}],'PUT /api/v1/projects/p/preferences':()=>apiError(409,'Project changed; refresh projects','conflict')});
 render(<ProjectsPage canEdit nativeProfile/>);
 await screen.findByText('Repo');await userEvent.selectOptions(screen.getByLabelText('Preferred execution environment'),'local');await userEvent.click(screen.getByRole('button',{name:'Save environment preference'}));await screen.findByText('Project changed; refresh projects');
 expect(backend.find('PUT','/api/v1/projects/p/preferences')[0].body).toEqual({executionMode:'local'});expect(backend.find('PUT','/api/v1/projects/p/preferences')[0].headers['If-Match']).toBe('2');expect((screen.getByLabelText('Preferred execution environment') as HTMLSelectElement).value).toBe('local');
});
