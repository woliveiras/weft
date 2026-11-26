import { HelpField } from './FieldHelp';
import {useState} from 'react';
import {api,errorMessage} from '../api/client';
import type {CleanupPreview,TaskWorkspace} from '../api/tasks';
import {Btn,ErrorBox} from './common';

export function TaskCleanup({taskId,workspaces,repositories=[],onChanged}:{taskId:string;workspaces:TaskWorkspace[];repositories?:string[];onChanged:()=>void}) {
 const [scope,setScope]=useState('');const [preview,setPreview]=useState<CleanupPreview>();const [confirmed,setConfirmed]=useState(false);const [busy,setBusy]=useState(false);const [error,setError]=useState<string>();const [removed,setRemoved]=useState(false);
 const selected=workspaces.find(x=>JSON.stringify([x.nodeId,x.replica])===scope);
 const repository=repositories.includes(scope)?scope:undefined;
 const valid=repositories.length?!!repository:!scope||!!selected;
 const review=async()=>{setBusy(true);setPreview(undefined);setConfirmed(false);setRemoved(false);setError(undefined);try{setPreview(await api.previewTaskCleanup(taskId,selected?.nodeId??'',selected?.replica??0,repository));}catch(e){setError(errorMessage(e));}finally{setBusy(false);}};
 const remove=async()=>{if(!preview||!confirmed||!valid)return;setBusy(true);setError(undefined);try{await api.cleanupTaskWorkspace(taskId,selected?.nodeId??'',selected?.replica??0,preview.fingerprint,repository);setPreview(undefined);setConfirmed(false);setRemoved(true);onChanged();}catch(e){setError(errorMessage(e));setConfirmed(false);setPreview(undefined);}finally{setBusy(false);}};
 return <details><summary>Review workspace cleanup</summary><p>Stop external writers before cleanup. Only a reviewed, clean task checkout can be removed. Untracked and ignored files block removal. Branches, task history and the source repository are retained.</p>
 <HelpField label={"Workspace to clean"} help={"Choose which task workspace to remove after reviewing the preview. Check the path and retained branch first so you can keep the work you still need."}><select disabled={busy} value={scope} onChange={e=>{setScope(e.target.value);setPreview(undefined);setConfirmed(false);setRemoved(false);setError(undefined);}}><option value="">{repositories.length?"Select repository":"Shared task checkout"}</option>{repositories.map(alias=><option key={alias}>{alias}</option>)}{workspaces.filter(x=>x.inspectionNode).map(x=><option key={JSON.stringify([x.nodeId,x.replica])} value={JSON.stringify([x.nodeId,x.replica])}>{x.nodeId} · replica {x.replica}</option>)}</select></HelpField>
 {!valid&&<p>This workspace is no longer available in the current snapshot.</p>}
 <Btn disabled={busy||!valid} onClick={()=>void review()}>Preview cleanup</Btn><ErrorBox error={error}/>
 {preview&&valid&&<section aria-label="Workspace cleanup preview"><p>Path: <code>{preview.path}</code></p><p>Retained branch: {preview.branch} · Commit: {preview.head}</p><pre>{preview.status||'Working tree clean.'}</pre>{preview.retainedRepository&&<p>The task’s Git repository, including its branches and checkpoints, will be retained outside the removed checkout.</p>}{!preview.clean?<p>Files must be reviewed and preserved before cleanup. Weft will not force removal.</p>:<><label><input type="checkbox" checked={confirmed} onChange={e=>setConfirmed(e.target.checked)}/>I stopped external writers and authorize removal of this clean checkout, keeping its branch.</label><Btn disabled={busy||!confirmed} onClick={()=>void remove()}>Remove reviewed checkout</Btn></>}</section>}
 {removed&&<p>Checkout removed. Its branch and task history were retained.</p>}
 </details>;
}
