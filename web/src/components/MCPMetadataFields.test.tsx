import {cleanup,render,screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {useState} from 'react';
import {afterEach,expect,it} from 'vitest';
import type {MCPConnectionInput} from '../api/types';
import {MCPMetadataFields} from './MCPMetadataFields';
afterEach(cleanup);
it('edits only known dbt metadata and removes cleared values',async()=>{
 let latest:MCPConnectionInput;
 function Host(){const [input,set]=useState<MCPConnectionInput>({id:'test',name:'Test',service:'generic',endpoint:'https://fixture.example/mcp',enabled:true});latest=input;return <MCPMetadataFields input={input} onChange={set}/>;}
 render(<Host/>);
 await userEvent.click(screen.getByText('dbt environment settings (optional)'));
 await userEvent.type(screen.getByLabelText('dbt production environment ID'),'123');
 expect(latest!.metadataHeaders).toEqual({'x-dbt-prod-environment-id':'123'});
 await userEvent.clear(screen.getByLabelText('dbt production environment ID'));
 expect(latest!.metadataHeaders).toBeUndefined();
 expect(screen.queryByLabelText('Header name')).toBeNull();
});
it('offers bounded LangSmith region choices without arbitrary upstream URLs',async()=>{
 let latest:MCPConnectionInput;
 function Host(){const [input,set]=useState<MCPConnectionInput>({id:'test',name:'Test',service:'generic',credentialType:'langsmith',endpoint:'https://fixture.example/mcp',enabled:true});latest=input;return <MCPMetadataFields input={input} onChange={set}/>;}
 render(<Host/>);
 await userEvent.click(screen.getByText('LangSmith workspace settings'));
 await userEvent.selectOptions(screen.getByLabelText('LangSmith API region'),'https://eu.api.smith.langchain.com');
 expect(latest!.metadataHeaders).toEqual({'langsmith-endpoint':'https://eu.api.smith.langchain.com'});
 expect(screen.getAllByRole('option')).toHaveLength(3);
});
