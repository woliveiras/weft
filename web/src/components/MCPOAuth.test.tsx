import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import { fakeApi, apiError } from '../test/fakeApi';
import { MCPOAuthActions, MCPOAuthReturn, MCPOAuthCallbackURL } from './MCPOAuth';
import type { MCPConnection } from '../api/types';
afterEach(cleanup);
const connection:MCPConnection={id:'oauth',name:'OAuth example',service:'generic',endpoint:'https://example.com/mcp',enabled:true,status:'ready',revision:1,credentialConfigured:true,tools:[],toolsRevision:1,updatedAt:'2026-10-17',updatedBy:'admin',oauth:{issuer:'https://issuer.example',clientId:'client',scopes:['read'],grantedScopes:['read'],status:'authorized',clientSecretConfigured:false}};
it('completes only on explicit authenticated action and reports discovery failures',async()=>{
 const state='a'.repeat(43);const backend=fakeApi({'POST /api/v1/mcp/oauth/complete':()=>({...connection,status:'unavailable'})});const changed=vi.fn(async()=>{});
 render(<MCPOAuthReturn query={'state='+state} canManage onChanged={changed}/>);
 expect(backend.find('POST','/api/v1/mcp/oauth/complete')).toHaveLength(0);
 await userEvent.click(screen.getByRole('button',{name:'Complete OAuth connection'}));
 expect(await screen.findByText(/tool discovery failed/)).toBeTruthy();expect(changed).toHaveBeenCalledOnce();
 expect(backend.find('POST','/api/v1/mcp/oauth/complete')[0].body).toEqual({state});
});
it('reports invalid returns and denied authorization without claiming success',async()=>{
 fakeApi({'POST /api/v1/mcp/oauth/complete':()=>apiError(422,'Authorization denied','invalid')});
 const view=render(<MCPOAuthReturn query="error=invalid_return" canManage onChanged={async()=>{}}/>);
 expect(screen.getByText(/Invalid OAuth return/)).toBeTruthy();expect(screen.queryByRole('button')).toBeNull();
 view.rerender(<MCPOAuthReturn query={'state='+'b'.repeat(43)} canManage onChanged={async()=>{}}/>);
 await userEvent.click(screen.getByRole('button',{name:'Complete OAuth connection'}));expect(await screen.findByText('Authorization denied')).toBeTruthy();expect(screen.queryByText(/OAuth connected/)).toBeNull();
});
it('requires confirmation to revoke and preserves viewer boundaries',async()=>{
 const backend=fakeApi({'POST /api/v1/mcp/connections/oauth/oauth/revoke':()=>({...connection,oauth:{...connection.oauth,status:'revoked',revocation:'failed'}})});
 const changed=vi.fn(async()=>{});const view=render(<MCPOAuthActions connection={connection} canManage onChanged={changed}/>);
 await userEvent.click(screen.getByRole('button',{name:'Revoke OAuth OAuth example'}));expect(backend.find('POST','/api/v1/mcp/connections/oauth/oauth/revoke')).toHaveLength(0);
 await userEvent.click(screen.getByRole('button',{name:'Confirm revoke OAuth example'}));expect(changed).toHaveBeenCalledOnce();
 view.rerender(<MCPOAuthActions connection={{...connection,oauth:{...connection.oauth!,status:'revoked',revocation:'failed'}}} canManage={false} onChanged={changed}/>);
 expect(screen.getByText(/Provider revocation: failed/)).toBeTruthy();expect(screen.queryByRole('button')).toBeNull();
});

it('shows the configured callback and reports configuration errors',async()=>{
 fakeApi({'GET /api/v1/mcp/oauth/config':()=>({callbackUrl:'https://weft.example/api/v1/mcp/oauth/callback'})});
 const view=render(<MCPOAuthCallbackURL/>);expect(await screen.findByText('https://weft.example/api/v1/mcp/oauth/callback')).toBeTruthy();view.unmount();
 fakeApi({'GET /api/v1/mcp/oauth/config':()=>apiError(400,'Configure public URL','invalid')});
 render(<MCPOAuthCallbackURL/>);expect(await screen.findByText('Configure public URL')).toBeTruthy();
});
it('distinguishes discovery errors from authorized tokens and labels reauthorization',()=>{
 const view=render(<MCPOAuthActions connection={{...connection,status:'unavailable',error:'Discovery failed'}} canManage onChanged={async()=>{}}/>);
 expect(screen.getByText('Error — authorization or discovery needs attention')).toBeTruthy();
 expect(screen.getByRole('button',{name:'Reauthorize OAuth example'})).toBeTruthy();
 expect(screen.getByText(/corporate IP\/domain policy/)).toBeTruthy();
 view.rerender(<MCPOAuthActions connection={{...connection,oauth:{...connection.oauth!,status:'revoked'}}} canManage onChanged={async()=>{}}/>);
 expect(screen.getByText('Revoked')).toBeTruthy();
 expect(screen.getByRole('button',{name:'Revoke OAuth OAuth example'}).hasAttribute('disabled')).toBe(true);
});
it('reviews automatic setup without registering and permits cancellation',async()=>{
 const backend=fakeApi({'GET /api/v1/mcp/connections/oauth/oauth/preview':()=>({mode:'dcr',reason:'CIMD unavailable on localhost'})});
 render(<MCPOAuthActions connection={{...connection,oauth:{...connection.oauth!,registrationMode:'auto',clientId:''}}} canManage onChanged={async()=>{}}/>);
 expect(screen.queryByRole('button',{name:'Authorize OAuth example'})).toBeNull();
 await userEvent.click(screen.getByRole('button',{name:'Review OAuth setup OAuth example'}));
 expect(await screen.findByText(/Selected mechanism: dcr/)).toBeTruthy();
 expect(backend.calls.every(c=>c.method==='GET')).toBe(true);
 await userEvent.click(screen.getByRole('button',{name:'Cancel OAuth setup'}));
 expect(screen.queryByText(/Selected mechanism/)).toBeNull();
});
it('reports automatic setup failure with manual registration guidance',async()=>{
 fakeApi({'GET /api/v1/mcp/connections/oauth/oauth/preview':()=>apiError(422,'Automatic client setup unavailable; configure a pre-registered client manually','invalid')});
 render(<MCPOAuthActions connection={{...connection,oauth:{...connection.oauth!,registrationMode:'auto',clientId:''}}} canManage onChanged={async()=>{}}/>);
 await userEvent.click(screen.getByRole('button',{name:'Review OAuth setup OAuth example'}));
 expect(await screen.findByText(/configure a pre-registered client manually/)).toBeTruthy();expect(screen.queryByRole('button',{name:/Confirm .* and authorize/})).toBeNull();
});

it('shows safe registration diagnostics and distinguishes missing historical evidence',()=>{
 const oauth={...connection.oauth!,registrationMode:'dcr' as const,registrationStatus:'uncertain',registrationDiagnostic:{code:'http_rejected',httpStatus:403}};
 const view=render(<MCPOAuthActions connection={{...connection,oauth}} canManage onChanged={async()=>{}}/>);
 expect(screen.getByText(/Provider HTTP 403/)).toBeTruthy();
 expect(screen.getByText(/provider refused registration/)).toBeTruthy();
 expect(screen.queryByRole('button',{name:/Confirm dcr/})).toBeNull();
 view.rerender(<MCPOAuthActions connection={{...connection,oauth:{...oauth,registrationDiagnostic:undefined}}} canManage onChanged={async()=>{}}/>);
 expect(screen.getByText(/original failure details were not recorded/)).toBeTruthy();
 expect(screen.getByText(/Do not create another connection/)).toBeTruthy();
});
it('reloads persisted failure and clears the stale registration confirmation after a failed start',async()=>{
 const backend=fakeApi({
  'GET /api/v1/mcp/connections/oauth/oauth/preview':()=>({mode:'dcr',reason:'Review registration'}),
  'POST /api/v1/mcp/connections/oauth/oauth/start':()=>apiError(502,'Registration rejected','oauth'),
 });
 const changed=vi.fn(async()=>{});
 render(<MCPOAuthActions connection={{...connection,oauth:{...connection.oauth!,registrationMode:'dcr'}}} canManage onChanged={changed}/>);
 await userEvent.click(screen.getByRole('button',{name:'Review OAuth setup OAuth example'}));
 await userEvent.click(await screen.findByRole('button',{name:'Confirm dcr and authorize OAuth example'}));
 expect(await screen.findByText('Registration rejected')).toBeTruthy();
 expect(changed).toHaveBeenCalledOnce();
 expect(screen.queryByRole('button',{name:'Confirm dcr and authorize OAuth example'})).toBeNull();
 expect(backend.calls.filter(c=>c.method==='POST')).toHaveLength(1);
});

it('reports a failed connection reload after a registration failure',async()=>{
 fakeApi({'POST /api/v1/mcp/connections/oauth/oauth/start':()=>apiError(502,'Registration rejected','oauth')});
 render(<MCPOAuthActions connection={connection} canManage onChanged={async()=>{throw new Error('Unavailable');}}/>);
 await userEvent.click(screen.getByRole('button',{name:'Reauthorize OAuth example'}));
 expect(await screen.findByText(/Registration rejected Connection reload failed: Unavailable/)).toBeTruthy();
});
