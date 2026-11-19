import type {Skill} from '../api/instructions';
import type {GraphSummary} from '../api/types';
import {api} from '../api/client';
import {useAsync} from '../hooks/useData';
import {Btn,ErrorBox} from './common';
import {skillsHash,type SkillsRoute} from './skillsNavigation';

export function skillUses(skill:Skill,workflows:GraphSummary[]) {
  return workflows.filter(({graph})=>!graph.metadata.id.startsWith('task-run-')&&!graph.metadata.labels?.['weft.task']).flatMap(({graph})=>{
    const refs=graph.spec.instructions;
    const selected=refs?.skillIds?.includes(skill.id)??false;
    const defaulted=!!refs?.installationDefaults&&skill.global;
    if(!selected&&!defaulted)return [];
    return [{graph,source:selected?'Selected in workflow':'Installation default (workflow opted in)',agents:graph.spec.nodes.map(node=>({
      node,uses:defaulted||node.skillIds==null||node.skillIds.includes(skill.id),inherited:defaulted||node.skillIds==null,
    }))}];
  });
}
export function SkillWorkflows({skill,navigate}:{skill:Skill;navigate:(route:SkillsRoute)=>void}) {
  const workflows=useAsync(api.listGraphs,[]);
  const uses=skillUses(skill,workflows.data??[]);
  return <section className="card skill-workflows" aria-label="Workflows using this skill"><h2>Workflows</h2>
    <p className="muted small">Saved associations, not a guarantee of loading. Disabled or deleted skills do not contribute instructions; execution records retain their original revisions.</p>
    {workflows.loading&&<p role="status">Loading workflow associations…</p>}<ErrorBox error={workflows.error}/>
    {workflows.error&&<Btn onClick={()=>void workflows.reload()}>Retry workflow associations</Btn>}
    {workflows.data&&!uses.length&&<p>No saved workflow uses this skill.</p>}
    {uses.map(({graph,source,agents})=><section key={graph.metadata.id} className="skill-workflow-use"><h3><a href={skillsHash({section:'associations',workflow:graph.metadata.id})} onClick={event=>{event.preventDefault();navigate({section:'associations',workflow:graph.metadata.id});}}>{graph.metadata.name||graph.metadata.id}</a></h3><p>{source}</p>
      <ul>{agents.map(({node,uses,inherited})=><li key={node.id}><a href={`#/g/${encodeURIComponent(graph.metadata.id)}/agent?agent=${encodeURIComponent(node.id)}&section=instructions`}>{node.name||node.id}</a><span className="muted small">{uses?(inherited?'Inherited from workflow':'Explicit agent selection'):'Excluded by agent selection'}</span></li>)}</ul>
    </section>)}
  </section>;
}
