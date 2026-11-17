import {expect,it} from 'vitest';
import type {Execution} from './types';
import {treeDepths,activeExecution,type ExecutionTree} from './executionTree';
const root={id:'r',graphId:'g',runId:'run',status:'running'} as Execution;
const child={...root,id:'c',parentExecutionId:'r'};
const tree:ExecutionTree={rootExecutionId:'r',selectedExecutionId:'c',nodes:[{execution:root},{execution:child}]};
it('derives hierarchy from saved parents and distinguishes active outcomes',()=>{expect([...treeDepths(tree,'g','c')]).toEqual([['r',0],['c',1]]);expect(activeExecution(root)).toBe(true);for(const status of ['succeeded','failed','cancelled','interrupted'] as const)expect(activeExecution({...root,status})).toBe(false);});
it('rejects unrelated, duplicate, missing, reversed and oversized trees',()=>{
 for(const nodes of [[],[{execution:child}],tree.nodes.slice().reverse(),[...tree.nodes,{execution:child}],[{execution:root},{execution:{...child,graphId:'other'}}],[{execution:root},{execution:{...child,runId:'other'}}],Array(257).fill({execution:root})]) expect(()=>treeDepths({...tree,nodes},'g','c')).toThrow();
 expect(()=>treeDepths(tree,'g','other')).toThrow();
});
