import { useRef, useState } from 'react';
import { api, errorMessage } from '../api/client';
import { delegationLimits, validDelegationPolicy, validDelegationProfile, type DelegationPolicy, type DelegationProfile, type DelegationProfileView } from '../api/delegation';
import { useAsync } from '../hooks/useData';
import { Btn, ErrorBox } from './common';

export function DelegationSection({canManage}: {canManage:boolean}) {
 return <section className="integration-section" aria-label="Delegation settings"><h2>Delegation</h2>
  <p>Native children use the parent model and only profile-selected parent capabilities. Empty profiles have no tools or workspace; further delegation requires an inherited grant and available depth. Agents still need an explicit delegation.request grant.</p>
  {!canManage && <p>Administrator access is required to change delegation settings.</p>}
  <PolicyControls key={'policy-'+canManage} canManage={canManage}/>
  <ProfileControls key={'profiles-'+canManage} canManage={canManage}/>
 </section>;
}
function PolicyControls({canManage}: {canManage:boolean}) {
 const current=useAsync(api.delegationPolicy,[]);
 const [draft,setDraft]=useState<DelegationPolicy>();
 const [review,setReview]=useState(false),[busy,setBusy]=useState(false);
 const lock=useRef(false);
 const [error,setError]=useState<string>(),[notice,setNotice]=useState<string>();
 const policy=draft??current.data?.policy;
 const disabled=!canManage||busy||current.loading||!!current.error||review;
 async function save(){
  if(!canManage||!policy||!current.data||!review||lock.current)return;
  lock.current=true;setBusy(true);setError(undefined);setNotice(undefined);
  try{await api.saveDelegationPolicy(policy,current.data.revision);setNotice('Delegation policy saved. New requests use these settings.');}
  catch(e){setError(`Save could not be confirmed: ${errorMessage(e)}. Reload and review before retrying.`);}
  finally{setDraft(undefined);setReview(false);await current.reload();lock.current=false;setBusy(false);}
 }
 return <section className="integration-card" aria-label="Delegation policy"><h3>Approval and limits</h3>
  <Btn disabled={busy||review||current.loading} onClick={()=>{setDraft(undefined);setError(undefined);setNotice(undefined);void current.reload();}}>Reload delegation policy</Btn>
  <ErrorBox error={current.error??error}/>{notice&&<p role="status">{notice}</p>}
  {current.loading&&<p>Loading policy…</p>}
  {policy&&current.data&&!current.error&&<>
   <p>Revision {current.data.revision} · Maximum depth {policy.maxDepth}</p>
   <label><input type="checkbox" checked={policy.enabled} disabled={disabled} onChange={e=>setDraft({...policy,enabled:e.target.checked})}/>Enable delegation</label>
   <label>Approval mode<select value={policy.approvalMode} disabled={disabled} onChange={e=>setDraft({...policy,approvalMode:e.target.value as DelegationPolicy['approvalMode']})}><option value="required">Approval required</option><option value="automatic">Automatic within limits</option></select></label>
   {delegationLimits.map(([key,label,max])=><label key={key}>{label}<input type="number" min={1} max={max} step={1} disabled={disabled} value={policy[key]} onChange={e=>setDraft({...policy,[key]:Number(e.target.value)})}/></label>)}
   <p>Tree budgets reserve estimated input plus output before each call. Reported usage is counted once; unknown usage after dispatch retains an estimated charge. Parent token limits also apply. This is not a guaranteed billing cap. Running children retain their snapshot; changed settings invalidate waiting requests before inference.</p>
   {canManage&&<Btn disabled={disabled||!validDelegationPolicy(policy)} onClick={()=>{setNotice(undefined);setReview(true);}}>Review delegation policy</Btn>}
   {review&&<section aria-label="Confirm delegation policy"><h4>Review policy changes</h4>
    <p>{policy.enabled ? policy.approvalMode==='automatic'?'New child requests can run without individual delegation approval. Explicit tool grants and any tool-level approvals still apply.':'Every child request requires approval.':'New child requests will be blocked.'}</p>
    <dl>{delegationLimits.map(([key,label])=><div key={key}><dt>{label}</dt><dd>{policy[key]}</dd></div>)}</dl>
    <Btn disabled={busy} onClick={()=>void save()}>Confirm delegation policy</Btn><Btn disabled={busy} onClick={()=>setReview(false)}>Cancel policy changes</Btn>
   </section>}
  </>}
 </section>;
}
const emptyProfile:DelegationProfile={id:'',name:'',instructions:'',enabled:true,maxOutputTokens:1024};
function ProfileControls({canManage}: {canManage:boolean}) {
 const current=useAsync(api.delegationProfiles,[]);
 const [edit,setEdit]=useState<{profile:DelegationProfile;revision?:number}>();
 const [review,setReview]=useState(false),[busy,setBusy]=useState(false);
 const lock=useRef(false);
 const [error,setError]=useState<string>(),[notice,setNotice]=useState<string>();
 const disabled=!canManage||busy||current.loading||!!current.error||review;
 function open(v?:DelegationProfileView){setEdit(v?{profile:{...v.profile},revision:v.revision}:{profile:{...emptyProfile}});setReview(false);setError(undefined);setNotice(undefined);}
 async function save(){
  if(!canManage||!edit||!review||lock.current)return;
  lock.current=true;setBusy(true);setError(undefined);
  try{await api.saveDelegationProfile(edit.profile,edit.revision);setNotice('Delegation profile saved. No agent permissions were changed.');}
  catch(e){setError(`Save could not be confirmed: ${errorMessage(e)}. Reload and review before retrying.`);}
  finally{setEdit(undefined);setReview(false);await current.reload();lock.current=false;setBusy(false);}
 }
 return <section className="integration-card" aria-label="Delegation profiles"><h3>Analysis profiles</h3>
  <p>Profiles customize guidance, output allowances and a subset of explicit parent capabilities. They cannot choose another model or grant new access. Each child retains the parent constraints and tool approvals. Disable a profile to block new requests without deleting execution history.</p>
  <Btn disabled={busy||review||current.loading} onClick={()=>{setEdit(undefined);setError(undefined);void current.reload();}}>Reload delegation profiles</Btn>
  <ErrorBox error={current.error??error}/>{notice&&<p role="status">{notice}</p>}
  {!current.error&&current.data?.map(v=><article key={v.profile.id}><h4>{v.profile.name}</h4><p>{v.profile.id} · Revision {v.revision} · {v.profile.enabled?'Enabled':'Disabled'}</p>{canManage&&<Btn disabled={disabled} onClick={()=>open(v)}>Edit {v.profile.name}</Btn>}</article>)}
  {canManage&&<Btn disabled={disabled} onClick={()=>open()}>New delegation profile</Btn>}
  {edit&&<section aria-label="Profile editor">
   <label>Profile ID<input value={edit.profile.id} disabled={disabled||edit.revision!==undefined} onChange={e=>setEdit({...edit,profile:{...edit.profile,id:e.target.value}})}/></label>
   <label>Profile name<input value={edit.profile.name} disabled={disabled} onChange={e=>setEdit({...edit,profile:{...edit.profile,name:e.target.value}})}/></label>
   <label>Profile instructions<textarea value={edit.profile.instructions} disabled={disabled} onChange={e=>setEdit({...edit,profile:{...edit.profile,instructions:e.target.value}})}/></label>
   <label>Inherited capabilities (one per line)<textarea value={(edit.profile.tools??[]).join('\n')} disabled={disabled} onChange={e=>setEdit({...edit,profile:{...edit.profile,tools:e.target.value ? e.target.value.split('\n').map(name=>name.trim()) : []}})}/></label>
   <p>Optional; empty means no tools. Use exact parent grant names, including exact MCP tool names. Wildcards and environment/review control tools are unsupported. Further delegation requires delegation.request and an increased depth limit. Missing parent grants reject the request.</p>
   <label>Profile output token allowance<input type="number" min={1} max={4096} value={edit.profile.maxOutputTokens} disabled={disabled} onChange={e=>setEdit({...edit,profile:{...edit.profile,maxOutputTokens:Number(e.target.value)}})}/></label>
   <label><input type="checkbox" checked={edit.profile.enabled} disabled={disabled} onChange={e=>setEdit({...edit,profile:{...edit.profile,enabled:e.target.checked}})}/>Profile enabled</label>
   <Btn disabled={disabled||!validDelegationProfile(edit.profile)} onClick={()=>setReview(true)}>Review delegation profile</Btn>
   <Btn disabled={busy} onClick={()=>{setEdit(undefined);setReview(false);}}>Cancel profile changes</Btn>
   {review&&<section aria-label="Confirm delegation profile"><h4>Review profile changes</h4><p>{edit.profile.name} ({edit.profile.id}) · {edit.profile.enabled?'Enabled':'Disabled'} · Output allowance {edit.profile.maxOutputTokens}</p><p>{edit.profile.instructions}</p><p>Inherited capabilities: {(edit.profile.tools??[]).join(", ") || "None"}</p><Btn disabled={busy} onClick={()=>void save()}>Confirm delegation profile</Btn></section>}
  </section>}
 </section>;
}
