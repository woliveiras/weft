import {test,expect} from '@playwright/test';
const admin={Authorization:'Bearer e2e-admin-token'},viewer={Authorization:'Bearer e2e-viewer-token'};
for(const scope of ['child','tree'] as const){
 test('execution tree interrupts '+scope+' with explicit role and confirmation',async({page})=>{
  const id='tree-stop-'+scope;
  const graph={metadata:{id,name:id},spec:{nodes:[{id:'parent',runtime:{kind:'native'},prompt:'delegation-fixture',model:{provider:'e2e',name:'fixture'},loop:{mode:'once'},tools:[{name:'delegation.request'}]}]}};
  expect((await page.request.post('/api/v1/graphs',{headers:admin,data:graph})).status()).toBe(201);
  await page.addInitScript(()=>{if(!localStorage.getItem('weftToken'))localStorage.setItem('weftToken','e2e-admin-token');});
  await page.goto('/#/g/'+id+'/canvas');await page.getByRole('button',{name:'Run all',exact:true}).click();
  let approval:{id:string;executionId:string;detail:{parentExecutionId:string}}|undefined;
  await expect.poll(async()=>{approval=(await(await page.request.get('/api/v1/approvals?graph='+id,{headers:admin})).json()).find((a:{kind:string;status:string})=>a.kind==='delegation'&&a.status==='pending');return !!approval;}).toBe(true);
  const child=approval!.executionId,root=approval!.detail.parentExecutionId;
  const held=await(await page.request.get('/api/v1/executions/'+root+'/tree',{headers:admin})).json();
  expect(held.budget.reserved).toBeGreaterThan(0);
  await page.goto('/#/g/'+id+'/runs?execution='+child+'&section=tree');
  const tree=page.getByRole('region',{name:'Execution tree',exact:true});await expect(tree.getByRole('link',{name:'parent · '+root})).toBeVisible();
  await page.evaluate(()=>localStorage.setItem('weftToken','e2e-viewer-token'));await page.reload();
  await expect(tree.getByRole('link',{name:'parent · '+child})).toBeVisible();await expect(tree.getByRole('button',{name:'Stop child'})).toHaveCount(0);
  expect((await page.request.post('/api/v1/executions/'+child+'/cancel',{headers:viewer,data:{scope,confirm:true}})).status()).toBe(403);
  await page.evaluate(()=>localStorage.setItem('weftToken','e2e-admin-token'));await page.reload();
  await tree.getByRole('button',{name:scope==='child'?'Stop child':'Stop entire tree',exact:true}).click();
  const review=page.getByRole('region',{name:'Confirm interruption'});await expect(review).toContainText(scope==='child'?'Its parent receives an error':'root execution and its active children');
  await review.getByRole('button',{name:'Keep running'}).click();
  expect((await(await page.request.get('/api/v1/executions/'+child,{headers:admin})).json()).status).toBe('awaiting_approval');
  await tree.getByRole('button',{name:scope==='child'?'Stop child':'Stop entire tree',exact:true}).click();await review.getByRole('button',{name:'Confirm interruption'}).click();
  await expect(tree).toContainText('Interruption requested. Waiting for the worker');
  await expect.poll(async()=>(await(await page.request.get('/api/v1/executions/'+root,{headers:admin})).json()).status).toBe(scope==='child'?'succeeded':'cancelled');
  await expect.poll(async()=>(await(await page.request.get('/api/v1/executions/'+child,{headers:admin})).json()).status).toBe('cancelled');
  await tree.getByRole('button',{name:'Refresh tree'}).click();await expect(tree.getByRole('button',{name:'Stop child'})).toBeDisabled();
  const settled=await(await page.request.get('/api/v1/executions/'+root+'/tree',{headers:admin})).json();
  expect(settled.budget.reserved).toBe(0);
  // This fixture omits parent usage: dispatched parent calls retain estimates,
  // while the child cancelled before inference is refunded in full.
  expect(settled.budget.estimated).toBeGreaterThanOrEqual(held.budget.estimated);
  expect(settled.budget.charged).toBe(settled.budget.estimated);
  if(scope==='tree')expect(settled.budget.estimated).toBe(held.budget.estimated);
  await expect(page.getByRole('region',{name:'Tree token budget',exact:true})).toContainText('Reserved: 0');
  const retry=await page.request.post('/api/v1/executions/'+child+'/cancel',{headers:admin,data:{scope,confirm:true}});
  expect(retry.status()).toBe(200);expect((await retry.json()).alreadyTerminal).toBe(true);
  const approvals=await(await page.request.get('/api/v1/approvals?graph='+id,{headers:admin})).json();expect(approvals.find((a:{id:string})=>a.id===approval!.id).status).toBe('expired');
  const saved=await(await page.request.get('/api/v1/graphs/'+id,{headers:admin})).json();expect(saved.record.version).toBe(1);expect(saved.graph.spec.nodes[0].tools).toEqual(graph.spec.nodes[0].tools);
 });
}
