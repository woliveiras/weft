import {RepositoryImport} from './RepositoryImport';
import {ProjectPreference} from './ProjectPreference';
import {useState} from 'react';
import {api,errorMessage} from '../api/client';
import {useAsync} from '../hooks/useData';
import {Btn,ErrorBox} from './common';
export function ProjectsPage({canEdit,nativeProfile=false}:{canEdit:boolean;nativeProfile?:boolean}){
 const list=useAsync(()=>api.projects(),[]);
 const [name,setName]=useState('');const [path,setPath]=useState('');const [confirmed,setConfirmed]=useState(false);
 const [importOpen,setImportOpen]=useState(false);
 const [busy,setBusy]=useState(false);const [error,setError]=useState<string>();
 const save=async()=>{setBusy(true);setError(undefined);try{await api.createProject({name,path});setName('');setPath('');setConfirmed(false);await list.reload();}catch(e){setError(errorMessage(e));}finally{setBusy(false);}};
 return <section className="page installation-page"><header className="page-head"><div><h1>Projects</h1><p>Register a repository, then select it when creating a task.</p></div></header>
 <p>Paths are on the machine running Weft. A Docker mount refers to the container filesystem; it does not execute commands on your host.</p>
 <ErrorBox error={list.error}/><Btn onClick={()=>void list.reload()}>Refresh projects</Btn>
 <ul className="task-list">{list.data?.map(p=><li key={p.id}><h2>{p.name}</h2><p>{p.path}</p><p>Registered commit: <code>{p.head}</code></p><ProjectPreference key={`${p.id}:${p.revision}`} project={p} canEdit={canEdit} nativeProfile={nativeProfile} onSaved={()=>void list.reload()}/><a href="#/tasks/new">Create task</a></li>)}</ul>
 {list.data?.length===0&&<p>No projects registered yet.</p>}
 <details onToggle={e=>setImportOpen(e.currentTarget.open)}><summary>Import from URL</summary>{importOpen&&<RepositoryImport nativeProfile={nativeProfile} canEdit={canEdit} onProject={()=>void list.reload()}/>}</details>
 <form className="card form" onSubmit={e=>{e.preventDefault();void save();}} aria-label="Register project">
 <h2>Register repository</h2><fieldset disabled={!canEdit||busy}>
 <label>Project name<input value={name} required maxLength={200} onChange={e=>setName(e.target.value)}/></label>
 <label>Repository path<input value={path} required maxLength={4096} placeholder="/absolute/path/to/repository" onChange={e=>setPath(e.target.value)}/></label>
 <label><input type="checkbox" checked={confirmed} onChange={e=>setConfirmed(e.target.checked)}/>I selected this repository on the Weft server and authorize reading its Git metadata.</label>
 <p>Registration does not execute agents. Task worktrees start from a reviewed commit; uncommitted source changes stay in your checkout. Worktrees separate files, not OS permissions.</p>
 <a href="https://woliveiras.com/posts/why-git-worktrees-matter-coding-agents/" target="_blank" rel="noreferrer">Learn about worktrees</a>
 </fieldset><ErrorBox error={error}/><Btn type="submit" tone="primary" disabled={!canEdit||busy||!confirmed}>Register project</Btn></form>
 </section>;
}
