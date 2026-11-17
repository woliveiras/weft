import {render,screen,fireEvent} from '@testing-library/react';
import {describe,it,expect,vi} from 'vitest';
import {TaskIssueSource,issueConnections} from './TaskIssueSource';
describe('task issue sources',()=>{
 it('only offers exact read grants',()=>{
  expect(issueConnections([{name:'mcp.personal.issue_read'},{name:'mcp.personal.issue_write'},{name:'mcp.*.issue_read'},{name:'mcp.work.getJiraIssue'}],'github')).toEqual(['personal']);
 });
 it('retains unavailable selections and makes reading separate from saving',()=>{
  const change=vi.fn();render(<TaskIssueSource source={{kind:'github',connectionId:'personal',reference:'o/r#1'}} grants={[]} onChange={change}/>);
  expect((screen.getByLabelText('Issue connection') as HTMLSelectElement).value).toBe('personal');
  expect(screen.getByText(/Saving does not fetch/)).toBeTruthy();
  fireEvent.change(screen.getByLabelText('Issue reference'),{target:{value:'o/r#2'}});
  expect(change).toHaveBeenCalledWith({kind:'github',connectionId:'personal',reference:'o/r#2'});
 });
});
