import {useState} from 'react';
import {api,errorMessage} from '../api/client';
import type {CleanupPreview,TaskWorkspace} from '../api/tasks';
import {Btn,ErrorBox} from './common';

export function TaskCleanup({taskId,workspaces,onChanged}:{taskId:string;workspaces:TaskWorkspace[];onChanged:()=>void}) {
 const [scope,setScope]=useState('');const [preview,setPreview]=useState<CleanupPreview>();const [confirmed,setConfirmed]=useState(false);const [busy,setBusy]=useState(false);const [error,setError]=useState<string>();const [removed,setRemoved]=useState(false);
 const selected=workspaces.find(x=>JSON.stringify([x.nodeId,x.replica])===scope);
 const valid=!scope||!!selected;
 const review=async()=>{setBusy(true);setPreview(undefined);setConfirmed(false);setRemoved(false);setError(undefined);try{setPreview(await api.previewTaskCleanup(taskId,selected?.nodeId??'',selected?.replica??0));}catch(e){setError(errorMessage(e));}finally{setBusy(false);}};
 const remove=async()=>{if(!preview||!confirmed||!valid)return;setBusy(true);setError(undefined);try{await api.cleanupTaskWorkspace(taskId,selected?.nodeId??'',selected?.replica??0,preview.fingerprint);setPreview(undefined);setConfirmed(false);setRemoved(true);onChanged();}catch(e){setError(errorMessage(e));setConfirmed(false);setPreview(undefined);}finally{setBusy(false);}};
 return <details><summary>Review workspace cleanup</summary><p>Stop external writers before cleanup. Only a reviewed, clean linked worktree can be removed. Untracked and ignored files block removal. Branches, task history and the source repository are retained.</p>
 <label>Workspace to clean<select disabled={busy} value={scope} onChange={e=>{setScope(e.target.value);setPreview(undefined);setConfirmed(false);setRemoved(false);setError(undefined);}}><option value="">Shared task worktree</option>{workspaces.filter(x=>x.inspectionNode).map(x=><option key={JSON.stringify([x.nodeId,x.replica])} value={JSON.stringify([x.nodeId,x.replica])}>{x.nodeId} · replica {x.replica}</option>)}</select></label>
 {!valid&&<p>This workspace is no longer available in the current snapshot.</p>}
 <Btn disabled={busy||!valid} onClick={()=>void review()}>Preview cleanup</Btn><ErrorBox error={error}/>
 {preview&&valid&&<section aria-label="Workspace cleanup preview"><p>Path: <code>{preview.path}</code></p><p>Retained branch: {preview.branch} · Commit: {preview.head}</p><pre>{preview.status||'Working tree clean.'}</pre>{!preview.clean?<p>Files must be reviewed and preserved before cleanup. Weft will not force removal.</p>:<><label><input type="checkbox" checked={confirmed} onChange={e=>setConfirmed(e.target.checked)}/>I stopped external writers and authorize removal of this clean checkout, keeping its branch.</label><Btn disabled={busy||!confirmed} onClick={()=>void remove()}>Remove reviewed checkout</Btn></>}</section>}
 {removed&&<p>Checkout removed. Its branch and task history were retained.</p>}
 </details>;
}
