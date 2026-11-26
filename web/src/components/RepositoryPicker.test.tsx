import {useState} from 'react';
import {cleanup,fireEvent,render,screen,within} from '@testing-library/react';
import {afterEach,expect,it} from 'vitest';
import type {Project} from '../api/tasks';
import {RepositoryPicker, RepositoryRows, repositoryBindings} from './RepositoryPicker';

const projects:Project[]=['infra','backend','frontend'].map((name,i)=>({
  id:'prj_'+i,revision:1,name,path:'/projects/'+name,head:String(i).repeat(40),createdAt:''
}));
afterEach(cleanup);

it('keeps reviewed bindings through reordering and identifies a missing suggestion',()=>{
  const saved=repositoryBindings(projects.map(p=>p.id),projects);
  const pins=repositoryBindings([projects[2].id,'prj_missing',projects[0].id],[],saved);
  expect(pins).toEqual([saved[2],expect.objectContaining({projectId:'prj_missing',baseCommit:''}),saved[0]]);
  expect(repositoryBindings([],projects,saved)).toEqual([]);
});

it('toggles immediately in one searchable dropdown and keeps changes when it closes', () => {
  function Editor() {
    const [ids,setIds] = useState<string[]>([]);
    return <><RepositoryPicker label="Repositories" help="Choose repositories" projects={projects} ids={ids} onChange={setIds}/>
      <output data-testid="selected">{ids.join(',')}</output><button>Next field</button></>;
  }
  render(<Editor/>);
  const field = screen.getByRole('combobox',{name:'Repositories'});
  field.focus();
  fireEvent.keyDown(field,{key:'ArrowDown'});
  fireEvent.keyDown(field,{key:'Enter'});
  expect(screen.getByTestId('selected').textContent).toBe('prj_0');
  fireEvent.change(field,{target:{value:'/projects/frontend'}});
  fireEvent.click(screen.getByRole('option',{name:/frontend/}));
  expect(screen.getByTestId('selected').textContent).toBe('prj_0,prj_2');
  fireEvent.change(field,{target:{value:'missing'}});
  expect(screen.getByText('No matching repositories.')).toBeTruthy();
  fireEvent.keyDown(field,{key:'Escape'});
  expect(screen.queryByRole('listbox')).toBeNull();
  expect(document.activeElement).toBe(field);
  expect(field).toHaveProperty('value','infra, frontend');
  fireEvent.click(field);
  expect(screen.getByRole('option',{name:/infra/}).getAttribute('aria-selected')).toBe('true');
  fireEvent.click(screen.getByRole('option',{name:/infra/}));
  fireEvent.pointerDown(screen.getByRole('button',{name:'Next field'}));
  expect(screen.queryByRole('listbox')).toBeNull();
  expect(field).toHaveProperty('value','frontend');
});

it('shows the pinned commit, delivery target and check inputs for each reviewed alias', () => {
  const rows = repositoryBindings(projects.map(p => p.id), projects).map((row, i) => ({
    ...row, repository: 'acme/' + row.alias, targetBranch: 'release-' + i, scmConnection: 'scm-' + i,
    checks: [{name: 'test', command: 'sh verify.sh'}], requiredChecks: ['test']
  }));
  render(<RepositoryRows repos={[rows[2],rows[0],rows[1]]} projects={projects}/>);
  for (const [i, row] of rows.entries()) {
    const region = within(screen.getByRole('region', {name: row.alias}));
    expect(region.getByText(row.baseCommit.slice(0, 12))).toBeTruthy();
    fireEvent.click(region.getByText('Repository details'));
    expect(region.getByText(row.baseCommit)).toBeTruthy();
    expect(region.getByText('PR → acme/' + row.alias + ' · release-' + i + ' · scm-' + i)).toBeTruthy();
    expect(region.getByText(row.alias + '.test · required')).toBeTruthy();
    expect(region.getByText(/Inputs: frontend, infra, backend/)).toBeTruthy();
  }
});
