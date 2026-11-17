import { useState } from 'react';
import type { Graph, GraphStatus } from '../api/types';
import { matchingAgents } from './graphNavigation';
export function AgentList({graph,status,statusError,selected,onSelect,onConfigure}:{graph:Graph;status?:GraphStatus;statusError?:string;selected?:string;onSelect:(id:string)=>void;onConfigure:(id:string)=>void}) {
 const [query,setQuery]=useState('');const nodes=matchingAgents(graph.spec.nodes,query);
 return <aside className="canvas-agent-list" aria-label="Agents"><h2>Agents <span className="muted">{graph.spec.nodes.length}</span></h2>
  <label>Search agents<input type="search" value={query} onChange={e=>setQuery(e.target.value)} placeholder="Name or ID"/></label>
  <p className="muted small">Names and settings reflect the graph draft. Runtime state reflects the saved graph.</p>
  {!nodes.length&&<p>{graph.spec.nodes.length?'No agents match this search.':'No agents in this graph.'}</p>}
  <ul>{nodes.map(n=><li key={n.id}><button type="button" aria-current={selected===n.id?'true':undefined} onClick={()=>onSelect(n.id)}><strong>{n.name||n.id}</strong><span>{n.id} · {statusError||!status?'Status unavailable':status.nodes?.find(s=>s.nodeId===n.id)?.state??'Not started'}{n.disabled?' · disabled':''}</span></button><button className="agent-configure-link" type="button" onClick={()=>onConfigure(n.id)}>Configure {n.name||n.id}</button></li>)}</ul>
 </aside>;
}
