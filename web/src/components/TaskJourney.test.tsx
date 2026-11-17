import {render,screen,cleanup} from '@testing-library/react';
import {afterEach,it,expect} from 'vitest';
import {TaskJourney} from './TaskJourney';
import {TaskDecisions} from './TaskDecisions';
import type {TaskProgress} from '../api/tasks';
afterEach(cleanup);
it('shows rejected decisions beside result guidance without inventing success',()=>{
 const progress={state:'succeeded',executions:[],decisions:[{id:'a',title:'Write file',status:'rejected'}]} as unknown as TaskProgress;
 render(<TaskJourney progress={progress}/>);expect(screen.getByText('Ready for review')).toBeTruthy();expect(screen.getByText(/Write file — rejected/)).toBeTruthy();expect(screen.getByText(/does not prove each tool succeeded/)).toBeTruthy();
});
it('renders the same approval identity in the task decision component',()=>{
 const approval={id:'a',graphId:'g',kind:'tool',title:'Read repository',status:'pending',createdAt:new Date().toISOString()};
 const progress={state:'running',executions:[],pendingApprovals:[approval]} as unknown as TaskProgress;
 render(<><TaskJourney progress={progress}/><TaskDecisions graphId="g" approvals={progress.pendingApprovals} canDecide={false} canAdmin={false} onChanged={()=>{}}/></>);
 expect(screen.getByText('Needs your decision')).toBeTruthy();expect(screen.getByText('Read repository')).toBeTruthy();expect(screen.queryByRole('button',{name:/^Approve$/})).toBeNull();
});
