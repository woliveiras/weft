import {createContext,useContext,useEffect,useRef,type ReactNode,type RefObject} from 'react';
import {Btn} from './common';

export type AdminDraftState={dirty:boolean;busy:boolean};
export const AdminDraftContext=createContext<(id:string,state:AdminDraftState)=>void>(()=>{});
export function useAdminDraft(id:string,dirty:boolean,busy=false) {
  const report=useContext(AdminDraftContext);
  useEffect(()=>{report(id,{dirty,busy});return()=>report(id,{dirty:false,busy:false});},[id,dirty,busy,report]);
  useEffect(()=>{
    if(!dirty&&!busy)return;
    const warn=(event:BeforeUnloadEvent)=>{event.preventDefault();event.returnValue='';};
    window.addEventListener('beforeunload',warn);return()=>window.removeEventListener('beforeunload',warn);
  },[dirty,busy]);
  return ()=>report(id,{dirty:false,busy:false});
}
export function discardAdminDraft(dirty:boolean){return !dirty||window.confirm('Discard unsaved administration changes?');}
export function AdminHeader({title,description,children}:{title:string;description?:string;children?:ReactNode}) {
  return <header className="section-head admin-section-head"><div><h2>{title}</h2>{description&&<p className="muted">{description}</p>}</div><div className="btn-row">{children}</div></header>;
}
export function AdminPages({page,total,onPage}:{page:number;total:number;onPage:(value:number)=>void}) {
 const pages=Math.max(1,Math.ceil(total/20));const current=Math.min(page,pages-1);
 return pages>1?<nav className="btn-row admin-pagination" aria-label="Table pages"><Btn disabled={current===0} onClick={()=>onPage(current-1)}>Previous</Btn><span>Page {current+1} of {pages}</span><Btn disabled={current===pages-1} onClick={()=>onPage(current+1)}>Next</Btn></nav>:null;
}
export function adminPage<T>(rows:T[],page:number):T[] {return rows.slice(Math.min(page,Math.max(0,Math.ceil(rows.length/20)-1))*20,(Math.min(page,Math.max(0,Math.ceil(rows.length/20)-1))+1)*20);}
export type AdminChange={label:string;before:string|number;after:string|number};
export function AdminChanges({changes}:{changes:AdminChange[]}) {
 return <div className="admin-table-scroll" tabIndex={0} role="region" aria-label="Changes to review"><table className="table"><thead><tr><th>Setting</th><th>Current</th><th>After confirmation</th></tr></thead><tbody>{changes.map(change=><tr key={change.label}><th scope="row">{change.label}</th><td>{change.before}</td><td>{change.after}</td></tr>)}</tbody></table></div>;
}

export function AdminReview({label,children,returnFocus}:{label:string;children:ReactNode;returnFocus?:RefObject<HTMLElement|null>}) {
 const panel=useRef<HTMLElement>(null);
 useEffect(()=>{panel.current?.focus();return()=>{if(returnFocus)requestAnimationFrame(()=>returnFocus.current?.focus());};},[returnFocus]);
 return <section className="card admin-review" ref={panel} tabIndex={-1} aria-label={label}>{children}</section>;
}
