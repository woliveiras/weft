import { FieldHelp, HelpField } from './FieldHelp';
import { useState, type ReactNode } from 'react';
import { api, errorMessage } from '../api/client';
import { workflowAgentLink, type Agent, type AgentDefinition } from '../api/agents';
import type { GraphNode, GraphSummary } from '../api/types';
import { useAsync } from '../hooks/useData';
import { AgentEditor, NodeSettings } from './NodePanel';
import { agentDefinitionChanged } from './agentSettingsModel';
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
      {latest && chosen && <HelpField className="form" label={"Version"} help={"Choose a published definition to inspect. Save changes as a new version, then select it in each workflow that should adopt it."}>
        <select value={chosen.version} onChange={event => setVersion(Number(event.target.value))}>
          {result.data?.versions.map(v => <option key={v.version} value={v.version}>Version {v.version}{v.version === latest.version ? ' (latest)' : ''}</option>)}
        </select>
      </HelpField>}
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
  const agentId = id.trim();
  const dirty = agentId !== initialId || agentDefinitionChanged(initial, definition);
  const canPublish = canEdit && dirty && !saving && /^[a-z0-9][a-z0-9-]{0,63}$/.test(agentId) && !!definition.name.trim();
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
    if (!canPublish) return;
    setError(undefined); setSaving(true);
    try { onPublished(await api.publishAgent(agentId,definition,currentVersion)); }
    catch (cause) { setError(errorMessage(cause)); }
    finally { setSaving(false); }
  }}>
    <div className="catalog-definition-toolbar">
      <nav className="section-navigation" aria-label="Agent definition sections">
        {sections.map(item => <button type="button" key={item} aria-current={section === item ? 'page' : undefined} onClick={() => setSection(item)}>
          {definitionSections[item]}
        </button>)}
      </nav>
      {canEdit && <div className="catalog-definition-actions">
        <span className="muted small" role="status">{saving ? 'Publishing…' : dirty ? 'Unpublished changes' : ''}</span>
        <Btn type="submit" tone="primary" disabled={!canPublish}>Publish</Btn>
      </div>}
    </div>
    <ErrorBox error={error} />
    {section === 'workflows' ? workflowsPanel : <>
    {section === 'general' && <HelpField label={"Agent ID"} help={"A stable catalog identifier used by workflow bindings. Use lowercase letters, digits and hyphens. The ID is fixed after publication."}><input value={id} onChange={event => setId(event.target.value)}
      readOnly={!!currentVersion || !canEdit} required pattern={'[a-z0-9][a-z0-9\\-]{0,63}'} /></HelpField>}
    <fieldset className="catalog-fields" disabled={!canEdit || saving}>
      {(section === 'general' || section === 'tools') && <AgentEditor summary={summary} nodeId={node.id} section={section} onUpdateNode={updateNode} definitionOnly />}
      {section === 'tools' && <>
        <HelpField className="check" label="Require a read-only workspace" help="Requires a read-only workspace in workflows that use this version. Use it for agents that inspect or review files. Tool grants and approval rules still apply.">
          <input type="checkbox" checked={readOnly ?? false}
            onChange={event => setDefinition({...definition,readOnly:event.target.checked})} />
        </HelpField>
        <NodeSettings summary={summary} nodeId={node.id} toolsOnly onUpdateNode={canEdit ? updateNode : undefined} />
      </>}
      {section === 'instructions' && <HelpField label={"Agent instructions"} help={"Describe the agent\u2019s shared responsibilities, inputs, expected output and success criteria. For reusable instructions, declare values in Parameters and refer to them as @name, such as @repository."}><textarea value={definition.prompt ?? ''} rows={14}
        onChange={event => setDefinition({...definition,prompt:event.target.value})} /></HelpField>}
      {section === 'parameters' && <>
        <div className="field-help-caption"><h3>Parameters</h3><FieldHelp label="Parameters">Declare the values a reusable agent accepts. Workflows set values for each use. Reference a parameter in the instructions using @ followed by its name.</FieldHelp></div>
        <p>Use named parameters such as <code>@repository</code> in the instructions. Workflows supply values for each use.</p>
        {(parameters ?? []).map((param,index) => <fieldset className="catalog-parameter" key={index}>
          <legend>Parameter {index + 1}</legend>
          <HelpField label={"Name"} help={"Use an identifier you can reference in instructions, such as repository for @repository. Use the same spelling in the instructions and when supplying the workflow value."}><input value={param.name} onChange={event => setDefinition({...definition,parameters:parameters?.map((p,i)=>i===index?{...p,name:event.target.value}:p)})}/></HelpField>
          <HelpField label={"Description"} help={"Summarize the parameter so workflow authors know what value to enter and in what format. For example, specify owner/repository for a GitHub repository."}><input value={param.description ?? ''} onChange={event => setDefinition({...definition,parameters:parameters?.map((p,i)=>i===index?{...p,description:event.target.value}:p)})}/></HelpField>
          <HelpField label={"Default value"} help={"Used if the workflow omits the parameter. Leave it empty when every workflow should supply its own value."}><input value={param.default ?? ''} onChange={event => setDefinition({...definition,parameters:parameters?.map((p,i)=>i===index?{...p,default:event.target.value}:p)})}/></HelpField>
          <HelpField className="check" label="Required" help="Requires a nonblank value when this version is used in a workflow. A nonblank default satisfies the requirement when the workflow omits a value.">
          <input type="checkbox" checked={param.required ?? false}
              onChange={event => setDefinition({...definition,parameters:parameters?.map((p,i)=>i===index?{...p,required:event.target.checked}:p)})} />
        </HelpField>
          <Btn onClick={() => setDefinition({...definition,parameters:parameters?.filter((_,i)=>i!==index)})}>Remove parameter</Btn>
        </fieldset>)}
        <Btn onClick={() => setDefinition({...definition,parameters:[...(parameters ?? []),{name:''}]})}>Add parameter</Btn>
      </>}
    </fieldset>
    </>}
  </form>;
}
