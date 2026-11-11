import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import { fakeApi, apiError } from '../test/fakeApi';
import { MCPTemplates } from './MCPTemplates';
afterEach(cleanup);
const template={id:'deepwiki',version:1,name:'DeepWiki',service:'generic',endpoint:'https://mcp.deepwiki.com/mcp',authentication:'none',guidance:'Review tools',source:'https://example.com/docs',reviewedAt:'2026-10-17',validation:'Test before use'};
it('reviews exact defaults and only selects on explicit action',async()=>{
 fakeApi({'GET /api/v1/mcp/templates':()=>[template]});const select=vi.fn();
 render(<MCPTemplates query="template=deepwiki&version=1" canManage onSelect={select}/>);
 expect(await screen.findByRole('article',{name:'Review MCP template'})).toBeTruthy();expect(select).not.toHaveBeenCalled();
 await userEvent.click(screen.getByRole('button',{name:'Use template defaults'}));expect(select).toHaveBeenCalledWith(template);
});
it('rejects unknown versions and prevents viewer selection',async()=>{
 fakeApi({'GET /api/v1/mcp/templates':()=>[template]});const select=vi.fn();
 const view=render(<MCPTemplates query="template=deepwiki&version=2" canManage onSelect={select}/>);
 expect(await screen.findByText(/Unknown MCP template version/)).toBeTruthy();
 view.rerender(<MCPTemplates query="template=deepwiki&version=1" canManage={false} onSelect={select}/>);
 expect(screen.getByText('An administrator must create this connection.')).toBeTruthy();expect(screen.queryByRole('button',{name:'Use template defaults'})).toBeNull();
});
it('reports catalogue failure',async()=>{
 fakeApi({'GET /api/v1/mcp/templates':()=>apiError(503,'Catalogue unavailable','unavailable')});
 render(<MCPTemplates canManage onSelect={()=>{}}/>);
 expect(await screen.findByText('Catalogue unavailable')).toBeTruthy();expect(screen.getByRole('button',{name:'Reload templates'})).toBeTruthy();
});
