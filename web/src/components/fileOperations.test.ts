import { expect, it } from 'vitest';
import { fileOperations } from './fileOperations';
const event = { seq: 1, id: 'ev', time: '', type: 'tool.completed', executionId: 'exec', data: { tool: 'filesystem.write', args: { path: 'src/file.txt' }, outcome: 'ok' } };
it('links recorded operations to execution and matching paths without guessing shell effects', () => {
 expect(fileOperations([event], 'src')).toHaveLength(1);
 expect(fileOperations([event], 'src/file.txt')[0].executionId).toBe('exec');
 expect(fileOperations([event], 'src/file')).toHaveLength(0);
 expect(fileOperations([{...event, data:{tool:'shell.exec',args:{path:'src/file.txt'}}}], '')).toHaveLength(0);
});
it('ignores malformed or truncated audit arguments and preserves failed outcomes', () => {
 expect(fileOperations([{...event, data:null}, {...event, executionId:undefined}, {...event, data:{tool:'filesystem.write',args:'truncated…'}}], '')).toHaveLength(0);
 expect(fileOperations([{...event, data:{...event.data,outcome:'error'}}], '')[0].outcome).toBe('error');
});
it('shows observed shell changes and partial captures without inventing attribution', () => {
 const observation = {...event, type:'workspace.observed', data:{tool:'shell.exec',status:'complete',changes:[{path:'src/a',operation:'modified'},{path:'new',operation:'created'}]}};
 expect(fileOperations([observation], 'src')[0].paths).toEqual(['src/a (modified)']);
 expect(fileOperations([observation], 'missing')).toEqual([]);
 expect(fileOperations([{...observation,data:{tool:'shell.exec',status:'partial',changes:[]}}], 'missing')[0].outcome).toContain('partial');
 expect(fileOperations([{...observation,data:{tool:'shell.exec',changes:[null,{}, {path:3}]}}], '')[0].paths).toEqual([]);
});
it('keeps external observations without inventing an execution or author', () => {
 const monitored = {...event, executionId:undefined, type:'workspace.observed',data:{source:'monitor',status:'complete',gap:true,changes:[{path:'external.txt',operation:'modified'}]}};
 const operation=fileOperations([monitored],'external.txt')[0];
 expect(operation.executionId).toBeUndefined();
 expect(operation.tool).toBe('workspace monitor');
 expect(operation.outcome).toContain('author unknown');
 expect(operation.outcome).toContain('coverage gap');
});
it('shows kernel PID evidence without guessing execution or process lifetime', () => {
 const record={...event,executionId:undefined,type:'workspace.observed',data:{source:'kernel-journal',status:'complete',process:{pid:12},changes:[{path:'transient',operation:'deleted'}]}};
 const operation=fileOperations([record],'')[0];
 expect(operation.tool).toBe('kernel journal');
 expect(operation.outcome).toContain('kernel PID 12');
 expect(operation.outcome).toContain('process lifetime unavailable');
 expect(operation.executionId).toBeUndefined();
});
it('lists the files of multi-file edit tools', () => {
 const patch = '--- a/src/a.go\n+++ b/src/a.go\n@@ -1 +1 @@\n-x\n+y\n--- /dev/null\n+++ b/docs/new.md\n@@ -0,0 +1 @@\n+z\n';
 expect(fileOperations([{...event, data:{tool:'filesystem.apply_patch',args:{patch},outcome:'ok'}}], '')[0].paths).toEqual(['src/a.go', 'docs/new.md']);
 expect(fileOperations([{...event, data:{tool:'filesystem.apply_patch',args:{patch},outcome:'ok'}}], 'docs')).toHaveLength(1);
 expect(fileOperations([{...event, data:{tool:'filesystem.apply_patch',args:{patch:'--- a/x\n+++ /dev/null\n'},outcome:'error'}}], '')[0].paths).toEqual([]);
 expect(fileOperations([{...event, data:{tool:'filesystem.multi_edit',args:{path:'src/b.go',edits:[]},outcome:'ok'}}], 'src/b.go')).toHaveLength(1);
});
