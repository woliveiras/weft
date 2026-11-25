import {useEffect,useRef,useState} from 'react';
import {api,errorMessage} from '../api/client';
import type {Skill,InstructionSnapshot,RepositoryGuidancePolicy,RepositorySource} from '../api/instructions';
import {repositorySources} from '../api/instructions';
import type {Graph,Execution} from '../api/types';
import {useAsync} from '../hooks/useData';
import {Btn,ErrorBox,Modal} from './common';
import {associationChanges} from './agentJourneyModel';
import {HelpField} from './FieldHelp';
import {InstructionManifest} from './InstructionManifest';

export function SkillAssociations({skills,skillsUnavailable,role,mode,workflowId,agentId,executionId,onSelection,onDirty,onBusy,onChanged}:{
 skills:Skill[];skillsUnavailable:boolean;role?:string;mode:'associations'|'inspect';workflowId?:string;agentId?:string;executionId?:string;
 onSelection:(workflow?:string,agent?:string,execution?:string)=>void;onDirty:(dirty:boolean)=>void;onBusy:(busy:boolean)=>void;onChanged:()=>void;
}) {
 const graphs=useAsync(api.listGraphs,[]);
 const [graph,setGraph]=useState<Graph>();
 const [node,setNode]=useState(agentId??'');
 const [snapshot,setSnapshot]=useState<InstructionSnapshot>();
 const [executions,setExecutions]=useState<Execution[]>();
 const [runSearch,setRunSearch]=useState('');const [runPage,setRunPage]=useState(0);
 const [snapshotAttempt,setSnapshotAttempt]=useState(0);
 const [error,setError]=useState<string>();const [notice,setNotice]=useState<string>();const [busy,setBusy]=useState(false);
 const [baseline,setBaseline]=useState('');
 const [reviewedGraph,setReviewedGraph]=useState<Graph>();
 const reviewTrigger=useRef<HTMLButtonElement>(null);
 const closeReview=()=>{if(busy)return;setReviewedGraph(undefined);reviewTrigger.current?.focus();};
 const dirty=!!graph&&JSON.stringify(graph)!==baseline;
 const edit=role==='admin'&&!skillsUnavailable&&!graphs.error&&!graphs.loading;
 const available=skills;
 const graphSelection=useRef('');
 const requestGeneration=useRef(0);
 useEffect(()=>{onDirty(dirty);return()=>onDirty(false);},[dirty,onDirty]);
 useEffect(()=>{onBusy(busy);return()=>onBusy(false);},[busy,onBusy]);
 useEffect(()=>{
  if(!workflowId||!graphs.data)return;
  if(graphSelection.current===workflowId){if(agentId!==undefined){setNode(agentId);setSnapshot(undefined);}return;}
  const selected=graphs.data.find(s=>s.graph.metadata.id===workflowId)?.graph;
  graphSelection.current=selected?workflowId:'';setGraph(selected);setBaseline(JSON.stringify(selected));setNode(agentId??selected?.spec.nodes[0]?.id??'');
  setSnapshot(undefined);setExecutions(undefined);setError(undefined);
 },[workflowId,agentId,graphs.data]);
 async function run(fn:()=>Promise<void>) {setBusy(true);setError(undefined);setNotice(undefined);try{await fn();}catch(e){setError(errorMessage(e));}finally{setBusy(false);}}
 function updateGraph(next:Graph){setGraph(next);setSnapshot(undefined);setNotice(undefined);setReviewedGraph(undefined);}
 const refs=graph?.spec.instructions??{};
 const repositoryEnabled=!!refs.repositoryGuidance&&!refs.repositoryGuidance.disabled;
 const savedInstruction=async()=>{
  if(!graph||!node)return;
  setSnapshot(undefined);onSelection(graph.metadata.id,node);await run(async()=>setSnapshot(await api.instructions(graph.metadata.id,node)));
 };
 useEffect(()=>{
  if(mode!=='inspect'||!executionId||!graph)return;
  const generation=++requestGeneration.current;setSnapshot(undefined);setBusy(true);setError(undefined);
  void api.executionInstructions(executionId).then(value=>{
    if(generation!==requestGeneration.current)return;
    if(value.graphId!==graph.metadata.id || (node&&value.nodeId!==node)){setError('This execution does not belong to the selected workflow and agent.');return;}
    setSnapshot(value);
  },e=>{if(generation===requestGeneration.current)setError(errorMessage(e));}).finally(()=>{if(generation===requestGeneration.current)setBusy(false);});
  return()=>{requestGeneration.current++;};
 },[executionId,mode,graph?.metadata.id,node,snapshotAttempt]);
 const runs=(executions??[]).filter(value=>(!node||value.nodeId===node)&&`${value.id} ${value.nodeId} ${value.status}`.toLowerCase().includes(runSearch.toLowerCase()))
  .sort((a,b)=>Date.parse(b.startedAt)-Date.parse(a.startedAt)||b.id.localeCompare(a.id));
 const pages=Math.max(1,Math.ceil(runs.length/20));const page=Math.min(runPage,pages-1);
 return <section className="skills-associations" aria-label={mode==='associations'?'Workflow associations':'Inspect instructions'}>
  <header className="section-head"><div><h2>{mode==='associations'?'Workflow associations':'Inspect instructions'}</h2><p className="muted">{mode==='associations'?'Choose which guidance a workflow and its agents may load. Saving creates a workflow version.':'Read saved configuration or the immutable instruction record from a past execution.'}</p></div>{dirty&&<span className="badge tone-warn">Unsaved associations</span>}</header>
  {graphs.loading&&!graphs.data&&<p role="status">Loading workflows…</p>}<ErrorBox error={graphs.error}/>
  {graphs.error&&<Btn onClick={()=>void graphs.reload()}>Retry loading workflows</Btn>}
  <div className="skills-filters"><HelpField label="Workflow" help="Select the workflow to configure or inspect. This does not start it."><select disabled={busy||graphs.loading||!!graphs.error} value={graph?.metadata.id??''} onChange={event=>{
    if(dirty&&!window.confirm('Discard unsaved associations?'))return;
    const next=graphs.data?.find(s=>s.graph.metadata.id===event.target.value)?.graph;
    setReviewedGraph(undefined);graphSelection.current=next?.metadata.id??'';setGraph(next);setBaseline(JSON.stringify(next));setNode(next?.spec.nodes[0]?.id??'');setSnapshot(undefined);setExecutions(undefined);setError(undefined);setNotice(undefined);
    onSelection(next?.metadata.id,next?.spec.nodes[0]?.id);
  }}><option value="">Select workflow</option>{graphs.data?.filter(s=>!s.graph.metadata.id.startsWith('task-run-')&&!s.graph.metadata.labels?.['weft.task']).map(s=><option key={s.graph.metadata.id} value={s.graph.metadata.id}>{s.graph.metadata.name||s.graph.metadata.id}</option>)}</select></HelpField>
  {mode==='inspect'&&graph&&<HelpField label="Agent" help="Inspect one workflow node. Saved configuration can differ from past execution snapshots."><select disabled={busy} value={node} onChange={event=>{setNode(event.target.value);setSnapshot(undefined);setRunPage(0);onSelection(graph.metadata.id,event.target.value);}}>{graph.spec.nodes.map(n=><option value={n.id} key={n.id}>{n.name||n.id}</option>)}</select></HelpField>}
  </div>
  {workflowId&&!graph&&!graphs.loading&&!graphs.error&&<p role="note">Workflow not found.</p>}
  {!graph&&!workflowId&&!graphs.loading&&<p className="empty">Select a workflow to {mode==='associations'?'configure its guidance.':'inspect its instruction records.'}</p>}
  {busy&&mode==='associations'&&<p role="status">Saving workflow associations…</p>}
  <ErrorBox error={error}/>{error&&executionId&&<Btn disabled={busy} onClick={()=>setSnapshotAttempt(value=>value+1)}>Retry execution instructions</Btn>}{notice&&<p role="status">{notice}</p>}
  {graph&&mode==='associations'&&<div className="card form skill-association-form">
            <fieldset disabled={!edit || busy}>
              <HelpField label="Load installation defaults" help="Loads every enabled installation-default skill for this workflow. Agent narrowing does not remove these defaults. Leave off to use only explicitly selected workflow skills.">
                <input
                  type="checkbox"
                  checked={!!refs.installationDefaults}
                  onChange={(e) =>
                    updateGraph({
                      ...graph,
                      spec: {
                        ...graph.spec,
                        instructions: {
                          ...refs,
                          installationDefaults: e.target.checked,
                        },
                      },
                    })
                  }
                />
              </HelpField>
              <HelpField label="Authorize project steering from this graph workspace" help="Allows bounded Markdown guidance from .weft/steering in this workflow workspace. It cannot grant tools or change permissions.">
                <input
                  type="checkbox"
                  checked={!!refs.projectSteering}
                  onChange={(e) =>
                    updateGraph({
                      ...graph,
                      spec: {
                        ...graph.spec,
                        instructions: {
                          ...refs,
                          projectSteering: e.target.checked,
                        },
                      },
                    })
                  }
                />
              </HelpField>
              <p>
                Project steering reads .weft/steering/*.md from {graph.spec.workspace?.path || graph.metadata.id}.

              </p>
              <fieldset>
                <legend>Repository guidance</legend>
                <HelpField label="Discover repository instructions and skills" help="Allows repository guidance from the supported source families. Block individual families below; agents can add restrictions but cannot re-enable a workflow-blocked source.">
                  <input
                    type="checkbox"
                    checked={repositoryEnabled}
                    onChange={(e) => updateGraph({
                      ...graph,
                      spec: { ...graph.spec, instructions: { ...refs, repositoryGuidance: { ...refs.repositoryGuidance, disabled: !e.target.checked } } },
                    })}
                  />
                </HelpField>
                <SourceBlocks
                  disabled={!repositoryEnabled}
                  policy={refs.repositoryGuidance}
                  onChange={(policy) => updateGraph({
                    ...graph,
                    spec: { ...graph.spec, instructions: { ...refs, repositoryGuidance: policy } },
                  })}
                />
              </fieldset>
              <h3>Workflow skills</h3><p className="muted small">Agents inherit this selection unless narrowed below. Installation defaults are loaded separately when opted in.</p><div className="skill-selection-list">{available.map((s) => (
                <label key={s.id}>
                  <input
                    type="checkbox"
                    checked={refs.skillIds?.includes(s.id) ?? false}
                    onChange={(e) => {
                      const ids = e.target.checked
                        ? [...(refs.skillIds ?? []), s.id]
                        : (refs.skillIds ?? []).filter((id) => id !== s.id);
                      updateGraph({
                        ...graph,
                        spec: {
                          ...graph.spec,
                          instructions: { ...refs, skillIds: ids },
                          nodes: graph.spec.nodes.map((n) => ({
                            ...n,
                            skillIds:
                              n.skillIds?.filter((id) => ids.includes(id)) ??
                              n.skillIds,
                          })),
                        },
                      });
                    }}
                  />
                  {s.title} ({s.id})
                </label>
              ))}
              </div><h3>Agent selections</h3>{graph.spec.nodes.map((n) => (
                <details className="skill-agent-settings" key={n.id} open={agentId===n.id || undefined}><summary>{n.name || n.id} · {n.skillIds==null?"Inherited skills":"Specific selection"}</summary><fieldset>
                  <legend>{n.name || n.id}</legend>
                  <HelpField label="Inherit graph skills" help="Use the workflow skill selection. Uncheck to select a subset for this agent; an empty subset loads none of those selected skills. Opted-in installation defaults are separate.">
                    <input
                      type="checkbox"
                      checked={n.skillIds == null}
                      onChange={(e) =>
                        updateGraph({
                          ...graph,
                          spec: {
                            ...graph.spec,
                            nodes: graph.spec.nodes.map((v) =>
                              v.id === n.id
                                ? {
                                    ...v,
                                    skillIds: e.target.checked ? null : [],
                                  }
                                : v,
                            ),
                          },
                        })
                      }
                    />
                  </HelpField>
                  <fieldset disabled={!repositoryEnabled}>
                    <legend>Repository guidance for {n.name || n.id}</legend>
                    <HelpField label="Disable inherited repository guidance" help="Turns repository guidance off for this agent. It does not disable managed workflow skills or grant access to additional sources.">
                      <input
                        type="checkbox"
                        checked={!!n.repositoryGuidance?.disabled}
                        onChange={(e) => updateGraph({
                          ...graph,
                          spec: { ...graph.spec, nodes: graph.spec.nodes.map((v) => v.id === n.id
                            ? { ...v, repositoryGuidance: { ...v.repositoryGuidance, disabled: e.target.checked } } : v) },
                        })}
                      />
                    </HelpField>
                    <SourceBlocks
                      disabled={!!n.repositoryGuidance?.disabled}
                      policy={n.repositoryGuidance}
                      inherited={refs.repositoryGuidance?.blockedSources}
                      onChange={(policy) => updateGraph({
                        ...graph,
                        spec: { ...graph.spec, nodes: graph.spec.nodes.map((v) => v.id === n.id
                          ? { ...v, repositoryGuidance: policy } : v) },
                      })}
                    />
                  </fieldset>
                  {n.skillIds != null &&
                    (refs.skillIds ?? []).map((id) => (
                      <label key={id}>
                        <input
                          type="checkbox"
                          checked={n.skillIds!.includes(id)}
                          onChange={(e) =>
                            updateGraph({
                              ...graph,
                              spec: {
                                ...graph.spec,
                                nodes: graph.spec.nodes.map((v) =>
                                  v.id === n.id
                                    ? {
                                        ...v,
                                        skillIds: e.target.checked
                                          ? [...(v.skillIds ?? []), id]
                                          : (v.skillIds ?? []).filter(
                                              (s) => s !== id,
                                            ),
                                      }
                                    : v,
                                ),
                              },
                            })
                          }
                        />
                        {id}
                      </label>
                    ))}
                </fieldset></details>
              ))}
              <button ref={reviewTrigger} className="btn btn-primary" type="button" disabled={!dirty} onClick={()=>setReviewedGraph(graph)}>
                Review associations
              </button>
            </fieldset>
  </div>}
  {reviewedGraph&&mode==='associations'&&<Modal title="Review workflow associations" onClose={closeReview} keyboard>
    <p>{reviewedGraph.metadata.name||reviewedGraph.metadata.id} · expected version {reviewedGraph.metadata.version}</p>
    <p>Save creates a new workflow version. Running executions keep their saved configuration. This does not start agents or grant tools.</p>
    <h3>Changed sections</h3>
    <ul>{associationChanges(JSON.parse(baseline) as Graph,reviewedGraph).map(path=><li key={path}><code>{path}</code></li>)}</ul>
    <div className="btn-row">
      <Btn autoFocus disabled={busy} onClick={closeReview}>Cancel</Btn>
      <Btn tone="primary" disabled={busy||!edit||reviewedGraph!==graph||!dirty} onClick={()=>void run(async()=>{
        if (!edit || reviewedGraph!==graph || !dirty) return;
        const candidate=reviewedGraph;
        setReviewedGraph(undefined);
        const saved=await api.updateGraph(candidate.metadata.id,candidate,candidate.metadata.version);
        setGraph(saved.graph);setBaseline(JSON.stringify(saved.graph));
        setNotice(`Associations saved in workflow version ${saved.graph.metadata.version}.`);
        await graphs.reload();onChanged();reviewTrigger.current?.focus();
      })}>{busy?'Saving…':'Save associations'}</Btn>
    </div>
  </Modal>}
  {graph&&mode==='inspect'&&<>
    <div className="card skill-inspection-controls"><h3>Saved configuration</h3><p className="muted small">Reads the currently saved workflow and skill revisions. This is not a record of a previous execution.</p><Btn disabled={busy||!node} onClick={()=>void savedInstruction()}>Inspect saved instructions</Btn></div>
    <section className="card skill-executions" aria-label="Execution instructions"><div className="section-head"><div><h3>Execution records</h3><p className="muted small">Up to 100 recent executions. A snapshot records the revisions and omissions used at that time.</p></div><Btn disabled={busy} onClick={()=>void run(async()=>{setExecutions(await api.executions(graph.metadata.id));setRunPage(0);})}>{busy?'Loading…':'Load executions'}</Btn></div>
      {executions&&<><label>Search executions<input type="search" value={runSearch} placeholder="Execution ID, agent or status" onChange={event=>{setRunSearch(event.target.value);setRunPage(0);}}/></label>
      {runs.length?<div className="skills-table-scroll"><table className="table"><thead><tr><th>Execution</th><th>Agent</th><th>Status</th><th>Started</th></tr></thead><tbody>{runs.slice(page*20,(page+1)*20).map(value=><tr key={value.id}><td><Btn disabled={busy} onClick={()=>{setSnapshot(undefined);onSelection(graph.metadata.id,value.nodeId,value.id);}}>Inspect {value.nodeId} · {value.id}</Btn></td><td>{value.nodeId}</td><td>{value.status}</td><td>{value.startedAt?new Date(value.startedAt).toLocaleString():'Not recorded'}</td></tr>)}</tbody></table></div>:<p>No executions match this selection.</p>}
      {pages>1&&<nav className="btn-row" aria-label="Execution pages"><Btn disabled={page===0} onClick={()=>setRunPage(page-1)}>Previous</Btn><span>Page {page+1} of {pages}</span><Btn disabled={page===pages-1} onClick={()=>setRunPage(page+1)}>Next</Btn></nav>}</>}
    </section>
    {busy&&<p role="status">Loading instruction data…</p>}
    {snapshot&&<div className="card skill-manifest"><InstructionManifest snapshot={snapshot} compact/></div>}
  </>}
 </section>;
}
function SourceBlocks({ policy, inherited, disabled, onChange }: {
  policy?: RepositoryGuidancePolicy;
  inherited?: RepositorySource[];
  disabled: boolean;
  onChange: (policy: RepositoryGuidancePolicy) => void;
}) {
  return repositorySources.map((source) => {
    const blockedByGraph = inherited?.includes(source) ?? false;
    const blocked = policy?.blockedSources?.includes(source) ?? false;
    return (
      <label key={source}>
        <input
          type="checkbox"
          disabled={disabled || blockedByGraph}
          checked={blocked || blockedByGraph}
          onChange={(e) => onChange({
            ...policy,
            blockedSources: e.target.checked
              ? [...(policy?.blockedSources ?? []), source]
              : policy?.blockedSources?.filter((value) => value !== source),
          })}
        />
        Block {source} sources{blockedByGraph ? " (inherited)" : ""}
      </label>
    );
  });
}
