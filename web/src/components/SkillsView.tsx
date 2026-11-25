import {useFieldValidation} from './useFieldValidation';
import {useEffect,useState,type MouseEvent,type ReactNode} from 'react';
import {api,errorMessage} from '../api/client';
import type {Skill,SkillInput} from '../api/instructions';
import {useAsync} from '../hooks/useData';
import {Badge,Btn,ContextHelp,ErrorBox} from './common';
import {HelpField} from './FieldHelp';
import {SkillMarkdown} from './SkillMarkdown';
import {SkillAssociations} from './SkillAssociations';
import {SkillWorkflows} from './SkillWorkflows';
import {editableSkillCopy,filterSkills,skillInput,skillsHash,skillStatus,type SkillsRoute} from './skillsNavigation';

const blank:SkillInput={id:'',title:'',description:'',body:'',enabled:true,global:false};
const tabs={content:'Content',workflows:'Workflows',history:'History'} as const;
type Navigate=(route:SkillsRoute)=>void;
function SkillLink({route,navigate,children,className}:{route:SkillsRoute;navigate:Navigate;children:ReactNode;className?:string}) {
  return <a href={skillsHash(route)} className={className} onClick={(event:MouseEvent<HTMLAnchorElement>)=>{
    if(event.button===0&&!event.metaKey&&!event.ctrlKey&&!event.shiftKey&&!event.altKey){event.preventDefault();navigate(route);}
  }}>{children}</a>;
}
export function SkillsView({role,onChanged,route,onNavigate}:{role?:string;onChanged:()=>void;route?:SkillsRoute;onNavigate?:Navigate}) {
  const catalog=useAsync(api.skills,[]);
  const [localRoute,setLocalRoute]=useState<SkillsRoute>({});
  const current=route??localRoute;
  const [dirty,setDirty]=useState(false);
  const [busy,setBusy]=useState(false);
  const [creationNotice,setCreationNotice]=useState<string>();
  const admin=role==='admin';
  const editing=!!current.skillId||!!current.create;
  const section=editing?'catalog':current.section??'catalog';
  const setRoute:Navigate=next=>{if(onNavigate)onNavigate(next);else setLocalRoute(next);};
  const navigate:Navigate=next=>{
    const sameSkill=editing && next.skillId===current.skillId && next.create===current.create && next.copyFrom===current.copyFrom && (next.skillId||next.create);
    const sameWorkflow=!editing && current.workflow===next.workflow && current.section===next.section;
    if(busy)return;
    if(dirty&&!sameSkill&&!sameWorkflow&&!window.confirm('Discard unsaved changes?'))return;
    if(!sameSkill&&!sameWorkflow)setDirty(false);
    setCreationNotice(undefined);setRoute(next);
  };
  useEffect(()=>{
    if(!dirty)return;
    const warn=(event:BeforeUnloadEvent)=>{event.preventDefault();event.returnValue='';};
    window.addEventListener('beforeunload',warn);return()=>window.removeEventListener('beforeunload',warn);
  },[dirty]);
  const requestedId=current.create?current.copyFrom:current.skillId;
  const selected=catalog.data?.find(skill=>skill.id===requestedId);
  return <div className="page skills-page">
    {!editing && <header className="page-head"><div><h1>Skills &amp; instructions</h1><p className="muted">Reusable guidance, workflow associations and instruction records.</p></div>
      {section==='catalog'&&admin&&<SkillLink route={{create:true}} navigate={navigate} className="btn btn-primary">New skill</SkillLink>}
    </header>}
    {!editing && <>
      <nav className="section-navigation" aria-label="Skills sections">{Object.entries({catalog:'Skills',associations:'Workflow associations',inspect:'Inspect instructions'}).map(([key,label])=><a key={key} href={skillsHash({section:key as SkillsRoute['section']})} aria-current={section===key?'page':undefined} onClick={event=>{event.preventDefault();navigate({section:key as SkillsRoute['section'],...(key!=='catalog'?{workflow:current.workflow,agent:current.agent}: {})});}}>{label}</a>)}</nav>
      <div className="skills-guidance"><p className="muted small">Guidance never grants tools, credentials or permissions.</p><ContextHelp title="How instruction precedence works"><p>Later scopes take precedence: installation → graph → project → agent → task. Workflow opt-in and agent selections still determine which skills are loaded.</p></ContextHelp></div>
    </>}
    {creationNotice&&<p role="status">{creationNotice}</p>}
    {catalog.loading&&!catalog.data&&<p role="status">Loading skills…</p>}
    <ErrorBox error={catalog.error}/>
    {catalog.error&&<Btn onClick={()=>void catalog.reload()}>Retry loading skills</Btn>}
    {editing ? <>
      {requestedId&&!selected ? !catalog.loading&&!catalog.error&&<><h1>{current.create?'Source skill not found':'Skill not found'}</h1><SkillLink route={{}} navigate={navigate}>Back to skills</SkillLink></> :
        <SkillDetails key={current.create?`new-${current.copyFrom??''}`:current.skillId} skill={current.create?undefined:selected} copyFrom={current.create?selected:undefined} initialDraft={current.create&&selected?editableSkillCopy(selected,catalog.data??[]):undefined} create={!!current.create} tab={current.tab??'content'} canEdit={admin&&!catalog.error&&!catalog.loading}
          navigate={navigate} onDirty={setDirty} onBusy={setBusy} onSaved={skill=>{
            catalog.setData(items=>[...(items??[]).filter(item=>item.id!==skill.id),skill]);
            if(current.create){setCreationNotice(`Skill saved as revision ${skill.revision}.`);setDirty(false);if(onNavigate)onNavigate({skillId:skill.id});else setLocalRoute({skillId:skill.id});}
          }}/>}
    </> : section==='catalog' ? <SkillCatalog skills={catalog.data??[]} ready={!!catalog.data} navigate={navigate} onReload={()=>void catalog.reload()}/> :
      <SkillAssociations key={section} skills={catalog.data??[]} skillsUnavailable={!catalog.data||!!catalog.error||catalog.loading} role={role} mode={section} workflowId={current.workflow} agentId={current.agent} executionId={current.execution}
        onSelection={(workflow,agent,execution)=>setRoute({section,workflow,agent,execution})} onDirty={setDirty} onBusy={setBusy} onChanged={onChanged}/>}
  </div>;
}
function SkillCatalog({skills,ready,navigate,onReload}:{skills:Skill[];ready:boolean;navigate:Navigate;onReload:()=>void}) {
  const [search,setSearch]=useState('');const [status,setStatus]=useState('current');const [origin,setOrigin]=useState('all');const [page,setPage]=useState(0);
  const filtered=filterSkills(skills,search,status,origin);const pages=Math.max(1,Math.ceil(filtered.length/20));const current=Math.min(page,pages-1);
  return <section aria-label="Skill catalog">
    <div className="skills-filters">
      <label>Search skills<input type="search" value={search} placeholder="Name, description or ID" onChange={event=>{setSearch(event.target.value);setPage(0);}}/></label>
      <label>Skill status<select value={status} onChange={event=>{setStatus(event.target.value);setPage(0);}}><option value="current">Current skills</option><option value="enabled">Enabled</option><option value="disabled">Disabled</option><option value="deleted">Deleted</option><option value="all">All statuses</option></select></label>
      <label>Origin<select value={origin} onChange={event=>{setOrigin(event.target.value);setPage(0);}}><option value="all">All origins</option><option value="builtin">Built-in</option><option value="custom">Custom</option></select></label>
      <Btn onClick={()=>{setSearch('');setStatus('current');setOrigin('all');setPage(0);}}>Clear filters</Btn><Btn onClick={onReload}>Refresh skills</Btn>
    </div>
    {ready&&<><p className="muted small">{filtered.length} of {skills.length} skills · Latest updated first</p>
      {filtered.length?<div className="skills-table-scroll" tabIndex={0} role="region" aria-label="Skills table"><table className="table"><thead><tr><th>Skill</th><th>Origin</th><th>Status</th><th>Revision</th><th>Updated</th></tr></thead>
        <tbody>{filtered.slice(current*20,(current+1)*20).map(skill=><tr key={skill.id}>
          <td><SkillLink route={{skillId:skill.id}} navigate={navigate}>{skill.title||skill.id}</SkillLink><span className="muted small skill-table-description">{skill.description||skill.id}</span></td>
          <td>{skill.builtin?<><Badge>Built-in</Badge><span className="muted small skill-table-description">Read-only</span></>:'Custom'}</td><td><Badge tone={skill.deleted?'bad':skill.enabled?'ok':undefined}>{skillStatus(skill)}</Badge>{skill.global&&<span className="muted small skill-table-description">Installation default</span>}</td>
          <td>v{skill.revision}</td><td>{skill.at?<time dateTime={skill.at}>{new Date(skill.at).toLocaleString()}</time>:'Not recorded'}<span className="muted small skill-table-description">{skill.actor}</span></td>
        </tr>)}</tbody></table></div>:<p className="empty">{skills.length?'No skills match these filters.':'No skills yet. Create one to define reusable guidance.'}</p>}
      {pages>1&&<nav className="btn-row" aria-label="Skill pages"><Btn disabled={current===0} onClick={()=>setPage(current-1)}>Previous</Btn><span>Page {current+1} of {pages}</span><Btn disabled={current===pages-1} onClick={()=>setPage(current+1)}>Next</Btn></nav>}
    </>}
  </section>;
}
function SkillDetails({skill,copyFrom,initialDraft,create,tab,canEdit,navigate,onDirty,onBusy,onSaved}:{skill?:Skill;copyFrom?:Skill;initialDraft?:SkillInput;create:boolean;tab:keyof typeof tabs;canEdit:boolean;navigate:Navigate;onDirty:(value:boolean)=>void;onBusy:(value:boolean)=>void;onSaved:(skill:Skill)=>void}) {
  const [saved,setSaved]=useState(skill);
  const [form,setForm]=useState<SkillInput>(skill?skillInput(skill):initialDraft??blank);
  const [preview,setPreview]=useState(false);const [busy,setBusy]=useState(false);const [error,setError]=useState<string>();const [notice,setNotice]=useState<string>();
  const dirty=JSON.stringify(form)!==JSON.stringify(saved?skillInput(saved):blank);
  const readOnly=!canEdit||!!saved?.builtin;
  useEffect(()=>{onDirty(dirty);return()=>onDirty(false);},[dirty,onDirty]);
  useEffect(()=>{onBusy(busy);return()=>onBusy(false);},[busy,onBusy]);
  const update=(patch:Partial<SkillInput>)=>{setForm(value=>({...value,...patch}));setNotice(undefined);};
  const validation=useFieldValidation({
    id: !/^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/.test(form.id) ? 'Use a lowercase ID with letters, numbers or hyphens, up to 63 characters; end with a letter or number.' : undefined,
    title: !form.title.trim() ? 'Enter a title.' : undefined,
    body: !form.body.trim() ? 'Enter Markdown instructions.' : new TextEncoder().encode(form.body).length>16*1024 ? 'Markdown instructions must fit within 16 KiB of UTF-8 text.' : undefined,
  });
  const save=async()=>{
    setError(undefined);setNotice(undefined);
    if(!validation.validate()){
      setPreview(false);
      if(!create && tab!=='content') navigate({skillId:saved?.id,tab:'content'});
      return;
    }
    setBusy(true);
    try{const result=await api.saveSkill(form,saved?.revision);setSaved(result);setForm(skillInput(result));onSaved(result);setNotice(`Skill saved as revision ${result.revision}.`);}catch(e){setError(errorMessage(e));}finally{setBusy(false);}
  };
  const remove=async()=>{
    if(!saved||!window.confirm(`Delete ${saved.title}? History is retained. Deletion is refused while a saved or active workflow references this skill.`))return;
    setBusy(true);setError(undefined);
    try{const result=await api.deleteSkill(saved.id,saved.revision);setSaved(result);setForm(skillInput(result));onSaved(result);setNotice('Skill deleted. Revision history is retained.');}catch(e){setError(errorMessage(e));}finally{setBusy(false);}
  };
  return <div ref={validation.ref} className="skill-detail">
    <SkillLink route={{}} navigate={navigate}>Back to skills</SkillLink>
    <header className="page-head"><div><h1>{create?'New skill':saved?.title||'Skill'}</h1>{saved&&<p className="muted">{saved.id} · Revision {saved.revision} · {skillStatus(saved)} {saved.builtin&&<Badge>Built-in · Read-only</Badge>}</p>}</div>
      <div className="btn-row">{dirty&&<Badge tone="warn">Unsaved changes</Badge>}{!readOnly&&<Btn tone="primary" disabled={busy||!dirty} onClick={()=>void save()}>{busy?'Saving…':'Save'}</Btn>}</div>
    </header>
    {saved?.builtin&&<aside className="skill-origin-notice" aria-label="Built-in skill" role="note"><div><strong>Included with Weft · Read-only</strong><p>This skill is maintained by Weft and cannot be edited. Create an independent copy to customize its instructions, then select the copy in your workflows.</p>{!canEdit&&<p>Only administrators can create an editable copy.</p>}</div>{canEdit&&<SkillLink route={{create:true,copyFrom:saved.id}} navigate={navigate} className="btn btn-primary">Create editable copy</SkillLink>}</aside>}
    {copyFrom&&<aside className="skill-origin-notice" role="note"><div><strong>Editable copy of {copyFrom.title}</strong><p>Review the ID and content, then Save to create a custom skill. The original stays read-only. Workflow associations and installation defaults are not copied.</p></div></aside>}
    {!canEdit&&!saved?.builtin&&<p role="note">Only administrators can create or change skills.</p>}
    {saved?.deleted&&<p role="note">This skill is deleted and is not loaded. History is retained; saving an enabled revision can restore it.</p>}
    {notice&&<p role="status">{notice}</p>}<ErrorBox error={error}/>
    {!create&&<nav className="section-navigation" aria-label="Skill details sections">{Object.entries(tabs).map(([key,label])=><a key={key} href={skillsHash({skillId:saved?.id,tab:key as keyof typeof tabs})} aria-current={tab===key?'page':undefined} onClick={event=>{event.preventDefault();navigate({skillId:saved?.id,tab:key as keyof typeof tabs});}}>{label}</a>)}</nav>}
    {tab==='content'||create?<form className="card form skill-content-form" aria-label="Skill content" onSubmit={event=>{event.preventDefault();if(!readOnly&&!busy&&dirty)void save();}}>
      <fieldset disabled={readOnly||busy}>
        <div className="skill-metadata-fields"><HelpField error={validation.errors.id} label="Skill ID" help="Stable reference used in workflows. Choose a lowercase identifier; the ID cannot change after creation."><input value={form.id} disabled={!!saved} onChange={event=>update({id:event.target.value})}/></HelpField>
        <HelpField error={validation.errors.title} label="Title" help="A short name that identifies this skill in the catalogue."><input id="skill-title" value={form.title} maxLength={200} onChange={event=>update({title:event.target.value})}/></HelpField></div>
        <label htmlFor="skill-description">Description</label><textarea id="skill-description" rows={3} value={form.description} maxLength={1000} onChange={event=>update({description:event.target.value})}/>
      </fieldset>
      <div className="btn-row" role="group" aria-label="Markdown mode"><Btn aria-pressed={!preview} onClick={()=>setPreview(false)}>Write</Btn><Btn aria-pressed={preview} onClick={()=>setPreview(true)}>Preview</Btn></div>
      {preview?<><p className="muted small">Preview supports headings, lists, code, emphasis and web links. Raw HTML and images stay as text.</p><SkillMarkdown body={form.body}/></>:<HelpField error={validation.errors.body} label="Markdown body" help="Describe when to use this skill, the steps to follow and decision criteria. Instructions cannot grant tools or override permissions."><textarea rows={14} value={form.body} disabled={readOnly||busy} onChange={event=>update({body:event.target.value})}/></HelpField>}
      <p className="muted small">{new TextEncoder().encode(form.body).length.toLocaleString()} / 16,384 UTF-8 bytes · Saving creates an immutable revision. No files or scripts are executed.</p>
      <fieldset disabled={readOnly||busy} className="skill-options">
        <HelpField label="Enabled" help="An enabled skill may be loaded when selected by a workflow. Disabled skills keep their history but do not contribute instructions."><input type="checkbox" checked={form.enabled} onChange={event=>update({enabled:event.target.checked})}/></HelpField>
        <HelpField label="Installation default" help="Marks this skill as a default candidate. It is loaded only by workflows that explicitly enable installation defaults; this does not attach it to every workflow."><input type="checkbox" checked={form.global} onChange={event=>update({global:event.target.checked})}/></HelpField>
      </fieldset>
      {saved&&!readOnly&&!saved.deleted&&<div><Btn tone="danger" disabled={busy} onClick={()=>void remove()}>Delete skill</Btn></div>}
    </form>:tab==='workflows'&&saved?<SkillWorkflows skill={saved} navigate={navigate}/>:saved?<section className="card skill-history" aria-label="Revision history"><h2>Revision history</h2><p className="muted small">Newest revisions first. Saved executions retain their original instruction snapshot.</p>{[...saved.history,saved].sort((a,b)=>b.revision-a.revision).map(version=><details key={version.revision}><summary>Revision {version.revision} · {version.actor} · {version.at}{version.deleted?' · Deleted':''}</summary><p>{version.description}</p><pre>{version.body}</pre></details>)}</section>:null}
  </div>;
}
