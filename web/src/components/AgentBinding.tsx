import { HelpField } from './FieldHelp';
import { useState } from 'react';
import { api } from '../api/client';
import { nodeWithAgent, type AgentDefinition, type AgentBinding } from '../api/agents';
import type { GraphNode, GraphSummary } from '../api/types';
import { useAsync } from '../hooks/useData';
import { Btn, ErrorBox, Modal } from './common';

export function AgentPicker({node, onCancel, onSelect}: {
  node?: GraphNode;
  onCancel: () => void;
  onSelect: (binding: AgentBinding, definition: AgentDefinition) => void;
}) {
  const [id,setId] = useState(node?.agent?.id ?? '');
  const [version,setVersion] = useState('');
  const [values,setValues] = useState<Record<string,string>>({});
  const [search, setSearch] = useState('');
  const catalog = useAsync(api.agents, []);
  const details = useAsync(() => id ? api.agent(id) : Promise.resolve(undefined), [id]);
  const selected = details.data?.id === id ? details.data?.versions.find(v=>v.version===Number(version)) : undefined;
  const definition = selected?.definition;
  const parameters = definition?.parameters ?? [];
  const valid = !!id && !!selected && parameters.every(p => !p.required || (values[p.name] ?? p.default ?? '').trim());
  const isUpdate = !!node?.agent;
  const query = search.trim().toLowerCase();
  const matches = (catalog.data ?? []).filter(agent => agent.id === id ||
    [agent.name, agent.id, agent.description, agent.origin?.workflow].some(value => value?.toLowerCase().includes(query)));
  return <Modal title={isUpdate ? 'Choose an agent version' : 'Select a reusable agent'} onClose={onCancel} keyboard>
    <div className="form">
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
        setValues(Object.fromEntries((next?.definition.parameters ?? []).map(p=>[p.name,
          node?.agent?.id === id ? (node.agent.parameters?.[p.name] ?? p.default ?? '') : (p.default ?? ''),
        ])));
      }}>
        <option value="">Choose a version</option>
        {details.data.versions.map(v=><option key={v.version} value={v.version}>
          Version {v.version}{node?.agent?.id===id && node.agent.version===v.version ? ' (in use)' : ''}
        </option>)}
      </select></HelpField>}
      {definition && <ParameterFields parameters={parameters} values={values} onChange={setValues}/>}
      {definition && <DefinitionChange node={node} definition={definition}/>}
      <div className="btn-row">
        <Btn onClick={onCancel}>Cancel</Btn>
        <Btn tone="primary" disabled={!valid} onClick={()=>{
          if (selected && valid) onSelect({id,version:selected.version,parameters:values},selected.definition);
        }}>{node ? 'Use this version' : 'Add to workflow'}</Btn>
      </div>
    </div>
  </Modal>;
}

function DefinitionChange({node, definition}: {node?: GraphNode; definition: AgentDefinition}) {
  const fields = ['runtime','model','prompt','tools','isolation','skillIds','repositoryGuidance'] as const;
  const changed = fields.filter(key=>JSON.stringify(node?.[key])!==JSON.stringify(definition[key]));
  return <details open>
    <summary>Review definition {node ? 'changes' : 'and access'}</summary>
    {node && <p>{changed.length ? `Updated sections: ${changed.join(', ')}.` : 'The selected definition matches the saved agent settings.'}</p>}
    <p>Runtime: {definition.runtime?.kind ?? 'native'} · Isolation: {definition.isolation?.mode ?? 'process'} · Workspace: {definition.readOnly || node?.workspace?.readOnly ? 'read-only' : 'workflow access'}</p>
    <p>Tool grants in selected version</p>
    <pre>{JSON.stringify(definition.tools ?? [],null,2)}</pre>
    {node && changed.includes('tools') && <details>
      <summary>Previous tool grants</summary><pre>{JSON.stringify(node.tools ?? [],null,2)}</pre>
    </details>}
    {changed.includes('prompt') && <details><summary>Instructions in selected version</summary><pre>{definition.prompt || '(empty)'}</pre></details>}
    <p>Review and save the workflow draft to adopt this version.</p>
  </details>;
}

function ParameterFields({parameters, values, onChange}: {
  parameters: NonNullable<AgentDefinition['parameters']>;
  values: Record<string,string>;
  onChange?: (values: Record<string,string>) => void;
}) {
  return parameters.length > 0 && <fieldset disabled={!onChange}>
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
  const binding = node.agent;
  const details = useAsync(() => binding ? api.agent(binding.id) : Promise.resolve(undefined), [binding?.id]);
  const origin = useAsync(async () => {
    if (binding) return undefined;
    const agents = await api.agents();
    return agents.find(agent => agent.origin?.workflow === summary.graph.metadata.id && agent.origin.node === node.id);
  }, [binding?.id, summary.graph.metadata.id, summary.record.version, node.id]);
  const parameters = details.data?.id === binding?.id
    ? details.data?.versions.find(version => version.version === binding?.version)?.definition.parameters ?? []
    : [];
  const agentId = binding?.id ?? origin.data?.id;
  const agentName = binding ? details.data?.versions.find(version => version.version === binding.version)?.definition.name : origin.data?.name;

  return <section className="card form" aria-label="Agent definition">
    {agentId && <p>
      <a href={`#/agents/${encodeURIComponent(agentId)}`}>{agentName || node.name || node.id} configuration</a>
      {binding ? ` · Version ${binding.version}` : ' · Origin workflow'}
    </p>}
    {binding && <>
      {details.loading && <p role="status">Loading parameter definitions…</p>}
      <ParameterFields parameters={parameters} values={binding.parameters ?? {}}
        onChange={canEdit ? values => onUpdate({...node, agent:{...binding, parameters:values}}) : undefined} />
    </>}
    <ErrorBox error={binding ? details.error : origin.error} />
    {(binding ? details.error : origin.error) &&
      <Btn onClick={() => void (binding ? details.reload() : origin.reload())}>Retry loading definition</Btn>}
    {canEdit && <Btn onClick={() => setPicker(true)}>Choose agent version</Btn>}
    {picker && <AgentPicker node={node} onCancel={() => setPicker(false)} onSelect={(selection, definition) => {
      onUpdate(nodeWithAgent(node, selection, definition));
      setPicker(false);
    }} />}
  </section>;
}
