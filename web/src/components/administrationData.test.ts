import {expect,it} from 'vitest';
import {adminPage} from './AdministrationUI';
import {changedHosts,filterAdminResources,filterAdminTools,filterNetworkUsage,resourceState} from './administrationData';
it('combines tool search and exact capability filters in stable name order with bounded pages',()=>{
 const rows=Array.from({length:43},(_,i)=>({name:`tool.${String(i).padStart(2,'0')}`,capability:i%2?'git.write':'git.read',description:'Inspect code',schema:{}}));
 expect(filterAdminTools(rows,'inspect','git.write')).toHaveLength(21);
 expect(filterAdminTools(rows,'missing','')).toEqual([]);
 expect(adminPage(rows,0)).toHaveLength(20);expect(adminPage(rows,2)).toHaveLength(3);
 expect(adminPage(rows,99)).toEqual(adminPage(rows,2));expect(adminPage([],1)).toEqual([]);
});
it('distinguishes workflow policy impact from execution copies and orders blocked admissions first',()=>{
 const rows=[{graph:'a',state:'running',hosts:['example.com'],blocked:false},{graph:'task-run-b',state:'stopped',hosts:['other.org'],blocked:true,reason:'Host not allowed'}];
 expect(filterNetworkUsage(rows,'','all','all')[0].graph).toBe('task-run-b');
 expect(filterNetworkUsage(rows,'EXAMPLE','permitted','workflow')).toHaveLength(1);
 expect(filterNetworkUsage(rows,'not allowed','blocked','execution')).toHaveLength(1);
 expect(filterNetworkUsage(rows,'','blocked','workflow')).toEqual([]);
});
it('keeps retained, failed and absent resources separate and reports exact added/removed hosts',()=>{
 const unused={id:'a',image:'image',eligible:true,removed:false,caches:[{volume:'cache',path:'/tmp'}]};
 const rows=[unused,{...unused,id:'b',usedBy:['task:test']},{...unused,id:'c',error:'Busy'},{...unused,id:'d',missing:true}];
 expect(rows.map(resourceState)).toEqual(['unused','in-use','error','retained']);
 expect(filterAdminResources(rows,'CACHE','unused').map(item=>item.id)).toEqual(['a']);
 expect(filterAdminResources(rows,'task:test','all').map(item=>item.id)).toEqual(['b']);
 expect(filterAdminResources(rows,'unknown','error')).toEqual([]);
 expect(changedHosts(['a.org','b.org'],['b.org','c.org'])).toEqual({added:['c.org'],removed:['a.org']});
});
