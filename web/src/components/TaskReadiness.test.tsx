import {cleanup,render,screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it,vi} from 'vitest';
import {TaskReadiness} from './TaskReadiness';
import type {Graph} from '../api/types';
import type {TaskInput} from '../api/tasks';
const input:TaskInput={title:'Work',request:'Inspect code',outcome:'freeform',graphId:'flow',graphVersion:2,nodeId:'worker'};
const graph={metadata:{id:'flow',name:'Work',version:2},spec:{nodes:[{id:'worker',runtime:{kind:'native'}}],defaults:{isolation:{mode:'docker'}}}} as Graph;
afterEach(cleanup);
it('reports unavailable discovery without asserting readiness or offering unauthorized repair',()=>{
 render(<TaskReadiness input={{...input,projectId:'gone'}} graph={graph} projectsLoading={false} projectsError="Project discovery failed" modelState="unavailable" admin={false} onRepair={vi.fn()} busy={false}/>);
 expect(screen.getByText('Unavailable: Project discovery failed')).toBeTruthy();
 expect(screen.getByText(/Provider discovery unavailable/)).toBeTruthy();
 expect(screen.getByText(/workflow · docker isolation/)).toBeTruthy();
 expect(screen.queryByRole('button',{name:'Configure models and return'})).toBeNull();
 expect(screen.getByText(/An administrator must repair/)).toBeTruthy();
});
it('explains distinct PR/issue/review setup and delegates repair without starting anything',async()=>{
 const repair=vi.fn();
 render(<TaskReadiness input={input} graph={graph} projectsLoading={false} modelState="configured" admin onRepair={repair} busy={false}/>);
 await userEvent.click(screen.getByText('Prepare this task for a specific goal'));
 for(const [goal,text] of [['pr',/SCM connections provide server-held Git/],['issue',/Jira REST access, MCP tools and SCM delivery/],['review',/Reading a PR does not authorize merging/]] as const){
  await userEvent.selectOptions(screen.getByLabelText('Setup goal'),goal);expect(screen.getByText(text)).toBeTruthy();
 }
 await userEvent.click(screen.getByRole('button',{name:'Configure connections and return'}));
 expect(repair.mock.calls).toEqual([['#/connections']]);
});
it('disables repair while a save is pending and marks echo models as unnecessary',async()=>{
 render(<TaskReadiness input={input} graph={{...graph,spec:{nodes:[{id:'worker',runtime:{kind:'echo'}}]}}} projectsLoading={false} modelState="missing" admin onRepair={vi.fn()} busy/>);
 await userEvent.click(screen.getByText('Prepare this task for a specific goal'));
 expect(screen.getByText('Not required for echo agents')).toBeTruthy();
 expect(screen.getByRole('button',{name:'Configure models and return'})).toHaveProperty('disabled',true);
});
