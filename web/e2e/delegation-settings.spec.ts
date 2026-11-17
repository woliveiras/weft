import {test,expect, type Page} from '@playwright/test';
const admin={Authorization:'Bearer e2e-admin-token'};
const viewer={Authorization:'Bearer e2e-viewer-token'};
async function run(page:Page,id:string,grants=true){
 const graph={metadata:{id,name:id},spec:{nodes:[{id:'parent',runtime:{kind:'native'},prompt:'delegation-fixture custom-profile',model:{provider:'e2e',name:'fixture'},loop:{mode:'once'},tools:grants?[{name:'delegation.request'}]:[]}]}};
 expect((await page.request.post('/api/v1/graphs',{headers:admin,data:graph})).status()).toBe(201);
 await page.goto('/#/g/'+id+'/canvas');await page.getByRole('button',{name:'Run all',exact:true}).click();
 await expect.poll(async()=>{const xs=await (await page.request.get('/api/v1/graphs/'+id+'/executions',{headers:admin})).json();return xs.some((x:{parentExecutionId?:string;status:string})=>!x.parentExecutionId&&x.status==='succeeded');}).toBe(true);
 return await (await page.request.get('/api/v1/graphs/'+id+'/executions',{headers:admin})).json();
}
test('administrator reviews profiles and automatic limits; disabling and grants remain enforced',async({page})=>{
 await page.addInitScript(()=>{if(!localStorage.getItem('weftToken'))localStorage.setItem('weftToken','e2e-admin-token');});
 const original=await (await page.request.get('/api/v1/delegation-policy',{headers:admin})).json();
 try{
 await page.goto('/#/administration/delegation');
 await page.getByRole('button',{name:'New delegation profile',exact:true}).click();
 await page.getByLabel('Profile ID',{exact:true}).fill('browser-review');await page.getByLabel('Profile name',{exact:true}).fill('Browser review');await page.getByLabel('Profile instructions',{exact:true}).fill('Review supplied text. Do not request tools.');
 await page.getByLabel('Profile output token allowance',{exact:true}).fill('40');
 await page.getByRole('button',{name:'Review delegation profile',exact:true}).click();
 await page.getByRole('button',{name:'Confirm delegation profile',exact:true}).click();
 await expect(page.getByText('Delegation profile saved. No agent permissions were changed.')).toBeVisible();
 await page.getByRole('combobox',{name:'Approval mode',exact:true}).selectOption('automatic');await page.getByLabel('Concurrent children',{exact:true}).fill('2');
 await page.getByRole('button',{name:'Review delegation policy',exact:true}).click();
 await expect(page.getByText(/without individual delegation approval/)).toBeVisible();
 await page.getByRole('button',{name:'Confirm delegation policy',exact:true}).click();await expect(page.getByText('Delegation policy saved. New requests use these settings.')).toBeVisible();
 const xs=await run(page,'profile-automatic');
 const child=xs.find((x:{parentExecutionId?:string})=>x.parentExecutionId);
 expect(child.status).toBe('succeeded');expect(child.approvalId).toBeUndefined();expect(child.delegationTemplate).toBe('browser-review');expect(child.delegationConfig.profile.revision).toBe(1);expect(child.delegationConfig.policy.policy.maxConcurrentChildren).toBe(2);
 const approvals=await (await page.request.get('/api/v1/approvals?graph=profile-automatic',{headers:admin})).json();expect(approvals).toHaveLength(0);
 await page.goto('/#/g/profile-automatic/runs?execution='+child.id+'&section=result');await expect(page.getByRole('complementary',{name:'Delegation context'})).toContainText('browser-review');await page.getByRole('link',{name:'View parent execution'}).click();await expect(page.getByRole('region',{name:'Run detail'})).toContainText('Delegated fixture result');
 const denied=await run(page,'profile-no-grant',false);expect(denied).toHaveLength(1);expect(denied[0].summary).not.toContain('Delegated fixture result');
 await page.goto('/#/administration/delegation');await page.getByRole('button',{name:'Edit Browser review',exact:true}).click();await page.getByLabel('Profile enabled',{exact:true}).uncheck();await page.getByRole('button',{name:'Review delegation profile',exact:true}).click();await page.getByRole('button',{name:'Confirm delegation profile',exact:true}).click();await expect(page.getByText('Delegation profile saved. No agent permissions were changed.')).toBeVisible();
 const disabled=await run(page,'profile-disabled');expect(disabled).toHaveLength(1);expect(disabled[0].summary).toContain('profile is disabled');
 const saved=await (await page.request.get('/api/v1/graphs/profile-automatic',{headers:admin})).json();expect(saved.record.version).toBe(1);expect(saved.graph.spec.nodes[0].tools).toEqual([{name:'delegation.request'}]);
 }finally{
 const latest=await (await page.request.get('/api/v1/delegation-policy',{headers:admin})).json();expect((await page.request.put('/api/v1/delegation-policy',{headers:{...admin,'If-Match':String(latest.revision)},data:original.policy})).status()).toBe(200);
 }
});
test('delegation settings enforce viewer access and stale policy conflicts',async({page})=>{
 const original=await (await page.request.get('/api/v1/delegation-policy',{headers:admin})).json();
 await page.addInitScript(()=>{if(!localStorage.getItem('weftToken'))localStorage.setItem('weftToken','e2e-admin-token');});
 try{
 await page.goto('/#/administration/delegation');await page.getByLabel('Concurrent children',{exact:true}).fill('3');
 await page.getByRole('button',{name:'Review delegation policy',exact:true}).click();
 expect((await page.request.put('/api/v1/delegation-policy',{headers:{...admin,'If-Match':String(original.revision)},data:original.policy})).status()).toBe(200);
 await page.getByRole('button',{name:'Confirm delegation policy',exact:true}).click();await expect(page.getByText(/Save could not be confirmed/)).toBeVisible();
 expect((await page.request.put('/api/v1/delegation-policy',{headers:{...viewer,'If-Match':String(original.revision)},data:original.policy})).status()).toBe(403);
 await page.evaluate(()=>localStorage.setItem('weftToken','e2e-viewer-token'));await page.reload();
 await expect(page.getByRole('combobox',{name:'Approval mode',exact:true})).toBeDisabled();await expect(page.getByRole('button',{name:'New delegation profile',exact:true})).toHaveCount(0);
 }finally{
 const latest=await (await page.request.get('/api/v1/delegation-policy',{headers:admin})).json();expect((await page.request.put('/api/v1/delegation-policy',{headers:{...admin,'If-Match':String(latest.revision)},data:original.policy})).status()).toBe(200);
 }
});

test('reviewed depth permits a bounded recursive tree with one shared budget',async({page})=>{
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 const original=await (await page.request.get('/api/v1/delegation-policy',{headers:admin})).json();
 try {
  expect((await page.request.post('/api/v1/delegation-profiles',{headers:admin,data:{id:'browser-recursive',name:'Recursive analysis',instructions:'Delegate one bounded follow-up if needed.',enabled:true,maxOutputTokens:128,tools:['delegation.request']}})).status()).toBe(201);
  await page.goto('/#/administration/delegation');
  await page.getByLabel('Maximum delegation depth',{exact:true}).fill('2');
  await page.getByRole('combobox',{name:'Approval mode',exact:true}).selectOption('automatic');
  await page.getByRole('button',{name:'Review delegation policy',exact:true}).click();
  await expect(page.getByRole('region',{name:'Confirm delegation policy'})).toContainText('Maximum delegation depth');
  await page.getByRole('button',{name:'Confirm delegation policy',exact:true}).click();
  await expect(page.getByText('Delegation policy saved. New requests use these settings.')).toBeVisible();
  const id='recursive-browser';
  expect((await page.request.post('/api/v1/graphs',{headers:admin,data:{metadata:{id,name:id},spec:{nodes:[{id:'parent',runtime:{kind:'native'},prompt:'delegation-fixture recursive-profile',model:{provider:'e2e',name:'fixture'},loop:{mode:'once'},tools:[{name:'delegation.request'}]}]}}})).status()).toBe(201);
  await page.goto('/#/g/'+id+'/canvas');await page.getByRole('button',{name:'Run all',exact:true}).click();
  let root:{id:string;summary:string}|undefined;
  await expect.poll(async()=>{const xs=await(await page.request.get('/api/v1/graphs/'+id+'/executions',{headers:admin})).json();root=xs.find((x:{parentExecutionId?:string;status:string})=>!x.parentExecutionId&&x.status==='succeeded');return !!root;}).toBe(true);
  expect(root!.summary).toContain('Recursive leaf completed');
  const tree=await(await page.request.get('/api/v1/executions/'+root!.id+'/tree',{headers:admin})).json();
  expect(tree.nodes).toHaveLength(3);expect(tree.nodes.every((n:{execution:{status:string}})=>n.execution.status==='succeeded')).toBe(true);expect(tree.budget.reserved).toBe(0);
  await page.goto('/#/g/'+id+'/runs?execution='+tree.nodes[2].execution.id+'&section=tree');
  const view=page.getByRole('region',{name:'Execution tree',exact:true});
  for(const node of tree.nodes) await expect(view.getByRole('link',{name:'parent · '+node.execution.id})).toBeVisible();
  await expect(page.getByRole('region',{name:'Tree token budget',exact:true})).toContainText('Reserved: 0');
 }finally{
  const latest=await(await page.request.get('/api/v1/delegation-policy',{headers:admin})).json();
  expect((await page.request.put('/api/v1/delegation-policy',{headers:{...admin,'If-Match':String(latest.revision)},data:original.policy})).status()).toBe(200);
 }
});
