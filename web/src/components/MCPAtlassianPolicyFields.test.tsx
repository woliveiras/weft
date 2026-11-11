import {cleanup,render,screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it} from 'vitest';
import {useState} from 'react';
import {MCPAtlassianPolicyFields} from './MCPAtlassianPolicyFields';
import type {MCPConnectionInput} from '../api/types';
afterEach(cleanup);
it('requires explicit opt-in and preserves independent product/site choices',async()=>{
 let current:MCPConnectionInput;
 function Host(){const [input,set]=useState<MCPConnectionInput>({id:'test',name:'Test',service:'atlassian',endpoint:'https://mcp.atlassian.com/v2/mcp?tools=all',enabled:false});current=input;return <MCPAtlassianPolicyFields input={input} onChange={set}/>;}
 render(<Host/>);
 expect(screen.queryByLabelText('Allow Jira reads')).toBeNull();
 await userEvent.click(screen.getByLabelText('Restrict Atlassian products and sites'));
 await userEvent.click(screen.getByLabelText('Allow Confluence reads'));
 await userEvent.click(screen.getByLabelText('Allow Jira reads'));
 await userEvent.type(screen.getByLabelText('Allowed Atlassian cloud IDs (one per line)'),'11111111-1111-1111-1111-111111111111');
 expect(current!.atlassianPolicy).toEqual({products:['confluence'],cloudIds:['11111111-1111-1111-1111-111111111111']});
 expect(screen.getByText(/does not restrict individual projects/)).toBeTruthy();
 await userEvent.click(screen.getByLabelText('Restrict Atlassian products and sites'));
 expect(current!.atlassianPolicy).toBeUndefined();
});
