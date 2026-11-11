import {useState} from 'react';
import {api,errorMessage} from '../api/client';
import type {Checkpoint,CheckpointRestore} from '../api/types';
import {Btn,ErrorBox,fmtDateTime} from './common';

const reasonLabel=(c:Checkpoint)=>c.reason==='before_restore'?'Before a restore':c.tool?`Before ${c.tool}`:'Before an edit';

/** Worktree checkpoints of a task's agents (NAT-07). Loads only when opened; restoring is an administrator action with explicit confirmation. */
export function TaskCheckpoints({graphId,canRestore}:{graphId:string;canRestore:boolean}) {
 const [list,setList]=useState<Checkpoint[]>();const [error,setError]=useState<string>();const [busy,setBusy]=useState(false);
 const [selected,setSelected]=useState('');const [confirmed,setConfirmed]=useState(false);const [restored,setRestored]=useState<CheckpointRestore>();
 const load=async()=>{setBusy(true);setError(undefined);try{const items=await api.checkpoints(graphId);setList(items);setSelected(items.find(c=>c.reason==='before_edit')?.id??'');}catch(e){setError(errorMessage(e));}finally{setBusy(false);}};
 const restore=async()=>{if(!selected||!confirmed)return;setBusy(true);setError(undefined);setRestored(undefined);try{setRestored(await api.restoreCheckpoint(graphId,selected));setConfirmed(false);await load();}catch(e){setError(errorMessage(e));setConfirmed(false);setBusy(false);}};
 const chosen=list?.find(c=>c.id===selected);
 return <details onToggle={e=>{if(e.currentTarget.open&&!list&&!busy)void load();}}><summary>Checkpoints</summary>
  <p>Before the edits of each round, the agent's workspace is recorded as a checkpoint on a hidden Git ref. Failed runs are never restored automatically: their files and checkpoints stay for inspection.</p>
  <ErrorBox error={error}/>
  {list&&list.length===0&&<p>No checkpoints recorded. Workspaces that are not Git repositories have none.</p>}
  {list&&list.length>0&&<table aria-label="Workspace checkpoints"><thead><tr><th>Checkpoint</th><th>Agent</th><th>Recorded</th><th>When</th><th>Execution</th></tr></thead>
   <tbody>{list.map(c=><tr key={c.id}><td><code>{c.id}</code></td><td>{c.nodeId} · replica {c.replica}</td><td>{reasonLabel(c)}</td><td>{fmtDateTime(c.createdAt)}</td><td>{c.executionId??'—'}</td></tr>)}</tbody></table>}
  {list&&list.length>0&&!canRestore&&<p>Only administrators can restore a checkpoint.</p>}
  {list&&list.length>0&&canRestore&&<section aria-label="Restore a checkpoint">
   <label>Checkpoint to restore<select disabled={busy} value={selected} onChange={e=>{setSelected(e.target.value);setConfirmed(false);setRestored(undefined);}}><option value="">Select a checkpoint</option>{list.map(c=><option key={c.id} value={c.id}>{c.id} · {reasonLabel(c)} · {fmtDateTime(c.createdAt)}</option>)}</select></label>
   {chosen&&<p>Restoring rewrites the files of {chosen.nodeId} replica {chosen.replica} to this checkpoint. The current state is saved as a new checkpoint first. Ignored files, the Git index, commits and branches are not changed. A running agent turn blocks the restore.</p>}
   <label><input type="checkbox" disabled={busy||!chosen} checked={confirmed} onChange={e=>setConfirmed(e.target.checked)}/>I authorize restoring this agent workspace to the selected checkpoint.</label>
   <Btn disabled={busy||!chosen||!confirmed} onClick={()=>void restore()}>Restore checkpoint</Btn>
  </section>}
  {restored&&<p role="status">Restored {restored.restored.id}: {restored.written} files written, {restored.removed} removed. The previous state is checkpoint {restored.saved.id}.</p>}
 </details>;
}
