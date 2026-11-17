import {test,expect} from '@playwright/test';
const admin={Authorization:'Bearer e2e-admin-token'};
test('tree budget is reviewed, blocks inference and accounts parent and child once',async({page})=>{
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 const original=await(await page.request.get('/api/v1/delegation-policy',{headers:admin})).json();
 try{
  await page.goto('/#/administration/delegation');
  await page.getByLabel('Tree token budget',{exact:true}).fill('1');
  await page.getByRole('button',{name:'Review delegation policy',exact:true}).click();
  await page.getByRole('button',{name:'Confirm delegation policy',exact:true}).click();
  await expect(page.getByText('Delegation policy saved. New requests use these settings.')).toBeVisible();
  for(const phase of ['blocked','allowed']){
   if(phase==='allowed'){
    const current=await(await page.request.get('/api/v1/delegation-policy',{headers:admin})).json();
    expect((await page.request.put('/api/v1/delegation-policy',{headers:{...admin,'If-Match':String(current.revision)},data:{...original.policy,approvalMode:'automatic'}})).status()).toBe(200);
   }
   const id='budget-'+phase;
   const graph={metadata:{id,name:id},spec:{nodes:[{id:'parent',runtime:{kind:'native'},prompt:'delegation-fixture',model:{provider:'e2e',name:'fixture'},loop:{mode:'once'},tools:[{name:'delegation.request'}]}]}};
   expect((await page.request.post('/api/v1/graphs',{headers:admin,data:graph})).status()).toBe(201);
   await page.goto('/#/g/'+id+'/canvas');await page.getByRole('button',{name:'Run all',exact:true}).click();
   let xs: {id:string;parentExecutionId?:string;status:string;usage:{inputTokens:number;outputTokens:number}}[]=[];
   await expect.poll(async()=>{xs=await(await page.request.get('/api/v1/graphs/'+id+'/executions',{headers:admin})).json();return xs.find(x=>!x.parentExecutionId)?.status;}).toBe(phase==='blocked'?'failed':'succeeded');
   const root=xs.find(x=>!x.parentExecutionId)!;
   await page.goto('/#/g/'+id+'/runs?execution='+root.id+'&section='+ (phase==='blocked'?'result':'tree'));
   if(phase==='blocked'){
    await expect(page.getByRole('region',{name:'Run detail'})).toContainText('execution tree token budget exhausted');expect(xs).toHaveLength(1);
   }else{
    expect(xs).toHaveLength(2);
    const tree=await(await page.request.get('/api/v1/executions/'+root.id+'/tree',{headers:admin})).json();
    expect(tree.budget.charged).toBe(root.usage.inputTokens+root.usage.outputTokens+tree.budget.estimated);
    expect(tree.budget.reserved).toBe(0);expect(tree.budget.estimated).toBeGreaterThan(0);
    await expect(page.getByRole('region',{name:'Tree token budget',exact:true})).toContainText('Charged: '+tree.budget.charged+' · Reserved: 0');
   }
  }
 }finally{
  const latest=await(await page.request.get('/api/v1/delegation-policy',{headers:admin})).json();
  expect((await page.request.put('/api/v1/delegation-policy',{headers:{...admin,'If-Match':String(latest.revision)},data:original.policy})).status()).toBe(200);
 }
});

test('startup recovers abandoned reservations and preserves active and retained budgets',async({page})=>{
 await page.addInitScript(()=>{if(!localStorage.getItem('weftToken'))localStorage.setItem('weftToken','e2e-admin-token');});
 for(const id of ['budget-crashed','budget-retained','budget-active']){
  await page.goto('/#/g/budget-recovery/runs?execution='+id+'&section=tree');
  const budget=page.getByRole('region',{name:'Tree token budget',exact:true});
  await expect(budget).toContainText(id==='budget-active'?'Charged: 0 · Reserved: 70 · Estimated charges: 0':'Charged: 30 · Reserved: 0 · Estimated charges: 30');
  if(id!=='budget-active')await expect(page.getByRole('region',{name:'Execution tree',exact:true})).toContainText('interrupted');
  await page.getByRole('button',{name:'Refresh tree',exact:true}).click();
  await page.reload();
  await expect(budget).toContainText(id==='budget-active'?'Reserved: 70':'Charged: 30 · Reserved: 0');
 }
 // Historical compacted totals remain readable without granting controls.
 await page.evaluate(()=>localStorage.setItem('weftToken','e2e-viewer-token'));
 await page.goto('/#/g/budget-recovery/runs?execution=budget-retained&section=tree');await page.reload();
 await expect(page.getByRole('region',{name:'Tree token budget',exact:true})).toContainText('Charged: 30 · Reserved: 0');
 await expect(page.getByRole('button',{name:'Stop entire tree',exact:true})).toHaveCount(0);
});
