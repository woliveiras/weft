import { cleanup,render,screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach,it,expect,vi } from 'vitest';
import { MCPTemplateSettings } from './MCPTemplateSettings';
import { fakeApi,apiError } from '../test/fakeApi';
import type { MCPTemplate } from '../api/types';
afterEach(cleanup);
const latest:MCPTemplate={id:'github',version:2,name:'GitHub read-only',service:'github',endpoint:'https://api.githubcopilot.com/mcp/readonly',authentication:'bearer or OAuth',guidance:'Review access',source:'https://example.com',reviewedAt:'2026-10-18',validation:'Not live qualified',authenticationModes:['bearer','oauth'],parameters:[{name:'toolset',label:'Read-only toolset',default:'default',options:['default','repos']}]};
const input={id:'personal',name:'Personal',service:'github',enabled:false,endpoint:latest.endpoint,template:{id:'github',version:1}};
it('previews and cancels upgrades without changing the draft or saving; application is explicit',async()=>{
 fakeApi({'GET /api/v1/mcp/templates':()=>[{...latest,version:1,parameters:[]},latest]});const change=vi.fn();
 render(<MCPTemplateSettings input={input} onChange={change}/>);
 await userEvent.click(await screen.findByRole('button',{name:'Review template update to v2'}));
 expect(screen.getByRole('dialog',{name:'Review template update'})).toBeTruthy();expect(change).not.toHaveBeenCalled();
 await userEvent.click(screen.getByRole('button',{name:'Cancel template update'}));expect(change).not.toHaveBeenCalled();
 await userEvent.click(screen.getByRole('button',{name:'Review template update to v2'}));
 await userEvent.click(screen.getByRole('button',{name:'Apply update to draft'}));
 expect(change).toHaveBeenCalledWith(expect.objectContaining({template:{id:'github',version:2},enabled:false,endpoint:latest.endpoint}));
});
it('shows catalogue failures and retains the existing draft',async()=>{
 fakeApi({'GET /api/v1/mcp/templates':()=>apiError(503,'Catalogue unavailable','unavailable')});const change=vi.fn();
 render(<MCPTemplateSettings input={input} onChange={change}/>);
 expect(await screen.findByText('Catalogue unavailable')).toBeTruthy();expect(change).not.toHaveBeenCalled();
 expect(screen.getByRole('button',{name:'Reload template settings'})).toBeTruthy();
});
