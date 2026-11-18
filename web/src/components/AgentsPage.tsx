import { useState, type ReactNode } from 'react';
import { api, errorMessage } from '../api/client';
import { workflowAgentLink, type Agent, type AgentDefinition } from '../api/agents';
import type { GraphNode, GraphSummary } from '../api/types';
import { useAsync } from '../hooks/useData';
import { AgentEditor, NodeSettings } from './NodePanel';
import { Btn, ErrorBox, Icon } from './common';

interface InventoryProps {
  workflows: GraphSummary[];
  workflowsLoading: boolean;
  workflowsError?: string;
  onReloadWorkflows: () => Promise<void>;
  canEdit: boolean;
}

const definitionSections = {
  general: 'General', instructions: 'Instructions', tools: 'Tools & permissions',
  parameters: 'Parameters', workflows: 'Workflows',
};
type DefinitionSection = keyof typeof definitionSections;

export function AgentsPage({id, create, ...inventory}: InventoryProps & {id?: string; create?: boolean}) {
  return <section className="page" aria-label="Agent catalog">
    {create ? <NewAgent canEdit={inventory.canEdit} />
      : id ? <AgentDetails id={id} {...inventory} />
        : <AgentInventory workflows={inventory.workflows} canEdit={inventory.canEdit} />}
  </section>;
}

function AgentInventory({workflows, canEdit}: Pick<InventoryProps, 'workflows' | 'canEdit'>) {
  const catalog = useAsync(api.agents, []);
  const [search, setSearch] = useState('');
  const workflowNames = new Map(workflows.map(({graph}) => [graph.metadata.id, graph.metadata.name]));
  const query = search.trim().toLowerCase();
  const agents = (catalog.data ?? []).filter(agent =>
    [agent.name, agent.id, agent.description, agent.origin && workflowNames.get(agent.origin.workflow)]
      .filter(Boolean).join(' ').toLowerCase().includes(query))
    .sort((a, b) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id));

  return <>
    <header className="page-head">
      <h1>Agents</h1>
      {canEdit && <a className="btn btn-primary" href="#/agents/new">New agent</a>}
    </header>
    <label className="form agent-catalog-search">Search agents
      <input type="search" value={search} onChange={event => setSearch(event.target.value)} />
    </label>
    {catalog.loading && !catalog.data && <p role="status">Loading agents…</p>}
    <ErrorBox error={catalog.error} />
    {catalog.error && <Btn onClick={() => void catalog.reload()}>Retry loading agents</Btn>}
    {catalog.data && (agents.length ? <ul className="agent-catalog-grid">{agents.map(agent => (
      <li key={agent.id}>
        <a className="card agent-catalog-card" href={`#/agents/${encodeURIComponent(agent.id)}`}>
          <span className="agent-catalog-card-icon"><Icon name="agents" /></span>
          <h2>{agent.name}</h2>
          {agent.description && <p>{agent.description}</p>}
          <span className="agent-catalog-card-meta">Version {agent.latest}</span>
        </a>
      </li>
    ))}</ul> : <p role="status">{query ? 'No agents match your search.' : 'No agents yet.'}</p>)}
  </>;
}

function NewAgent({canEdit}: {canEdit: boolean}) {
  const [published, setPublished] = useState<Agent>();
  return <>
    <a href="#/agents">Back to Agents</a>
    <h1>New agent</h1>
    {published ? <p role="status"><a href={`#/agents/${encodeURIComponent(published.id)}`}>Open {published.versions[0]?.definition.name}</a></p>
      : <DefinitionEditor initial={{name:'', runtime:{kind:'native'}, isolation:{mode:'docker'}}}
          initialId="" canEdit={canEdit} onPublished={setPublished} />}
  </>;
}

function AgentDetails({id, workflows, workflowsLoading, workflowsError, onReloadWorkflows, canEdit}: InventoryProps & {id: string}) {
  const result = useAsync(() => api.agent(id), [id]);
  const [version, setVersion] = useState<number>();
  const latest = result.data?.versions.at(-1);
  const chosen = result.data?.versions.find(value => value.version === version) ?? latest;
  const origin = result.data?.origin;
  const uses = workflows.flatMap(({graph}) => graph.spec.nodes.filter(node =>
    node.agent?.id === id || (!node.agent && origin?.workflow === graph.metadata.id && origin.node === node.id),
  ).map(node => ({node, workflow:graph.metadata.id, name:graph.metadata.name || graph.metadata.id})));

  return <div className="agent-catalog-detail">
    <a href="#/agents">Back to Agents</a>
    <header className="page-head">
      <div>
        <h1>{chosen?.definition.name ?? 'Agent'}</h1>
        {chosen?.definition.description && <p>{chosen.definition.description}</p>}
      </div>
      {latest && chosen && <label className="form">Version
        <select value={chosen.version} onChange={event => setVersion(Number(event.target.value))}>
          {result.data?.versions.map(v => <option key={v.version} value={v.version}>Version {v.version}{v.version === latest.version ? ' (latest)' : ''}</option>)}
        </select>
      </label>}
    </header>
    {result.loading && <p role="status">Loading agent versions…</p>}
    <ErrorBox error={result.error} />
    {result.error && <Btn onClick={() => void result.reload()}>Retry loading versions</Btn>}
    {latest && chosen && <DefinitionEditor key={chosen.version} initial={chosen.definition} initialId={id} currentVersion={latest.version}
      canEdit={canEdit && chosen.version === latest.version} onPublished={agent => {
        result.setData(agent);
        setVersion(agent.versions.at(-1)?.version);
      }} workflowsPanel={<section aria-label="Workflows using this agent">
        {workflowsLoading && !uses.length && <p role="status">Loading workflows…</p>}
        <ErrorBox error={workflowsError} />
        {workflowsError && <Btn onClick={() => void onReloadWorkflows()}>Retry loading workflows</Btn>}
        {uses.length ? <ul className="agent-workflow-list">{uses.map(({node,workflow,name}) => (
          <li key={`${workflow}/${node.id}`}>
            <a href={workflowAgentLink(workflow,node.id)}>{name} / {node.name || node.id}</a>
            <span>{node.agent ? `Version ${node.agent.version}` : 'Origin workflow'}</span>
          </li>
        ))}</ul> : !workflowsLoading && !workflowsError && <p>This agent is not used in a workflow yet.</p>}
      </section>} />}
  </div>;
}

function DefinitionEditor({initial, initialId, currentVersion, canEdit, onPublished, workflowsPanel}: {
  initial: AgentDefinition;
  initialId: string;
  currentVersion?: number;
  canEdit: boolean;
  onPublished: (agent: Agent) => void;
  workflowsPanel?: ReactNode;
}) {
  const [id,setId] = useState(initialId);
  const [definition,setDefinition] = useState(initial);
  const [saving,setSaving] = useState(false);
  const [error,setError] = useState<string>();
  const [section, setSection] = useState<DefinitionSection>('general');
  const sections: DefinitionSection[] = ['general', 'instructions', 'tools', 'parameters'];
  if (workflowsPanel) sections.push('workflows');
  const {readOnly, parameters, ...fields} = definition;
  const node: GraphNode = {...fields,id:'agent-definition',workspace:{readOnly}};
  const summary: GraphSummary = {
    graph:{apiVersion:'weft.io/v1',kind:'Graph',metadata:{id:'agent-definition',name:definition.name},spec:{nodes:[node]}},
    record:{id:'agent-definition',state:'stopped',desiredState:'stopped',version:currentVersion ?? 0,updatedAt:''},
  };
  const updateNode = (_: string, patch: Partial<GraphNode>) => setDefinition(value => ({
    ...value, ...patch, name: patch.name ?? (Object.hasOwn(patch, 'name') ? '' : value.name),
  }));
  return <form className="card form catalog-definition" onSubmit={async event => {
    event.preventDefault();
    if (!canEdit || saving || section === 'workflows') return;
    setError(undefined); setSaving(true);
    try { onPublished(await api.publishAgent(id.trim(),definition,currentVersion)); }
    catch (cause) { setError(errorMessage(cause)); }
    finally { setSaving(false); }
  }}>
    <nav className="section-navigation" aria-label="Agent definition sections">
      {sections.map(item => <button type="button" key={item} aria-current={section === item ? 'page' : undefined} onClick={() => setSection(item)}>
        {definitionSections[item]}
      </button>)}
    </nav>
    {section === 'workflows' ? workflowsPanel : <>
    {section === 'general' && <label>Agent ID<input value={id} onChange={event => setId(event.target.value)}
      readOnly={!!currentVersion || !canEdit} required pattern="[a-z0-9][a-z0-9-]{0,63}" /></label>}
    <fieldset className="catalog-fields" disabled={!canEdit || saving}>
      {(section === 'general' || section === 'tools') && <AgentEditor summary={summary} nodeId={node.id} section={section} onUpdateNode={updateNode} definitionOnly />}
      {section === 'tools' && <>
        <label className="check">
          <input type="checkbox" checked={readOnly ?? false}
            onChange={event => setDefinition({...definition,readOnly:event.target.checked})} />
          <span>Require a read-only workspace</span>
        </label>
        <NodeSettings summary={summary} nodeId={node.id} toolsOnly onUpdateNode={canEdit ? updateNode : undefined} />
      </>}
      {section === 'instructions' && <label>Agent instructions<textarea value={definition.prompt ?? ''} rows={14}
        onChange={event => setDefinition({...definition,prompt:event.target.value})} /></label>}
      {section === 'parameters' && <>
        <p>Use named parameters such as <code>@repository</code> in the instructions. Workflows supply values for each use.</p>
        {(parameters ?? []).map((param,index) => <fieldset className="catalog-parameter" key={index}>
          <legend>Parameter {index + 1}</legend>
          <label>Name<input value={param.name} onChange={event => setDefinition({...definition,parameters:parameters?.map((p,i)=>i===index?{...p,name:event.target.value}:p)})}/></label>
          <label>Description<input value={param.description ?? ''} onChange={event => setDefinition({...definition,parameters:parameters?.map((p,i)=>i===index?{...p,description:event.target.value}:p)})}/></label>
          <label>Default value<input value={param.default ?? ''} onChange={event => setDefinition({...definition,parameters:parameters?.map((p,i)=>i===index?{...p,default:event.target.value}:p)})}/></label>
          <label className="check">
            <input type="checkbox" checked={param.required ?? false}
              onChange={event => setDefinition({...definition,parameters:parameters?.map((p,i)=>i===index?{...p,required:event.target.checked}:p)})} />
            <span>Required</span>
          </label>
          <Btn onClick={() => setDefinition({...definition,parameters:parameters?.filter((_,i)=>i!==index)})}>Remove parameter</Btn>
        </fieldset>)}
        <Btn onClick={() => setDefinition({...definition,parameters:[...(parameters ?? []),{name:''}]})}>Add parameter</Btn>
      </>}
    </fieldset>
    <ErrorBox error={error} />
    {canEdit && <button className="btn btn-primary" type="submit" disabled={saving || !id.trim() || !definition.name.trim()}>
      {saving ? 'Publishing…' : currentVersion ? 'Publish new version' : 'Publish version 1'}
    </button>}
    </>}
  </form>;
}
