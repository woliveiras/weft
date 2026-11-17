import { useState } from 'react';
import type { GraphSummary } from '../api/types';
import { Btn, ErrorBox } from './common';
import { matchingGraphs } from './graphNavigation';
export function GraphSelector({graphs,currentId,dirty,loading,error,onSelect,onReload}:{graphs:GraphSummary[];currentId?:string;dirty:Record<string,boolean>;loading:boolean;error?:string;onSelect:(id:string)=>void;onReload:()=>void}) {
 const [open,setOpen]=useState(false);const [query,setQuery]=useState('');
 const current=graphs.find(g=>g.graph.metadata.id===currentId);
 const matches=matchingGraphs(graphs,query);
 return <div className="graph-selector">
  <Btn aria-label="Select graph" aria-expanded={open} aria-controls="graph-selector-panel" onClick={()=>setOpen(!open)}>{current?.graph.metadata.name||currentId||'Select graph'}{currentId&&dirty[currentId]?' · unsaved':''}</Btn>
  {open&&<section id="graph-selector-panel" aria-label="Graph selector" className="card" onKeyDown={e=>{if(e.key==='Escape')setOpen(false);}}>
   <label>Search graphs<input type="search" autoFocus value={query} onChange={e=>setQuery(e.target.value)} placeholder="Name or ID"/></label>
   <ErrorBox error={error}/>{loading&&<p role="status">Loading graphs…</p>}
   <Btn onClick={onReload} disabled={loading}>Reload graphs</Btn>
   {!loading&&!error&&!matches.length&&<p>{graphs.length?'No graphs match this search.':'No graphs available.'}</p>}
   <ul>{matches.map(g=><li key={g.graph.metadata.id}><button type="button" aria-current={currentId===g.graph.metadata.id?'page':undefined} onClick={()=>{onSelect(g.graph.metadata.id);setOpen(false);setQuery('');}}><strong>{g.graph.metadata.name||g.graph.metadata.id}</strong><span>{g.graph.metadata.id} · {g.record.state}{dirty[g.graph.metadata.id]?' · unsaved draft':''}</span></button></li>)}</ul>
  </section>}
 </div>;
}
