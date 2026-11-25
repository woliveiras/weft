import { useEffect, useRef, useState } from 'react';
import { parse, parseDocument, stringify } from 'yaml';
import { api, errorMessage } from '../api/client';
import { nodeWithAgent, type AgentBinding, type AgentDefinition } from '../api/agents';
import type { Channel, Graph, GraphNode, GraphSummary, LoopMode } from '../api/types';
import { AgentPickerFields } from './AgentBinding';
import { HelpField } from './FieldHelp';
import { Badge, Btn, ErrorBox, PageHeading } from './common';
import { guidedWorkflow, guidedWorkflowIssues, newWorkflowAgent, proposeWorkflowConnections, workflowSlug,
  type GuidedWorkflowInput, type WorkflowCreationIssue } from './workflowCreation';
import './workflowCreation.css';

const blank: GuidedWorkflowInput = {id:'',name:'',description:'',purpose:'task',nodes:[],channels:[]};
type DraftState = {dirty:boolean;busy:boolean};
type Reviewed = {document:Graph | {yaml:string}; graph:Graph};

/** Guided and advanced drafts stay separate until the operator explicitly replaces a document. */
export function WorkflowCreate({onCreated,onCancel,onDraftState,canCreate=true}: {
  onCreated:(summary:GraphSummary)=>void;
  canCreate?:boolean;
  onCancel:()=>void;
  onDraftState?:(state:DraftState)=>void;
}) {
  const [input,setInput] = useState<GuidedWorkflowInput>(blank);
  const [mode,setMode] = useState<'visual'|'advanced'>('visual');
  const [advanced,setAdvanced] = useState<string>();
  const [picker,setPicker] = useState(false);
  const [newAgent,setNewAgent] = useState(false);
  const [agentName,setAgentName] = useState('');
  const [agentPrompt,setAgentPrompt] = useState('');
  const [error,setError] = useState<string>();
  const [issues,setIssues] = useState<WorkflowCreationIssue[]>([]);
  const [warnings,setWarnings] = useState<string[]>([]);
  const [reviewed,setReviewed] = useState<Reviewed>();
  const [busy,setBusy] = useState<'review'|'save'>();
  const [notice,setNotice] = useState('');
  const [documentLoading,setDocumentLoading] = useState(false);
  const documentGeneration = useRef(0);
  useEffect(()=>()=>{documentGeneration.current++;},[]);
  const lock = useRef(false);
  const completed = useRef(false);
  const root = useRef<HTMLDivElement>(null);
  useEffect(()=>{
    if (reviewed) root.current?.querySelector<HTMLElement>('[data-review-heading]')?.focus();
  },[reviewed]);
  const dirty = !completed.current && (JSON.stringify(input)!==JSON.stringify(blank) || !!advanced || !!agentName || !!agentPrompt);
  useEffect(()=>{onDraftState?.({dirty,busy:!!busy});return()=>onDraftState?.({dirty:false,busy:false});},[dirty,busy,onDraftState]);
  useEffect(()=>{
    if (!dirty && !busy) return;
    const warn = (event:BeforeUnloadEvent)=>{event.preventDefault();event.returnValue='';};
    window.addEventListener('beforeunload',warn);return()=>window.removeEventListener('beforeunload',warn);
  },[dirty,busy]);

  const invalidate = ()=>{documentGeneration.current++;setDocumentLoading(false);setReviewed(undefined);setIssues([]);setWarnings([]);setError(undefined);setNotice('');};
  const change = (next:GuidedWorkflowInput)=>{invalidate();setInput(next);};
  const updateNode = (index:number,patch:Partial<GraphNode>)=>change({...input,nodes:input.nodes.map((node,i)=>i===index?{...node,...patch}:node)});
  const updateChannel = (index:number,patch:Partial<Channel>)=>change({...input,channels:input.channels.map((channel,i)=>i===index?{...channel,...patch}:channel)});
  const cancel = ()=>{
    if (lock.current || (dirty && !window.confirm('Discard this unsaved workflow?'))) return;
    documentGeneration.current++;completed.current=true;onDraftState?.({dirty:false,busy:false});onCancel();
  };
  const selectMode = (next:'visual'|'advanced')=>{
    if (busy || next===mode) return;
    invalidate();
    if (next==='advanced' && advanced===undefined) setAdvanced(stringify(guidedWorkflow(input)));
    setMode(next);
  };
  const addDefinition = (binding:AgentBinding,definition:AgentDefinition)=>{
    try {
      if (mode==='visual') {
        const node = newWorkflowAgent(definition.name,definition.prompt??'',input.nodes,input.purpose);
        change({...input,nodes:[...input.nodes,nodeWithAgent(node,binding,definition)]});
      } else {
        const text = advanced ?? '';
        const json = text.trimStart().startsWith('{');
        const document = json ? undefined : parseDocument(text);
        if (document?.errors.length) throw document.errors[0];
        const source:Graph = document ? document.toJS() : JSON.parse(text);
        if (!source?.spec || !Array.isArray(source.spec.nodes)) throw new Error('Add a spec.nodes list before selecting an agent.');
        const node = newWorkflowAgent(definition.name,definition.prompt??'',source.spec.nodes,'event');
        const linked = nodeWithAgent(node,binding,definition);
        invalidate();
        if (document) {document.addIn(['spec','nodes'],linked);setAdvanced(document.toString());}
        else setAdvanced(JSON.stringify({...source,spec:{...source.spec,nodes:[...source.spec.nodes,linked]}},null,2));
      }
      setPicker(false);
    } catch (cause) {setError(errorMessage(cause));}
  };
  const review = async()=>{
    if (lock.current || documentLoading) return;
    documentGeneration.current++;
    setError(undefined);setWarnings([]);setReviewed(undefined);
    const localIssues = mode==='visual'?guidedWorkflowIssues(input):[];
    setIssues(localIssues);
    if (localIssues.length) {
      setError('Complete the highlighted workflow settings before reviewing.');
      queueMicrotask(()=>root.current?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus());
      return;
    }
    lock.current=true;setBusy('review');
    try {
      const text = (advanced??'').trim();
      const document = mode==='visual'?guidedWorkflow(input):text.startsWith('{')?JSON.parse(text) as Graph:{yaml:text};
      const graph = mode==='visual'?document as Graph:text.startsWith('{')?document as Graph:parse(text) as Graph;
      if (!graph?.spec || !Array.isArray(graph.spec.nodes) || !graph.metadata) throw new Error('The document needs metadata and a spec.nodes list.');
      const result = await api.validateGraph(document);
      setWarnings((result.warnings??[]).map(issue=>`${issue.path}: ${issue.message}`));
      if (!result.valid) {setError((result.errors??[]).map(issue=>`${issue.path}: ${issue.message}`).join('\n') || 'Workflow validation failed. Correct the definition and review again.');return;}
      setReviewed({document,graph});
    } catch (cause) {setError(errorMessage(cause));}
    finally {lock.current=false;setBusy(undefined);}
  };
  const save = async()=>{
    if (!reviewed || lock.current) return;
    lock.current=true;setBusy('save');setError(undefined);
    try {
      const saved = await api.createGraph(reviewed.document);
      completed.current=true;
      onDraftState?.({dirty:false,busy:false});
      onCreated(saved);
    } catch (cause) {setError(errorMessage(cause));}
    finally {lock.current=false;setBusy(undefined);}
  };
  const fieldIssue = (field:string)=>issues.find(issue=>issue.field===field)?.message;
  const feedback = (field:string)=>fieldIssue(field)?<small className="field-error" id={`workflow-${field}-error`}>{fieldIssue(field)}</small>:null;
  const invalid = (field:string)=>({'aria-invalid':!!fieldIssue(field),'aria-describedby':fieldIssue(field)?`workflow-${field}-error`:undefined});

  if (!canCreate) return <div className="page workflow-create"><PageHeading title="New workflow" description="An administrator must create workflow definitions."/><Btn onClick={onCancel}>Back to workflows</Btn></div>;

  return <div ref={root} className="page workflow-create">
    <PageHeading title="New workflow" description="Define reusable work, review its agents and connections, then save without starting it.">
      <Btn disabled={!!busy} onClick={cancel}>Cancel</Btn>
    </PageHeading>
    <nav className="section-navigation" aria-label="Workflow creation mode">
      <Btn disabled={!!busy} aria-pressed={mode==='visual'} onClick={()=>selectMode('visual')}>Visual builder</Btn>
      <Btn disabled={!!busy} aria-pressed={mode==='advanced'} onClick={()=>selectMode('advanced')}>Advanced YAML / JSON</Btn>
    </nav>
    {picker ? <section className="card form" aria-label="Agent selection">
      <h2>Add an existing agent</h2>
      <AgentPickerFields onCancel={()=>setPicker(false)} onSelect={addDefinition}/>
    </section>:null}
    {!picker && !reviewed && <fieldset className="workflow-create-fields" disabled={!!busy}>
      {mode==='visual'?<>
        <section className="card form" aria-label="Workflow identity">
          <h2>1. Describe the workflow</h2>
          <div className="workflow-create-grid">
            <label>Workflow name<input autoFocus value={input.name} {...invalid('name')} onChange={event=>change({...input,name:event.target.value,id:!input.id || input.id===workflowSlug(input.name)?workflowSlug(event.target.value):input.id})}/>{feedback('name')}</label>
            <HelpField label="Workflow ID" help="A permanent identifier used by links, task templates and saved history. Use lowercase letters, digits and hyphens.">
              <input value={input.id} {...invalid('id')} onChange={event=>change({...input,id:event.target.value})}/>{feedback('id')}
            </HelpField>
            <label className="workflow-create-wide">Description<textarea rows={3} value={input.description} onChange={event=>change({...input,description:event.target.value})}/></label>
            <HelpField label="Purpose" help="Task workflows run a reviewed request in a separate task copy. Event workflows wait for messages. Continuous workflows repeat when started explicitly. Choosing a purpose never starts work.">
              <select value={input.purpose} onChange={event=>change({...input,purpose:event.target.value as GuidedWorkflowInput['purpose']})}>
                <option value="task">Run a task</option><option value="event">React to events</option><option value="continuous">Run continuously</option>
              </select>
            </HelpField>
          </div>
        </section>
        <section className="card form" aria-label="Workflow agents">
          <h2>2. Choose agents</h2>
          <p className="muted">Reuse a published definition or create a new agent. The first enabled agent receives tasks; the last enabled agent supplies the result. Review order and run modes before saving.</p>
          {feedback('nodes')}
          {input.nodes.length===0 && <p>No agents added yet.</p>}
          <div className="workflow-create-agents">
            {input.nodes.map((node,index)=><article className="workflow-create-agent" key={index} aria-label={`Agent ${index+1}`}>
              <header><strong>{index+1}. {node.name || node.id}</strong><Badge>{node.agent?`Version ${node.agent.version}`:'New agent'}</Badge></header>
              <div className="workflow-create-grid">
                <label>Agent {index+1} ID<input value={node.id} {...invalid(`nodes.${index}.id`)} onChange={event=>updateNode(index,{id:event.target.value})}/>{feedback(`nodes.${index}.id`)}</label>
                <label>Agent {index+1} run mode<select value={node.loop?.mode??'once'} {...invalid(`nodes.${index}.mode`)} onChange={event=>updateNode(index,{loop:{mode:event.target.value as LoopMode}})}>
                  <option value="once">Once</option><option value="event">Event</option><option value="continuous">Continuous</option>
                </select>{feedback(`nodes.${index}.mode`)}</label>
                {node.agent?<p className="muted workflow-create-wide">Shared definition {node.agent.id}. Instructions, runtime and grants come from the selected version; this workflow owns its run mode.</p>:<label className="workflow-create-wide">Agent {index+1} instructions<textarea rows={3} value={node.prompt??''} {...invalid(`nodes.${index}.prompt`)} onChange={event=>updateNode(index,{prompt:event.target.value})}/>{feedback(`nodes.${index}.prompt`)}</label>}
              </div>
              <div className="btn-row">
                <Btn small disabled={index===0} onClick={()=>{const nodes=[...input.nodes];[nodes[index-1],nodes[index]]=[nodes[index],nodes[index-1]];change({...input,nodes});}}>Move up<span className="sr-only"> {node.name || node.id}</span></Btn>
                <Btn small disabled={index===input.nodes.length-1} onClick={()=>{const nodes=[...input.nodes];[nodes[index+1],nodes[index]]=[nodes[index],nodes[index+1]];change({...input,nodes});}}>Move down<span className="sr-only"> {node.name || node.id}</span></Btn>
                <Btn small tone="danger" onClick={()=>change({...input,nodes:input.nodes.filter((_,i)=>i!==index),channels:input.channels.filter(channel=>channel.from!==node.id&&!channel.to?.includes(node.id))})}>Remove<span className="sr-only"> {node.name || node.id}</span></Btn>
              </div>
            </article>)}
          </div>
          {!newAgent?<div className="btn-row"><Btn onClick={()=>setPicker(true)}>Add existing agent</Btn><Btn onClick={()=>setNewAgent(true)}>Create new agent</Btn></div>:<div className="workflow-create-new-agent">
            <label>New agent name<input value={agentName} onChange={event=>setAgentName(event.target.value)}/></label>
            <label>New agent instructions<textarea rows={4} value={agentPrompt} onChange={event=>setAgentPrompt(event.target.value)}/></label>
            <p className="muted small">New native agents use Docker isolation and receive no tools. Set provider, environment and permissions in agent configuration after saving. Saving makes the agent available in the catalogue.</p>
            <div className="btn-row"><Btn tone="primary" disabled={!agentName.trim()||!agentPrompt.trim()} onClick={()=>{
              change({...input,nodes:[...input.nodes,newWorkflowAgent(agentName,agentPrompt,input.nodes,input.purpose)]});setAgentName('');setAgentPrompt('');setNewAgent(false);
            }}>Add new agent</Btn><Btn onClick={()=>setNewAgent(false)}>Cancel new agent</Btn></div>
          </div>}
        </section>
        <section className="card form" aria-label="Workflow connections">
          <h2>3. Review connections</h2>
          <p className="muted">Propose an external input and sequential handoffs, or define your own connections. Review the direction before saving. Native agents must publish results to their outgoing channels.</p>
          <div className="btn-row"><Btn disabled={!input.nodes.length} onClick={()=>{
            if (input.channels.length && !window.confirm('Replace the current connections with new proposals based on agent order?')) return;
            change({...input,channels:proposeWorkflowConnections(input.nodes,input.purpose)});setNotice('Connections proposed. Review or correct each source and receiver below.');
          }}>Propose connections</Btn><Btn disabled={!input.nodes.length} onClick={()=>change({...input,channels:[...input.channels,{id:`connection-${input.channels.length+1}`,type:'queue',to:[]}]})}>Add connection</Btn></div>
          {!input.channels.length && <p className="muted">No connections. Tasks can send direct instructions to the first agent; event agents need an input to receive messages.</p>}
          {input.channels.map((channel,index)=><div className="workflow-create-connection" key={index}>
            <label>Connection {index+1} ID<input value={channel.id} {...invalid(`channels.${index}.id`)} onChange={event=>updateChannel(index,{id:event.target.value})}/>{feedback(`channels.${index}.id`)}</label>
            <label>Connection {index+1} source<select value={channel.from??''} {...invalid(`channels.${index}.from`)} onChange={event=>updateChannel(index,{from:event.target.value || undefined})}>
              <option value="">External input</option>{channel.from && !input.nodes.some(node=>node.id===channel.from) && <option value={channel.from}>{channel.from} (unavailable)</option>}{input.nodes.map(node=><option key={node.id} value={node.id}>{node.name||node.id}</option>)}
            </select>{feedback(`channels.${index}.from`)}</label>
            <label>Connection {index+1} receiver<select value={channel.to?.[0]??''} {...invalid(`channels.${index}.to`)} onChange={event=>updateChannel(index,{to:event.target.value?[event.target.value]:[]})}>
              <option value="">Choose an agent</option>{channel.to?.[0] && !input.nodes.some(node=>node.id===channel.to![0]) && <option value={channel.to[0]}>{channel.to[0]} (unavailable)</option>}{input.nodes.map(node=><option key={node.id} value={node.id}>{node.name||node.id}</option>)}
            </select>{feedback(`channels.${index}.to`)}</label>
            <Btn tone="danger" onClick={()=>change({...input,channels:input.channels.filter((_,i)=>i!==index)})}>Remove connection {index+1}</Btn>
          </div>)}
          <p className="muted small">Queue channels are used here. Advanced YAML / JSON supports other routing and message contracts.</p>
        </section>
      </>:<section className="card form" aria-label="Advanced workflow definition">
        <h2>Workflow document</h2>
        <p className="muted">The visual draft is retained separately. Only this document will be reviewed and saved while Advanced YAML / JSON is selected.</p>
        <label>Upload a workflow document<input type="file" accept=".yaml,.yml,.json" onChange={async event=>{
          const file=event.target.files?.[0];if(!file)return;
          const generation=++documentGeneration.current;setDocumentLoading(true);
          try {
            const text=await file.text();
            if (generation!==documentGeneration.current) return;
            setAdvanced(text);invalidate();
          } catch(cause) {
            if(generation===documentGeneration.current){setDocumentLoading(false);setError(errorMessage(cause));}
          }
        }}/></label>
        <HelpField label="Workflow document" help="Define the workflow in YAML or JSON. The document includes agents, channels and optional defaults. Review checks the complete definition before it is saved.">
          <textarea className="code-editor mono" rows={20} spellCheck={false} value={advanced??''} onChange={event=>{setAdvanced(event.target.value);invalidate();}}/>
        </HelpField>
        <div className="btn-row"><Btn onClick={()=>setPicker(true)}>Add existing agent</Btn><Btn onClick={()=>{
          if (advanced?.trim() && !window.confirm('Replace the advanced document with the current visual draft?')) return;
          setAdvanced(stringify(guidedWorkflow(input)));invalidate();
        }}>Replace with visual draft</Btn></div>
      </section>}
    </fieldset>}
    {reviewed && <section className="card form" aria-label="Workflow review">
      <h2 tabIndex={-1} data-review-heading>Review before saving</h2>
      <p><strong>{reviewed.graph.metadata.name || reviewed.graph.metadata.id}</strong> · {reviewed.graph.metadata.id}</p>
      <p className="muted">Versioned definition only. Saving opens configuration; it does not start agents, build an environment or grant consent. Existing catalogue grants stay explicit in each selected version.</p>
      <div className="workflow-create-review-list" role="region" aria-label="Agents and access to save" tabIndex={0}>
        {reviewed.graph.spec.nodes.map((node,index)=><article key={index}><strong>{node.name||node.id}</strong>
          <p>{node.agent?`${node.agent.id} · Version ${node.agent.version}`:'New inline agent'} · {node.loop?.mode??'server default'} · {node.runtime?.kind??'native'} · {node.isolation?.mode??reviewed.graph.spec.defaults?.isolation?.mode??'process'} isolation</p>
          <details><summary>Instructions, parameters and grants for {node.name||node.id}</summary><pre>{JSON.stringify({instructions:node.prompt,parameters:node.agent?.parameters,tools:node.tools??[],defaults:reviewed.graph.spec.defaults},null,2)}</pre></details>
        </article>)}
      </div>
      <h3>Connections</h3>
      {!reviewed.graph.spec.channels?.length?<p>No channels.</p>:<ul>{reviewed.graph.spec.channels.map((channel,index)=><li key={index}><code>{channel.id}</code>: {channel.from||'External input'} → {(channel.to??[]).join(', ') || channel.topic || 'No explicit receiver'} · {channel.type??'queue'}{channel.types?.length?` · ${channel.types.join(', ')}`:''}</li>)}</ul>}
      {reviewed.graph.spec.task && <p>Task defaults: entry {reviewed.graph.spec.task.nodeId}, result {reviewed.graph.spec.task.resultNodeId}{reviewed.graph.spec.task.channelId?`, input ${reviewed.graph.spec.task.channelId} (${reviewed.graph.spec.task.messageType})`:`, direct instruction`}. Repository, model, environment and access are reviewed when a task is created.</p>}
      <div className="btn-row"><Btn disabled={!!busy} onClick={()=>setReviewed(undefined)}>Edit definition</Btn><Btn tone="primary" disabled={!!busy} onClick={()=>void save()}>{busy==='save'?'Saving workflow…':'Save workflow'}</Btn></div>
    </section>}
    <ErrorBox error={error}/>
    {warnings.length>0 && <aside className="card" aria-label="Workflow validation warnings"><h3>Review warnings</h3><ul>{warnings.map((warning,index)=><li key={index}>{warning}</li>)}</ul></aside>}
    <p role="status">{documentLoading?'Reading workflow document…':busy==='review'?'Checking the complete workflow definition…':notice}</p>
    {!picker && !reviewed && <div className="btn-row"><Btn tone="primary" disabled={!!busy||documentLoading} onClick={()=>void review()}>{busy==='review'?'Reviewing workflow…':'Review workflow'}</Btn></div>}
  </div>;
}
