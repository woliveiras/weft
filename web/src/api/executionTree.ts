import type { Execution } from './types';
export type CancelScope = 'child'|'tree';
export interface Cancellation { executionId:string; rootExecutionId:string; scope:CancelScope; actor:string; requestedAt:string }
export interface ExecutionTree { budget?:{limit:number;charged:number;reserved:number;estimated:number}; rootExecutionId:string; selectedExecutionId:string; nodes:{execution:Execution;cancellation?:Cancellation}[] }
export function activeExecution(x:Execution) { return ['pending','running','awaiting_approval'].includes(x.status); }
export function treeDepths(tree:ExecutionTree,graph:string,selected:string):Map<string,number> {
 const depths=new Map<string,number>();
 if(tree.selectedExecutionId!==selected || !tree.nodes.length || tree.nodes.length>256) throw new Error('Invalid execution tree. Refresh before taking action.');
 const root=tree.nodes[0].execution;
 if(root.id!==tree.rootExecutionId||root.parentExecutionId)throw new Error('Invalid execution tree root.');
 for(const {execution:x} of tree.nodes){
  if(x.graphId!==graph||x.runId!==root.runId||depths.has(x.id))throw new Error('Inconsistent execution tree.');
  const parent=x.parentExecutionId?depths.get(x.parentExecutionId): -1;
  if(parent===undefined || (x.id!==root.id&&!x.parentExecutionId)||parent>=15)throw new Error('Incomplete execution tree.');
  depths.set(x.id,parent+1);
 }
 if(!depths.has(selected))throw new Error('Selected execution is missing from the tree.');
 return depths;
}
