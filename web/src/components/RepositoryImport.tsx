import {useEffect,useState} from 'react';
import {api,errorMessage} from '../api/client';
import type {Project,RepositoryImportRecord} from '../api/tasks';
import {Btn,ErrorBox} from './common';
const active=(state:string)=>['fetching','verifying','publishing','cancel_requested'].includes(state);
export function RepositoryImport({canEdit,canSelect=canEdit,initialURL='',nativeProfile=false,onProject}:{nativeProfile?:boolean;canEdit:boolean;canSelect?:boolean;initialURL?:string;onProject:(project:Project)=>void}){
 const [authMode,setAuthMode]=useState('anonymous');const [username,setUsername]=useState('');const [token,setToken]=useState('');
 const [url,setURL]=useState(initialURL);const [ref,setRef]=useState('HEAD');
 const [confirmed,setConfirmed]=useState(false);const [records,setRecords]=useState<RepositoryImportRecord[]>([]);
 const [selected,setSelected]=useState<string>();const [error,setError]=useState<string>();const [busy,setBusy]=useState(false);
 const record=records.find(r=>r.id===selected);
 useEffect(()=>{
  let disposed=false;let restore=true;let timer:ReturnType<typeof setTimeout>;
  const load=async()=>{try{const values=await api.repositoryImports();if(!disposed){setRecords(values);if(restore){const found=values.find(r=>r.url===initialURL);setSelected(old=>old??found?.id);if(found)setAuthMode(found.authentication||'anonymous');restore=false;}}}catch(e){if(!disposed)setError(errorMessage(e));}finally{if(!disposed)timer=setTimeout(()=>void load(),1500);}};
  void load();return()=>{disposed=true;clearTimeout(timer);};
 },[initialURL]);
 const update=(r:RepositoryImportRecord)=>{setRecords(old=>[...old.filter(item=>item.id!==r.id),r]);setSelected(r.id);setURL(r.url);setRef(r.ref);setAuthMode(r.authentication||'anonymous');};
 const start=async(retry=false)=>{setBusy(true);setError(undefined);try{update(await api.importRepository({url,ref,confirmed,retry,revision:retry?record?.revision:undefined,authentication:authMode==='anonymous'?undefined:{mode:authMode,...(authMode==='token'?{username,token}:{})}}));setConfirmed(false);}catch(e){setError(errorMessage(e));}finally{setToken('');setBusy(false);}};
 const cancel=async()=>{if(!record)return;setBusy(true);setError(undefined);try{update(await api.cancelRepositoryImport(record.id,record.revision));}catch(e){setError(errorMessage(e));}finally{setBusy(false);}};
 const useProject=async()=>{if(!record?.projectId)return;setBusy(true);setError(undefined);try{const projects=await api.projects();const project=projects.find(p=>p.id===record.projectId);if(!project)throw new Error('The imported project is unavailable. Refresh and retry.');onProject(project);}catch(e){setError(errorMessage(e));}finally{setBusy(false);}};
 return <section className="card form" aria-label="Import repository">
  <h2>Import repository</h2>
  <p>Download an HTTPS Git snapshot into Weft-managed storage. Existing checkouts stay untouched. Import does not start agents or change their permissions.</p>
  {!canEdit&&<p>An administrator must import repositories. Existing project selection and agent permissions are unchanged.</p>}
  <fieldset disabled={!canEdit||busy}>
   <label>Clone URL<input type="url" maxLength={2048} placeholder="https://github.com/owner/repository.git" value={url} onChange={e=>{setURL(e.target.value);setToken('');setSelected(undefined);setConfirmed(false);}}/></label>
   <label>Git authentication<select value={authMode} onChange={e=>{setAuthMode(e.target.value);setToken('');setConfirmed(false);}}><option value="anonymous">Public repository (no credentials)</option><option value="token">HTTPS username and token (this attempt only)</option>{nativeProfile&&<><option value="local_git">Use local Git credential helper</option><option value="local_gh">Use local GitHub CLI authentication</option></>}</select></label>
   {authMode==='token'&&<><label>Git username<input autoComplete="off" maxLength={256} value={username} onChange={e=>{setUsername(e.target.value);setConfirmed(false);}}/></label><label>Git access token<input type="password" autoComplete="new-password" maxLength={8192} value={token} onChange={e=>{setToken(e.target.value);setConfirmed(false);}}/></label><p>Use credentials with repository read access. The token is sent only to this clone endpoint, held in memory for this attempt and cleared from this form after submission. Re-enter it for retries. MCP OAuth does not authorize Git.</p></>}
   {authMode.startsWith('local_')&&<p>Explicitly authorize the selected credential helper or GitHub CLI on the native Weft host to read its existing credentials for this repository. Helpers may execute local commands. Weft does not save, erase or modify these credentials. No interactive login is started.</p>}
   {!nativeProfile&&<p>Server imports use the explicitly supplied token. Host Git configuration, GitHub CLI and MCP credentials are not copied into the container.</p>}
   <label>Base reference<input maxLength={256} value={ref} onChange={e=>{setRef(e.target.value);setSelected(undefined);setConfirmed(false);}}/></label>
   <p>Use HEAD, refs/heads/main, refs/tags/v1.0 or a full commit hash. The imported commit is pinned; reusing an import never silently fetches a newer branch tip.</p>
   <label><input type="checkbox" checked={confirmed} onChange={e=>setConfirmed(e.target.checked)}/>Authorize the selected Git authentication and download on the Weft server (up to 128 MiB transfer and three minutes).</label>
   <Btn disabled={!confirmed||!url||!ref||(authMode==='token'&&(!username||!token))||busy||!!record&&active(record.state)} onClick={()=>void start(false)}>Import repository</Btn>
  </fieldset>
  <ErrorBox error={error}/>
  {records.length>0&&<label>Saved imports<select value={selected??''} onChange={e=>{const r=records.find(item=>item.id===e.target.value);if(r)update(r);else setSelected(undefined);setToken('');setConfirmed(false);}}><option value="">Select import</option>{records.map(r=><option key={r.id} value={r.id}>{r.url} · {r.ref} · {r.state}</option>)}</select></label>}
  {record&&<article aria-label="Repository import status"><p role="status">{record.state}: {record.message}</p><p>Transferred: {(record.bytes/1048576).toFixed(2)} MiB</p><p>Reference: {record.ref}{record.commit&&<> · Commit: <code>{record.commit}</code></>}</p>
   {['fetching','verifying'].includes(record.state)&&<Btn disabled={!canEdit||busy} onClick={()=>void cancel()}>Cancel import</Btn>}
   {['failed','cancelled','interrupted'].includes(record.state)&&<><p>Retry retains the previous attempt and starts in a new managed directory.</p><Btn disabled={!canEdit||busy||!confirmed||(authMode==='token'&&(!username||!token))} onClick={()=>void start(true)}>Retry import</Btn></>}
   {record.state==='ready'&&<Btn disabled={busy||!canSelect} onClick={()=>void useProject()}>Use imported project</Btn>}
  </article>}
 </section>;
}
