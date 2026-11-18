import { useRef, useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { MCPHistoryPage } from '../api/types';
import { Btn, ErrorBox, fmtDateTime } from './common';
const initial={connection:'',graph:'',node:'',tool:'',outcome:'',type:'',since:'',until:''};
export function MCPHistory({connectionID}:{connectionID?:string} = {}){
 const [filters,setFilters]=useState({...initial,connection:connectionID??''});
 const [page,setPage]=useState<MCPHistoryPage>();
 const [applied,setApplied]=useState('');
 const [busy,setBusy]=useState(false);
 const [error,setError]=useState<string>();
 const generation=useRef(0);
 async function load(older=false){
  const ticket=++generation.current;
  setBusy(true);setError(undefined);
  try{
   const query=older?new URLSearchParams(applied):new URLSearchParams();
   if(!older){for(const [key,value] of Object.entries(filters)){if(value)query.set(key,key==='since'||key==='until'?new Date(value).toISOString():value);}}
   if(older&&page?.nextBefore)query.set('before',String(page.nextBefore));
   const result=await api.mcpHistory(query.toString());
   if(ticket===generation.current){setPage(result);query.delete('before');setApplied(query.toString());}
  }catch(e){if(ticket===generation.current)setError(errorMessage(e));}
  finally{if(ticket===generation.current)setBusy(false);}
 }
 return <section className="card connections-panel mcp-history" aria-label="MCP call history">
  <h2>MCP call history</h2>
  <p className="muted">Newest events first. History records outcomes and follows the installation’s retention policy.{connectionID === undefined && ' It includes deleted connections.'}</p>
  <form className="form mcp-history-form" onSubmit={e=>{e.preventDefault();void load();}}>
   <fieldset disabled={busy}><legend>History filters</legend>
    {(['connection','graph','node','tool'] as const).map(key=><label key={key}>{({connection:'History connection ID',graph:'History graph ID',node:'History agent ID',tool:'History tool name'})[key]}<input readOnly={key==='connection'&&connectionID!==undefined} value={filters[key]} onChange={e=>setFilters({...filters,[key]:e.target.value})}/></label>)}
    <label>History outcome<select value={filters.outcome} onChange={e=>setFilters({...filters,outcome:e.target.value})}><option value="">All outcomes</option>{['started','ok','error','denied','unknown','rate_limited','rejected','pending','approved','expired'].map(x=><option key={x} value={x}>{x}</option>)}</select></label>
    <label>History event type<select value={filters.type} onChange={e=>setFilters({...filters,type:e.target.value})}><option value="">All event types</option>{['tool.invoked','tool.completed','tool.denied','approval.requested','approval.decided'].map(x=><option key={x} value={x}>{x}</option>)}</select></label>
    <label>History since<input type="datetime-local" value={filters.since} onChange={e=>setFilters({...filters,since:e.target.value})}/></label>
    <label>History until<input type="datetime-local" value={filters.until} onChange={e=>setFilters({...filters,until:e.target.value})}/></label>
   </fieldset>
   <div className="btn-row">
    <Btn type="submit" tone="primary" disabled={busy}>Apply history filters</Btn>
    <Btn disabled={busy} onClick={()=>{setFilters({...initial,connection:connectionID??''});setPage(undefined);setApplied('');setError(undefined);}}>Clear history filters</Btn>
   </div>
  </form>
  <ErrorBox error={error}/>
  {busy&&<p role="status">Loading MCP history…</p>}
  {!page&&!busy&&!error&&<p>Apply filters to load history.</p>}
  {page&&<>
   <p>Showing the last successfully applied query. Edit filters and apply to update.</p>
   {!page.entries.length&&<p>{page.hasMore?'No matches in this scanned page; older events remain.':'No matching events in this page.'}</p>}
   {page.entries.length>0&&<div className="connection-table mcp-history-table" role="region" aria-label="MCP history results" tabIndex={0}>
    <table><caption className="sr-only">MCP events</caption><thead><tr>{['Time','Connection / tool','Graph / agent','Event / outcome','Duration','Result','References'].map(x=><th key={x} scope="col">{x}</th>)}</tr></thead><tbody>{page.entries.map(e=><tr key={e.seq}>
     <td><time dateTime={e.time}>{fmtDateTime(e.time)}</time></td><td>{e.connection}<br/>{e.tool}</td><td>{e.graph}<br/>{e.node}</td><td>{e.type}<br/>{e.outcome}</td><td>{e.durationMs} ms</td><td>{e.summary}</td><td>{e.execution}{e.approval&&<><br/>{e.approval}</>}</td>
    </tr>)}</tbody></table>
   </div>}
   <div className="btn-row"><Btn disabled={busy||!page.hasMore} onClick={()=>void load(true)}>Older MCP events</Btn></div>
  </>}
 </section>;
}
