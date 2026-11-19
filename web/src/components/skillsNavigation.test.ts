import {expect,it} from 'vitest';
import {parseSkillsRoute,skillsHash,filterSkills,editableSkillCopy} from './skillsNavigation';
import type {Skill} from '../api/instructions';
import {skillUses} from './SkillWorkflows';
import type {GraphSummary} from '../api/types';
const item:Skill={id:'review',title:'Review',description:'Security guidance',body:'Read',enabled:true,global:false,revision:1,actor:'admin',at:'2026-11-19',deleted:false,history:[]};
it('round trips legacy, editor and association/inspection routes',()=>{
  expect(parseSkillsRoute(['skills'])).toEqual({});
  for(const route of [{create:true},{create:true,copyFrom:'weft-planner'},{skillId:'a/b',tab:'history' as const},{section:'inspect' as const,workflow:'a/b',agent:'reviewer',execution:'exe_1'},{section:'associations' as const,workflow:'g'}]) {
    const [path,query]=skillsHash(route).slice(1).split('?');expect(parseSkillsRoute(path.split('/').filter(Boolean),query)).toEqual(route);
  }
  expect(parseSkillsRoute(['skills','unknown'])).toEqual({});
});
it('combines search, status and origin and sorts latest first with stable ties',()=>{
  const data=[item,{...item,id:'old',at:'2026-01-01',builtin:true,enabled:false},{...item,id:'gone',deleted:true}];
  expect(filterSkills(data,'SECURITY','current','all').map(s=>s.id)).toEqual(['review','old']);
  expect(filterSkills(data,'','disabled','builtin').map(s=>s.id)).toEqual(['old']);
  expect(filterSkills(data,'','deleted','custom').map(s=>s.id)).toEqual(['gone']);
  expect(filterSkills(data,'none','all','all')).toEqual([]);
});
it('distinguishes inherited, narrowed and installation-default workflow uses',()=>{
  const graph={metadata:{id:'g'},spec:{instructions:{skillIds:['review']},nodes:[{id:'inherited'},{id:'excluded',skillIds:[]},{id:'explicit',skillIds:['review']}]}};
  const uses=skillUses(item,[{graph} as GraphSummary]);
  expect(uses[0].agents.map(a=>a.uses)).toEqual([true,false,true]);
  expect(skillUses({...item,global:true},[{graph:{...graph,spec:{...graph.spec,instructions:{installationDefaults:true}}}} as GraphSummary])[0].agents.every(a=>a.uses)).toBe(true);
  expect(skillUses(item,[{graph:{...graph,metadata:{id:'task-run-copy'}}} as GraphSummary])).toEqual([]);
});

it('prepares an independent editable draft with an unreserved unused ID',()=>{
  const source={...item,id:'weft-planner',builtin:true,global:true,history:[item]};
  const draft=editableSkillCopy(source,[source,{...item,id:'planner-copy',deleted:true},{...item,id:'planner-copy-2'}]);
  expect(draft).toEqual({id:'planner-copy-3',title:source.title,description:source.description,body:source.body,enabled:true,global:false});
  expect(source.global).toBe(true);
  const long=editableSkillCopy({...source,id:'a'.repeat(63)},[]);
  expect(long.id.length).toBeLessThanOrEqual(63);
  expect(long.id).not.toMatch(/^weft-/);
});
