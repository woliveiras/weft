import './agent-journeys.css';
import { HelpField } from './FieldHelp';
import { useEffect, useState } from 'react';
import { api } from '../api/client';
import { nodeWithAgent, nodeWithAgentVersion, type AgentDefinition, type AgentBinding } from '../api/agents';
import type { GraphNode, GraphSummary } from '../api/types';
import { useAsync } from '../hooks/useData';
import { Btn, ErrorBox, Modal } from './common';
import { agentDefinitionSectionsChanged } from './agentJourneyModel';

type AgentPickerProps = {
  node?: GraphNode;
  onCancel: () => void;
  onSelect: (binding: AgentBinding, definition: AgentDefinition) => void;
};

export function AgentPicker(props: AgentPickerProps) {
  return <Modal title="Select a reusable agent" onClose={props.onCancel} keyboard>
    <AgentPickerFields {...props} />
  </Modal>;
}

export function AgentPickerFields({node, onCancel, onSelect}: AgentPickerProps) {
  const [id,setId] = useState('');
  const [version,setVersion] = useState('');
  const [values,setValues] = useState<Record<string,string>>({});
  const [search, setSearch] = useState('');
  const catalog = useAsync(api.agents, []);
  const details = useAsync(() => id ? api.agent(id) : Promise.resolve(undefined), [id]);
  const selected = details.data?.id === id ? details.data?.versions.find(v=>v.version===Number(version)) : undefined;
  const definition = selected?.definition;
  const parameters = definition?.parameters ?? [];
  const valid = !!id && !!selected && parameters.every(p => !p.required || (values[p.name] ?? p.default ?? '').trim());
  const query = search.trim().toLowerCase();
  const matches = (catalog.data ?? []).filter(agent => agent.id === id ||
    [agent.name, agent.id, agent.description, agent.origin?.workflow].some(value => value?.toLowerCase().includes(query)));
  return <div className="form">
      <label>Search agents<input autoFocus type="search" value={search} onChange={event => setSearch(event.target.value)} /></label>
      <HelpField label={"Reusable agent"} help={"Choose a catalog definition to reuse for this node. Review its instructions and grants before saving the workflow."}><select value={id} disabled={catalog.loading && !catalog.data} onChange={e=>{
        setId(e.target.value); setVersion(''); setValues({});
      }}>
        <option value="">Select an agent</option>
        {matches.map(a=><option key={a.id} value={a.id}>{a.name}{a.origin ? ` · ${a.origin.workflow}` : ` · ${a.id}`}</option>)}
      </select></HelpField>
      {catalog.data?.length===0 && <p>No reusable agents yet. Create one in <a href="#/agents">Agents</a>.</p>}
      {!!catalog.data?.length && !matches.length && <p role="status">No agents match your search.</p>}
      {(catalog.loading || (id && details.loading)) && <p role="status">Loading agent versions…</p>}
      <ErrorBox error={catalog.error ?? details.error}/>
      {catalog.error && <Btn onClick={()=>void catalog.reload()}>Retry loading agents</Btn>}
      {details.error && <Btn onClick={()=>void details.reload()}>Retry loading versions</Btn>}
      {id && details.data?.id === id && <HelpField label={"Agent version"} help={"Pins the node to one published definition. Future versions become available to select without changing the version already used by this workflow."}><select value={version} onChange={e=>{
        setVersion(e.target.value);
        const next = details.data?.versions.find(v=>v.version===Number(e.target.value));
        setValues(Object.fromEntries((next?.definition.parameters ?? []).map(p=>[p.name, p.default ?? ''])));
      }}>
        <option value="">Choose a version</option>
        {details.data.versions.map(v=><option key={v.version} value={v.version}>Version {v.version}</option>)}
      </select></HelpField>}
      {definition && <ParameterFields parameters={parameters} values={values} onChange={setValues}/>}
      {definition && <DefinitionChange node={node} definition={definition}/>}
      <div className="btn-row">
        <Btn onClick={onCancel}>Cancel</Btn>
        <Btn tone="primary" disabled={!valid} onClick={()=>{
          if (selected && valid) onSelect({id,version:selected.version,parameters:values},selected.definition);
        }}>{node ? 'Use this version' : 'Add to workflow'}</Btn>
      </div>
  </div>;
}

function DefinitionChange({node, definition, previous}: {node?: GraphNode; definition: AgentDefinition; previous?:AgentDefinition}) {
  const fields = ['name','description','runtime','model','prompt','tools','isolation','skillIds','repositoryGuidance','readOnly','parameters'] as const;
  const before = previous ?? (node && {
    name:node.name ?? node.id, description:node.description, runtime:node.runtime, model:node.model,
    prompt:node.prompt,tools:node.tools,isolation:node.isolation,skillIds:node.skillIds,
    repositoryGuidance:node.repositoryGuidance,readOnly:node.workspace?.readOnly,
  });
  const changed = before ? agentDefinitionSectionsChanged(before, definition) : [...fields];
  return <details open>
    <summary>Review definition {node ? 'changes' : 'and access'}</summary>
    {node && <p>{changed.length ? `Updated sections: ${changed.join(', ')}.` : 'The selected definition matches the saved agent settings.'}</p>}
    <p>Runtime: {definition.runtime?.kind ?? 'native'} · Isolation: {definition.isolation?.mode ?? 'process'} · Workspace: {definition.readOnly || node?.workspace?.readOnly ? 'read-only' : 'workflow access'}</p>
    <p>Tool grants in selected version</p>
    <pre>{JSON.stringify(definition.tools ?? [],null,2)}</pre>
    {node && changed.includes('tools') && <details>
      <summary>Previous tool grants</summary><pre>{JSON.stringify(before?.tools ?? [],null,2)}</pre>
    </details>}
    {changed.filter(field=>field!=='tools').map(field=><details key={field}>
      <summary>{field} · proposed change</summary>
      {before && <><p>Current published value</p><pre>{JSON.stringify(before[field as keyof AgentDefinition] ?? null,null,2)}</pre></>}
      <p>Selected version</p><pre>{JSON.stringify(definition[field as keyof AgentDefinition] ?? null,null,2)}</pre>
    </details>)}
    <p>Workflow node identity, local instructions, compatible parameter values and instance execution settings are retained. Review and save the workflow draft to adopt this version.</p>
  </details>;
}

function ParameterFields({parameters, values, onChange}: {
  parameters: NonNullable<AgentDefinition['parameters']>;
  values: Record<string,string>;
  onChange?: (values: Record<string,string>) => void;
}) {
  return parameters.length > 0 && <fieldset className="workflow-parameters" disabled={!onChange}>
    <legend>Workflow parameters</legend>
    {parameters.map(p=><HelpField key={p.name} label={p.name + (p.required ? ' (required)' : '')}
      help={p.description || `Supplies @${p.name} for this node. Use the value the shared instructions expect for this workflow.`}>
      <input value={values[p.name] ?? p.default ?? ''} onChange={event=>onChange?.({...values,[p.name]:event.target.value})}/>
      {p.description && <small className="muted">{p.description}</small>}
    </HelpField>)}
  </fieldset>;
}

export function AgentInstance({node, summary, canEdit, onUpdate}: {
  node: GraphNode;
  summary: GraphSummary;
  canEdit: boolean;
  onUpdate: (node: GraphNode) => void;
}) {
  const [picker, setPicker] = useState(false);
  const [pendingVersion, setPendingVersion] = useState<number>();
  const binding = node.agent;
  const details = useAsync(() => binding ? api.agent(binding.id) : Promise.resolve(undefined), [binding?.id]);
  const origin = useAsync(async () => {
    if (binding) return undefined;
    const agents = await api.agents();
    return agents.find(agent => agent.origin?.workflow === summary.graph.metadata.id && agent.origin.node === node.id);
  }, [binding?.id, summary.graph.metadata.id, summary.record.version, node.id]);
  const agent = binding && details.data?.id === binding.id ? details.data : undefined;
  const current = agent?.versions.find(version => version.version === binding?.version);
  const parameters = current?.definition.parameters ?? [];
  const proposed = pendingVersion !== binding?.version ? agent?.versions.find(value=>value.version===pendingVersion) : undefined;
  useEffect(()=>setPendingVersion(undefined),[binding?.id,binding?.version,node.id]);
  const agentId = binding?.id ?? origin.data?.id;
  const agentName = binding ? current?.definition.name : origin.data?.name;

  return <section className="card form" aria-label="Agent definition">
    {agentId && <p>
      <a href={`#/agents/${encodeURIComponent(agentId)}`}>{agentName || node.name || node.id} configuration</a>
      {!binding && ' · Origin workflow'}
    </p>}
    {binding ? <p role="note">Shared definition · Version {binding.version} pinned in this workflow. Publishing elsewhere does not update this use.</p>
      : <p role="note">Workflow-owned definition. Saving registers a reusable snapshot in Agents; later workflow edits do not publish shared versions. Link an agent to use a pinned shared definition.</p>}
    {binding && <>
      <HelpField label={"Agent version"} help={"Choose a published version of this agent. The workflow keeps using its saved version until you review and save the draft."}>
        <select value={proposed?.version ?? binding.version} disabled={!canEdit || details.loading || !!details.error || !agent?.versions.some(version => version.version !== binding.version)}
          onChange={event => {
            if (!canEdit || !agent) return;
            const version=Number(event.target.value);
            setPendingVersion(agent.versions.some(value=>value.version===version) ? version : undefined);
          }}>
          {!current && <option value={binding.version}>Version {binding.version}</option>}
          {agent?.versions.map(version => <option key={version.version} value={version.version}>Version {version.version}</option>)}
        </select>
      </HelpField>
      {details.loading && <p role="status">Loading agent versions…</p>}
      {proposed && <section className="agent-version-review" aria-label="Version adoption review">
        <h3>Review version {binding.version} → {proposed.version}</h3>
        <DefinitionChange node={node} definition={proposed.definition} previous={current?.definition}/>
        <div className="btn-row">
          <Btn onClick={()=>setPendingVersion(undefined)}>Cancel version change</Btn>
          <Btn tone="primary" disabled={!canEdit} onClick={()=>{
            if (!canEdit || !agent) return;
            const updated=nodeWithAgentVersion(node,agent,proposed.version);
            if (updated!==node) onUpdate(updated);
            setPendingVersion(undefined);
          }}>Use this version</Btn>
        </div>
      </section>}
      <h3>This workflow</h3><p className="muted small">Parameters and local instructions affect only {summary.graph.metadata.name || summary.graph.metadata.id} / {node.name || node.id}. Save them with this workflow draft.</p>
      <ParameterFields parameters={parameters} values={binding.parameters ?? {}}
        onChange={canEdit ? values => onUpdate({...node, agent:{...binding, parameters:values}}) : undefined} />
    </>}
    <ErrorBox error={binding ? details.error : origin.error} />
    {(binding ? details.error : origin.error) &&
      <Btn onClick={() => void (binding ? details.reload() : origin.reload())}>Retry loading definition</Btn>}
    {canEdit && !binding && <Btn onClick={() => setPicker(true)}>Link an agent</Btn>}
    {picker && !binding && <AgentPicker node={node} onCancel={() => setPicker(false)} onSelect={(selection, definition) => {
      onUpdate(nodeWithAgent(node, selection, definition));
      setPicker(false);
    }} />}
  </section>;
}
