import {cleanup,render,screen} from '@testing-library/react';
import {afterEach,expect,it} from 'vitest';
import type {TaskReview} from '../api/tasks';
import {requiredConnections,TaskRequirementRepairs} from './TaskRequirementRepairs';
afterEach(cleanup);
const agents=[{id:'worker',model:{},isolation:'process',grants:[{name:'mcp.personal.issue_read'},{name:'mcp.personal.get_me'},{name:'filesystem.read'},{name:'mcp.*'},{name:'mcp.other.search'}]}] as NonNullable<TaskReview['agents']>;
it('derives unique exact connection references without interpreting wildcards as grants',()=>{expect(requiredConnections(agents)).toEqual(['other','personal']);});
it('keeps the saved task in place while repairing configuration',()=>{
 render(<TaskRequirementRepairs agents={agents}/>);
 const link=screen.getByRole('link',{name:'Review connection personal in another tab'});
 expect(link.getAttribute('href')).toBe('#/connections/c/personal/overview');expect(link.getAttribute('target')).toBe('_blank');expect(link.getAttribute('rel')).toContain('noopener');
 expect(screen.getByText(/No permissions are granted automatically/)).toBeTruthy();
});
