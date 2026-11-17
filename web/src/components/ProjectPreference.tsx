import {useState} from 'react';
import type {Project} from '../api/tasks';
import {api,errorMessage} from '../api/client';
import {Btn,ErrorBox} from './common';
export function ProjectPreference({project,canEdit,nativeProfile,onSaved}:{project:Project;canEdit:boolean;nativeProfile:boolean;onSaved:()=>void}) {
 const [mode,setMode]=useState(project.executionMode??'workflow');const [busy,setBusy]=useState(false);const [error,setError]=useState<string>();
 const save=async()=>{setBusy(true);setError(undefined);try{await api.saveProjectPreference(project.id,project.revision,mode);onSaved();}catch(e){setError(errorMessage(e));}finally{setBusy(false);}};
 return <section aria-label={`Execution preference for ${project.name}`}><label>Preferred execution environment<select disabled={!canEdit||busy} value={mode} onChange={e=>setMode(e.target.value as typeof mode)}><option value="workflow">Use workflow environment</option><option value="local" disabled={!nativeProfile}>Local worktree</option><option value="sandbox">Sandbox</option></select></label><p>Suggested when selecting this project for a new task. Every task still requires compatible workflow settings and explicit review; saved drafts are unchanged.</p><ErrorBox error={error}/><Btn disabled={!canEdit||busy||mode===(project.executionMode??'workflow')} onClick={()=>void save()}>Save environment preference</Btn></section>;
}
