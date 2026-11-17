import { useAsync } from '../hooks/useData';
import { useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { MCPConnection } from '../api/types';
import { Btn, ErrorBox } from './common';

export function MCPOAuthReturn({query,canManage,onChanged}:{query:string;canManage:boolean;onChanged:()=>Promise<void>}) {
 const params=new URLSearchParams(query);
 const state=params.get('state')??'';
 const valid=/^[A-Za-z0-9_-]{43}$/.test(state)&&params.size===1;
 const [busy,setBusy]=useState(false);const [error,setError]=useState<string>();const [result,setResult]=useState<string>();
 async function complete(){setBusy(true);setError(undefined);try{
  const connection=await api.completeMCPOAuth(state);
  setResult(connection.status==='ready'?'OAuth connected. Select tools for agents explicitly.':'OAuth authorized, but tool discovery failed. Test the connection to retry discovery.');
  window.history.replaceState(null,'',`${location.pathname}${location.search}#/integrations`);
  await onChanged();
 }catch(e){setError(errorMessage(e));}finally{setBusy(false);}}
 return <section aria-label="OAuth return"><h3>Return from OAuth provider</h3>
  {!valid?<ErrorBox error="Invalid OAuth return. Start authorization again from the connection."/>:<>
   <p>Complete the connection using the same Weft login that started authorization.</p>
   <ErrorBox error={error}/>{error&&<p>Check the registered callback, MCP scopes and corporate access policy. Restart authorization from the connection after correcting configuration; do not reuse an expired or denied return.</p>}{result?<p role="status">{result}</p>:canManage?<Btn disabled={busy} onClick={()=>void complete()}>Complete OAuth connection</Btn>:<p>Sign in as the administrator who started this connection.</p>}
  </>}
 </section>;
}
const registrationMessages:Record<string,string>={
 http_rejected:'Registration rejected or unavailable.',
 provider_error:'Provider reported a registration error.',
 transport_outcome_unknown:'The registration request did not complete; its remote outcome is unknown.',
 response_unreadable:'The registration response could not be read within the limits.',
 response_incompatible:'The registration response did not match the requested client contract.',
 scope_expansion:'The registration response expanded the requested scopes.',
 credential_storage_failed:'The returned client credential could not be stored securely.',
 request_setup_failed:'The registration request could not be prepared.',
 local_setup_failed:'Local client setup failed.',
};
export function MCPOAuthActions({connection,canManage,onChanged}:{connection:MCPConnection;canManage:boolean;onChanged:()=>Promise<void>}) {
 const [busy,setBusy]=useState(false);const [error,setError]=useState<string>();const [confirm,setConfirm]=useState(false);
 const [plan,setPlan]=useState<{mode:string;reason:string}>();
 const oauth=connection.oauth!;
 const automatic=!!oauth.registrationMode&&oauth.registrationMode!=="manual";
 const status=error||oauth.registrationStatus==='uncertain'?'Error — last OAuth action failed':oauth.status==='revoked'?'Revoked':oauth.status==='reauthorization_required'||connection.status==='unavailable'?'Error — authorization or discovery needs attention':oauth.status==='authorized'?'Authorized':oauth.status==='authorization_required'?'Authorization required':'Authorization in progress';
 async function start(){setBusy(true);setError(undefined);try{const result=await api.startMCPOAuth(connection.id,plan?.mode);window.location.assign(result.url);}catch(e){setError(errorMessage(e));setPlan(undefined);setBusy(false);try{await onChanged();}catch(refreshError){setError(`${errorMessage(e)} Connection reload failed: ${errorMessage(refreshError)}`);}}}
 async function preview(){setBusy(true);setError(undefined);setPlan(undefined);try{setPlan(await api.previewMCPOAuth(connection.id));}catch(e){setError(errorMessage(e));}finally{setBusy(false);}}
 async function revoke(){setBusy(true);setError(undefined);try{await api.revokeMCPOAuth(connection.id);setConfirm(false);await onChanged();}catch(e){setError(errorMessage(e));}finally{setBusy(false);}}
 return <section aria-label={`OAuth ${connection.name}`}>
  <p>{status}</p><p>OAuth: {oauth.status} · Issuer: {oauth.issuer}</p><p>Requested scopes: {oauth.scopes.join(', ')||'Provider defaults'} · Granted: {oauth.grantedScopes?.join(', ')||'Not authorized'}</p>
  {oauth.registrationStatus&&<p>Client setup: {oauth.effectiveRegistrationMode} · {oauth.registrationStatus}. {oauth.registrationStatus==='uncertain'&&'Inspect the provider before changing registration settings; no automatic retry was made.'}</p>}
  {(oauth.registrationStatus==='uncertain'||oauth.registrationStatus==='pending')&&<p>
   {oauth.registrationDiagnostic?<>{registrationMessages[oauth.registrationDiagnostic.code]??'Registration failed; inspect the provider before retrying.'} Diagnostic: {oauth.registrationDiagnostic.code}. {oauth.registrationDiagnostic.httpStatus?`Provider HTTP ${oauth.registrationDiagnostic.httpStatus}.`:'No provider HTTP status recorded.'}</>:'The original failure details were not recorded when this attempt was made. The outcome cannot be reconstructed from this status.'}
   {' '}Do not create another connection or change registration settings to retry until the provider confirms whether a client was created.
   {oauth.registrationDiagnostic?.httpStatus===403&&' The provider refused registration. Review its client registration policy with the administrator; changing localhost to HTTPS alone is not a confirmed fix.'}
  </p>}
  {oauth.expiresAt&&<p>Token expires: {oauth.expiresAt}. Renewal is attempted before use when supported.</p>}
  {oauth.revocation&&<p>Local access revoked. Provider revocation: {oauth.revocation}. {oauth.revocation!=='requested'&&'Review and revoke access in the provider account settings.'}</p>}
  <ErrorBox error={error}/>{(error||connection.error)&&<p>Check the client registration and exact callback. A 403 may indicate insufficient MCP scopes, repository/site permissions, SSO, or a corporate IP/domain policy. Correct the specific restriction before reauthorizing.</p>}
  {canManage&&automatic&&<div><Btn disabled={busy||!connection.enabled} onClick={()=>void preview()}>Review OAuth setup {connection.name}</Btn>{plan&&<div role="status"><p>Selected mechanism: {plan.mode}. {plan.reason}</p><Btn disabled={busy} onClick={()=>void start()}>Confirm {plan.mode} and authorize {connection.name}</Btn><Btn disabled={busy} onClick={()=>setPlan(undefined)}>Cancel OAuth setup</Btn></div>}</div>}
  {canManage&&<div className="btn-row">{!automatic&&<Btn disabled={busy||!connection.enabled} onClick={()=>void start()}>{oauth.status==='authorized'||oauth.status==='reauthorization_required'||oauth.status==='revoked'?'Reauthorize':'Authorize'} {connection.name}</Btn>}
   {confirm?<><p>Revoke this connection for all agents? Already dispatched calls cannot be undone.</p><Btn disabled={busy} onClick={()=>void revoke()}>Confirm revoke {connection.name}</Btn><Btn disabled={busy} onClick={()=>setConfirm(false)}>Cancel revocation</Btn></>:<Btn disabled={busy||oauth.status==='revoked'} onClick={()=>setConfirm(true)}>Revoke OAuth {connection.name}</Btn>}
  </div>}
 </section>;
}

export function MCPOAuthCallbackURL() {
 const config=useAsync(api.mcpOAuthConfig,[]);
 return <div><p>Register a client with your provider using this callback URL:</p>
  {config.data&&<code>{config.data.callbackUrl}</code>}
  {config.loading&&<p role="status">Loading callback URL…</p>}
  <ErrorBox error={config.error}/>
  {config.error&&<Btn onClick={()=>void config.reload()}>Retry callback configuration</Btn>}
 </div>;
}
