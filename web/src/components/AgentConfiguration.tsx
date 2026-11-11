import type { GraphNode, GraphSummary } from '../api/types';
import type { ElementIssue } from './canvas/edit';
import { AgentEditor, NodeSettings } from './NodePanel';
import { agentBoundaries, agentSections, agentSectionTitles, type AgentSection } from './agentSettingsModel';
import { ElementIssues, FieldIssues } from './GraphIssues';

export function AgentConfiguration({summary, nodeId, section, onSection, onSelect, onUpdateNode, issues, dirty}: {
  summary:GraphSummary; nodeId:string; section:AgentSection; onSection:(section:AgentSection)=>void;
  onSelect:(id:string)=>void; onUpdateNode?:(id:string,patch:Partial<GraphNode>)=>void; issues?:ElementIssue[]; dirty:boolean;
}) {
 const node=summary.graph.spec.nodes.find(n=>n.id===nodeId);
 const boundary=node?agentBoundaries(summary.graph,node):undefined;
 return <section className="agent-configuration-page" aria-label="Agent settings">
  <label className="agent-selector">Agent<select aria-label="Agent" value={node?.id??''} onChange={e=>onSelect(e.target.value)}><option value="" disabled>Select an agent</option>{summary.graph.spec.nodes.map(n=><option key={n.id} value={n.id}>{n.name||n.id}</option>)}</select></label>
  {!node||!boundary?<p role="status">Select an agent from this graph. The requested agent may have been removed.</p>:<>
   <header className="page-head"><div><p className="muted">{summary.graph.metadata.name||summary.graph.metadata.id} / Agents</p><h1>{node.name||node.id}</h1><p>{dirty?'Unsaved graph draft':`Saved in graph version ${summary.record.version}`}</p></div></header>
   <nav className="section-navigation" aria-label="Agent configuration sections">{agentSections.map(s=><button key={s} type="button" aria-current={section===s?'page':undefined} onClick={()=>onSection(s)}>{agentSectionTitles[s]}</button>)}</nav>
   <ElementIssues issues={issues} label="Agent validation"/>
   <div className="agent-settings-layout"><div className="card">
    {!onUpdateNode&&<p role="note">Read-only view. An operator or administrator can edit the graph.</p>}
    {section==='instructions'?<section aria-label="Instructions editor"><h2>Instructions</h2><label>Agent instructions<textarea aria-label="Agent instructions" rows={14} value={node.prompt??''} readOnly={!onUpdateNode} onChange={e=>onUpdateNode?.(node.id,{prompt:e.target.value})}/></label><FieldIssues issues={issues} field="prompt"/><p className="muted">Instructions guide behavior. Tool permissions are enforced separately.</p></section>:<>
     <fieldset className="agent-fields" disabled={!onUpdateNode}><AgentEditor summary={summary} nodeId={node.id} section={section} onUpdateNode={(id,patch)=>onUpdateNode?.(id,patch)} issues={issues}/></fieldset>
     {section==='tools'&&<NodeSettings toolsOnly summary={summary} nodeId={node.id} onUpdateNode={onUpdateNode} issues={issues}/>}
    </>}
   </div><aside className="card agent-boundaries" aria-label="Execution boundaries"><h2>Execution boundaries</h2><dl className="kv"><dt>Workspace</dt><dd>{boundary.readOnly?'Read-only':'Writes subject to tool grants'}</dd><dt>Isolation</dt><dd>{boundary.isolation}</dd><dt>Run mode</dt><dd>{boundary.loop}</dd><dt>Tool grants</dt><dd>{boundary.tools.length} {node.tools===undefined?'(graph defaults)':'(agent override)'}</dd></dl>
    <p className="muted">Draft configuration. Installation policies, tool constraints and approvals still apply on every call. Running replicas may use an earlier version.</p><p>Changes join the complete graph draft. Review all changed fields before saving.</p></aside></div>
  </>}
 </section>;
}
