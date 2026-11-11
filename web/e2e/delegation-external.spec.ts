import {test,expect} from '@playwright/test';
const headers={Authorization:'Bearer e2e-admin-token'};
for(const decision of ['approve','reject','cancel'] as const){
 test('external runtime '+decision+' returns a native child outcome through the callback',async({page})=>{
  const id='external-parent-'+decision;
  // Each scenario explicitly selects required approval in the disposable fixture.
  const policy=await (await page.request.get('/api/v1/delegation-policy',{headers})).json();
  expect((await page.request.put('/api/v1/delegation-policy',{headers:{...headers,'If-Match':String(policy.revision)},data:{...policy.policy,approvalMode:'required'}})).ok()).toBeTruthy();
  await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
  await page.goto('/#/g/'+id+'/canvas');
  await page.getByRole('button',{name:'Run all',exact:true}).click();
  let approval:{id:string;executionId:string;detail:{parentExecutionId:string}}|undefined;
  await expect.poll(async()=>{
   const all=await(await page.request.get('/api/v1/approvals?graph='+id,{headers})).json();
   approval=all.find((a:{kind:string;status:string})=>a.kind==='delegation'&&a.status==='pending');return !!approval;
  }).toBe(true);
  const parent=approval!.detail.parentExecutionId,child=approval!.executionId;
  if(decision==='cancel'){
   expect((await page.request.post('/api/v1/executions/'+parent+'/cancel',{headers,data:{scope:'tree',confirm:true}})).ok()).toBeTruthy();
  }else{
   await page.getByRole('button',{name:/^Approvals/}).click();
   await page.locator('.approval').filter({hasText:id}).getByRole('button',{name:decision==='approve'?'Approve':'Reject',exact:true}).click();
  }
  await expect.poll(async()=>(await(await page.request.get('/api/v1/executions/'+parent,{headers})).json()).status).toBe(decision==='cancel'?'cancelled':'succeeded');
  await page.goto('/#/g/'+id+'/runs?execution='+child+'&section=result');
  await expect(page.getByRole('complementary',{name:'Delegation context'})).toBeVisible();
  if(decision!=='cancel') await expect(page.getByRole('region',{name:'Run detail'})).toContainText(decision==='approve'?'Delegated fixture result':'delegation rejected or expired');
  const saved=await(await page.request.get('/api/v1/graphs/'+id,{headers})).json();
  expect(saved.record.version).toBe(1);expect(saved.graph.spec.nodes).toHaveLength(1);expect(saved.graph.spec.nodes[0].runtime.kind).toBe('external');expect(saved.graph.spec.nodes[0].tools).toEqual([{name:'delegation.request'}]);
 });
}
