import type { Skill, SkillInput } from '../api/instructions';
export type SkillTab='content'|'workflows'|'history';
export type SkillsRoute={section?:'catalog'|'associations'|'inspect';skillId?:string;create?:boolean;copyFrom?:string;tab?:SkillTab;workflow?:string;agent?:string;execution?:string};
export function parseSkillsRoute(parts:string[],query=''):SkillsRoute {
  if(parts[1]==='new') {const source=new URLSearchParams(query).get('copyFrom');return {create:true,...(source?{copyFrom:source}:{})};}
  if(parts[1]==='s' && parts[2]) return {skillId:decodeURIComponent(parts[2]),tab:['content','workflows','history'].includes(parts[3])?parts[3] as SkillTab:'content'};
  if(parts[1]==='associations'||parts[1]==='inspect') {
    const q=new URLSearchParams(query);
    return {section:parts[1],...(q.get('workflow')?{workflow:q.get('workflow')!}:{}),...(q.get('agent')?{agent:q.get('agent')!}:{}),...(q.get('execution')?{execution:q.get('execution')!}:{})};
  }
  return {};
}
export function skillsHash(route:SkillsRoute={}):string {
  if(route.create) return '#/skills/new'+(route.copyFrom?`?${new URLSearchParams({copyFrom:route.copyFrom})}`:'');
  if(route.skillId) return `#/skills/s/${encodeURIComponent(route.skillId)}/${route.tab??'content'}`;
  const path=route.section && route.section!=='catalog'?`#/skills/${route.section}`:'#/skills';
  const query=new URLSearchParams();
  if(route.workflow) query.set('workflow',route.workflow);
  if(route.agent) query.set('agent',route.agent);
  if(route.execution) query.set('execution',route.execution);
  return path+(query.size?`?${query}`:'');
}
export function skillStatus(skill:Skill):string {return skill.deleted?'Deleted':skill.enabled?'Enabled':'Disabled';}
export function filterSkills(skills:Skill[],search:string,status:string,origin:string):Skill[] {
  const query=search.trim().toLowerCase();
  return skills.filter(skill=>(!query||`${skill.id} ${skill.title} ${skill.description}`.toLowerCase().includes(query)) &&
    (status==='all'||(status==='current'?!skill.deleted:skillStatus(skill).toLowerCase()===status)) &&
    (origin==='all'||(origin==='builtin'?!!skill.builtin:!skill.builtin)))
    .sort((a,b)=>(Date.parse(b.at)||0)-(Date.parse(a.at)||0)||a.id.localeCompare(b.id));
}
export function skillInput(skill:Skill) {return {id:skill.id,title:skill.title,description:skill.description,body:skill.body,enabled:skill.enabled,global:skill.global};}

// A copy is a new, independent skill. Reserved IDs and existing tombstones stay unavailable.
export function editableSkillCopy(source:Skill,skills:Skill[]):SkillInput {
  const stem=(source.id.replace(/^weft-/, '') || 'skill').slice(0,50).replace(/-+$/,'');
  const occupied=new Set(skills.map(skill=>skill.id));
  let id=`${stem}-copy`;let suffix=2;
  while(occupied.has(id))id=`${stem}-copy-${suffix++}`;
  return {id,title:source.title,description:source.description,body:source.body,enabled:true,global:false};
}
