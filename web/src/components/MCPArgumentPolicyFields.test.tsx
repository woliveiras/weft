import {cleanup,render,screen,within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it} from 'vitest';
import {useState} from 'react';
import {MCPArgumentPolicyFields} from './MCPArgumentPolicyFields';
import type {MCPConnectionInput} from '../api/types';
afterEach(cleanup);
it('edits independent exact rules and explicitly removes them without changing grants',async()=>{
 let current:MCPConnectionInput;
 function Host(){const [input,set]=useState<MCPConnectionInput>({id:'test',name:'Test',service:'generic',endpoint:'https://example.com',enabled:false});current=input;return <MCPArgumentPolicyFields input={input} onChange={set}/>;}
 render(<Host/>);const user=userEvent.setup();
 await user.click(screen.getByText('Tool argument restrictions'));
 await user.click(screen.getByRole('button',{name:'Add argument rule'}));
 const first=within(screen.getByRole('group',{name:'Argument rule 1'}));
 await user.type(first.getByLabelText('Remote tool name'),'read');
 await user.type(first.getByLabelText('Argument name'),'repo');
 await user.type(first.getByLabelText('Allowed values (one per line)'),'One\nTwo');
 await user.click(screen.getByRole('button',{name:'Add argument rule'}));
 expect(current!.argumentPolicy?.[0]).toEqual({tool:'read',argument:'repo',values:['One','Two']});
 expect(current!.argumentPolicy?.[1].tool).toBe('');
 await user.click(screen.getByRole('button',{name:'Remove rule 2'}));
 expect(current!.argumentPolicy).toHaveLength(1);
 await user.click(screen.getByRole('button',{name:'Remove rule 1'}));
 expect(current!.argumentPolicy).toBeUndefined();
 expect(current!.enabled).toBe(false);
});
it('edits whole combinations without mixing independent values or changing connection state',async()=>{
 let current:MCPConnectionInput;
 function Host(){const [input,set]=useState<MCPConnectionInput>({id:'test',name:'Test',service:'generic',endpoint:'https://example.com',enabled:false});current=input;return <MCPArgumentPolicyFields input={input} onChange={set}/>;}
 render(<Host/>);const user=userEvent.setup();
 await user.click(screen.getByText('Tool argument restrictions'));
 await user.click(screen.getByRole('button',{name:'Add combination rule'}));
 await user.type(screen.getByLabelText('Remote tool name'),'read');
 const first=within(screen.getByRole('group',{name:'Allowed combination 1'}));
 await user.type(first.getByLabelText('workspace'),'team-a');
 await user.type(first.getByLabelText('repo'),'one');
 await user.click(screen.getByRole('button',{name:/^Add combination$/}));
 const second=within(screen.getByRole('group',{name:'Allowed combination 2'}));
 await user.type(second.getByLabelText('workspace'),'team-b');
 await user.type(second.getByLabelText('repo'),'two');
 expect(current!.argumentPolicy).toEqual([{tool:'read',arguments:['workspace','repo'],combinations:[['team-a','one'],['team-b','two']]}]);
 await user.click(screen.getByRole('button',{name:'Remove combination 1'}));
 expect(current!.argumentPolicy?.[0].combinations).toEqual([['team-b','two']]);
 expect(current!.enabled).toBe(false);
});
