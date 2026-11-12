import type { Execution, WeftEvent } from '../api/types';
export const runSections = ['result', 'tree', 'timeline', 'tools', 'messages', 'guidance', 'diagnostics'] as const;
export type RunSection = typeof runSections[number];
export const runSectionTitles: Record<RunSection,string> = {result:'Result',tree:'Execution tree',timeline:'Timeline',tools:'Tool calls',messages:'Messages',guidance:'Guidance',diagnostics:'Diagnostics'};
export function runSection(value?:string|null):RunSection { return runSections.includes(value as RunSection) ? value as RunSection : 'result'; }
export function runLink(graph:string,id?:string,section:RunSection='result') {
 return `#/g/${encodeURIComponent(graph)}/runs${id ? `?execution=${encodeURIComponent(id)}&section=${section}` : ''}`;
}
export function matchingRuns(runs:Execution[],graph:string,query:string,status:string) {
 const search=query.trim().toLowerCase();
 return runs.filter(r=>r.graphId===graph&&(!status||r.status===status)&&[r.id,r.nodeId,r.summary??''].some(v=>v.toLowerCase().includes(search)));
}
export function runEvents(events:WeftEvent[],execution:Execution) {
 return events.filter(e=>e.graphId===execution.graphId&&e.executionId===execution.id).sort((a,b)=>a.seq-b.seq);
}
export function blockedToolCalls(events:WeftEvent[]) { return events.filter(e=>e.type==='tool.denied').length; }
