import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import { fakeApi, apiError } from '../test/fakeApi';
import { MCPOAuthActions, MCPOAuthReturn, MCPOAuthCallbackURL, registrationFieldLabels } from './MCPOAuth';
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

const uncertainConnection:MCPConnection={...connection,revision:3,status:'authorization_required',oauth:{...connection.oauth!,clientId:'',registrationMode:'dcr',effectiveRegistrationMode:'dcr',registrationStatus:'uncertain',status:'authorization_required',grantedScopes:undefined,registrationDiagnostic:{code:'response_incompatible',httpStatus:201,fields:['token_endpoint_auth_method','redirect_uris']}}};
it('describes only known registration response fields without exposing upstream values',()=>{
 expect(registrationFieldLabels(['client_secret','upstream-secret','scope','client_secret','response'])).toEqual(['Client credential','Scopes','JSON response']);
 const view=render(<MCPOAuthActions connection={uncertainConnection} canManage onChanged={async()=>{}}/>);
 expect(screen.getByText(/Incompatible fields: Token authentication method, Callback URLs/)).toBeTruthy();
 expect(screen.getByRole('button',{name:'Review OAuth setup OAuth example'}).hasAttribute('disabled')).toBe(true);
 view.unmount();
});
it('requires acknowledgement, permits cancellation and preserves admin boundaries for recovery',async()=>{
 const backend=fakeApi({});const changed=vi.fn(async()=>{});
 const view=render(<MCPOAuthActions connection={uncertainConnection} canManage onChanged={changed}/>);
 await userEvent.click(screen.getByRole('button',{name:'Review registration recovery'}));
 expect(screen.getByText(/provider may retain a duplicate or orphaned client/)).toBeTruthy();
 expect(screen.getByRole('button',{name:'Prepare new registration attempt'}).hasAttribute('disabled')).toBe(true);
 await userEvent.click(screen.getByRole('button',{name:'Cancel registration recovery'}));
 expect(screen.queryByRole('checkbox')).toBeNull();expect(backend.calls).toHaveLength(0);
 view.rerender(<MCPOAuthActions connection={uncertainConnection} canManage={false} onChanged={changed}/>);
 expect(screen.queryByRole('button')).toBeNull();
 for(const registrationStatus of ['pending','ready']){
  view.rerender(<MCPOAuthActions connection={{...uncertainConnection,oauth:{...uncertainConnection.oauth!,registrationStatus}}} canManage onChanged={changed}/>);
  expect(screen.queryByRole('button',{name:'Review registration recovery'})).toBeNull();
 }
});
it('prepares recovery through the real API client once and never starts provider registration automatically',async()=>{
 let finish!:(value:MCPConnection)=>void;
 const recovered:MCPConnection={...uncertainConnection,revision:4,oauth:{...uncertainConnection.oauth!,registrationStatus:undefined,registrationDiagnostic:undefined,effectiveRegistrationMode:undefined,registrationHistory:[{revision:3,recoveredAt:'2026-11-23T10:00:00Z',diagnostic:uncertainConnection.oauth!.registrationDiagnostic}]}};
 const backend=fakeApi({'POST /api/v1/mcp/connections/oauth/oauth/registration/recover':()=>new Promise<MCPConnection>(resolve=>{finish=resolve;})});
 const changed=vi.fn(async()=>{});const view=render(<MCPOAuthActions connection={uncertainConnection} canManage onChanged={changed}/>);
 await userEvent.click(screen.getByRole('button',{name:'Review registration recovery'}));
 await userEvent.click(screen.getByRole('checkbox',{name:'I accept the duplicate or orphaned client risk.'}));
 await userEvent.click(screen.getByRole('button',{name:'Prepare new registration attempt'}));
 expect(screen.getByRole('button',{name:'Preparing registration recovery…'}).hasAttribute('disabled')).toBe(true);
 expect(backend.calls).toHaveLength(1);expect(backend.calls[0].body).toEqual({expectRevision:3,acknowledgeDuplicateClientRisk:true});
 finish(recovered);
 expect(await screen.findByText(/Previous attempt preserved/)).toBeTruthy();expect(changed).toHaveBeenCalledOnce();
 expect(backend.calls).toHaveLength(1);
 view.rerender(<MCPOAuthActions key={recovered.revision} connection={recovered} canManage onChanged={changed}/>);
 expect(screen.getByText(/Previous attempt preserved/)).toBeTruthy();
 expect(screen.getByRole('button',{name:'Review OAuth setup OAuth example'}).hasAttribute('disabled')).toBe(false);
 await userEvent.click(screen.getByText('Previous registration attempts (1)'));
 expect(screen.getByText(/Revision 3/)).toBeTruthy();expect(screen.getByText(/Previous remote outcome remains uncertain/)).toBeTruthy();
 expect(screen.getByText(/Previous incompatible fields: Token authentication method, Callback URLs/)).toBeTruthy();
 view.rerender(<MCPOAuthActions connection={{...uncertainConnection,revision:6}} canManage onChanged={changed}/>);
 expect(screen.getByRole('button',{name:'Review registration recovery'})).toBeTruthy();
 expect(screen.queryByText(/Previous attempt preserved/)).toBeNull();
});
it('blocks a stale recovery review and reports API conflicts without retrying',async()=>{
 const backend=fakeApi({'POST /api/v1/mcp/connections/oauth/oauth/registration/recover':()=>apiError(409,'Connection changed; reload and review recovery again','conflict')});
 const changed=vi.fn(async()=>{});const view=render(<MCPOAuthActions connection={uncertainConnection} canManage onChanged={changed}/>);
 await userEvent.click(screen.getByRole('button',{name:'Review registration recovery'}));
 await userEvent.click(screen.getByRole('checkbox',{name:'I accept the duplicate or orphaned client risk.'}));
 view.rerender(<MCPOAuthActions connection={{...uncertainConnection,revision:4}} canManage onChanged={changed}/>);
 expect(screen.getByRole('button',{name:'Prepare new registration attempt'}).hasAttribute('disabled')).toBe(true);
 expect(screen.getByText(/Connection changed. Cancel and review recovery again/)).toBeTruthy();
 expect(backend.calls).toHaveLength(0);
 await userEvent.click(screen.getByRole('button',{name:'Cancel registration recovery'}));
 await userEvent.click(screen.getByRole('button',{name:'Review registration recovery'}));
 await userEvent.click(screen.getByRole('checkbox',{name:'I accept the duplicate or orphaned client risk.'}));
 await userEvent.click(screen.getByRole('button',{name:'Prepare new registration attempt'}));
 expect(await screen.findByText('Connection changed; reload and review recovery again')).toBeTruthy();
 expect(backend.calls).toHaveLength(1);expect(backend.calls[0].body).toEqual({expectRevision:4,acknowledgeDuplicateClientRisk:true});
 expect(screen.queryByText(/Previous attempt preserved/)).toBeNull();expect(changed).not.toHaveBeenCalled();
 expect((screen.getByRole('checkbox',{name:'I accept the duplicate or orphaned client risk.'}) as HTMLInputElement).checked).toBe(false);
});
it('reports recovery dependency or refresh failures without starting authorization',async()=>{
 const backend=fakeApi({'POST /api/v1/mcp/connections/oauth/oauth/registration/recover':()=>apiError(503,'Storage unavailable','unavailable')});
 const view=render(<MCPOAuthActions connection={uncertainConnection} canManage onChanged={async()=>{}}/>);
 await userEvent.click(screen.getByRole('button',{name:'Review registration recovery'}));
 await userEvent.click(screen.getByRole('checkbox',{name:'I accept the duplicate or orphaned client risk.'}));
 await userEvent.click(screen.getByRole('button',{name:'Prepare new registration attempt'}));
 expect(await screen.findByText('Storage unavailable')).toBeTruthy();expect(backend.calls).toHaveLength(1);
 view.unmount();
 fakeApi({'POST /api/v1/mcp/connections/oauth/oauth/registration/recover':()=>({...uncertainConnection,revision:4})});
 render(<MCPOAuthActions connection={uncertainConnection} canManage onChanged={async()=>{throw new Error('Reload unavailable');}}/>);
 await userEvent.click(screen.getByRole('button',{name:'Review registration recovery'}));
 await userEvent.click(screen.getByRole('checkbox',{name:'I accept the duplicate or orphaned client risk.'}));
 await userEvent.click(screen.getByRole('button',{name:'Prepare new registration attempt'}));
 expect(await screen.findByText(/Recovery was saved, but connection reload failed: Reload unavailable/)).toBeTruthy();
 expect(screen.queryByRole('button',{name:'Prepare new registration attempt'})).toBeNull();
});

it('does not apply an acknowledged recovery review to a different connection',async()=>{
 const backend=fakeApi({});const changed=vi.fn(async()=>{});
 const view=render(<MCPOAuthActions connection={uncertainConnection} canManage onChanged={changed}/>);
 await userEvent.click(screen.getByRole('button',{name:'Review registration recovery'}));
 await userEvent.click(screen.getByRole('checkbox',{name:'I accept the duplicate or orphaned client risk.'}));
 view.rerender(<MCPOAuthActions connection={{...uncertainConnection,id:'other'}} canManage onChanged={changed}/>);
 expect(screen.getByRole('button',{name:'Prepare new registration attempt'}).hasAttribute('disabled')).toBe(true);
 expect(backend.calls).toHaveLength(0);
});
