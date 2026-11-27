import {test,expect} from '@playwright/test';
const headers={Authorization:'Bearer e2e-admin-token'};
test('switches graphs with independent drafts and preserves optimistic concurrency',async({page})=>{
 for(const id of ['navigation-a','navigation-b']){
  expect((await page.request.post('/api/v1/graphs',{headers,data:{apiVersion:'weft.io/v1',kind:'Graph',metadata:{id,name:id},spec:{nodes:[{id:'worker',name:'Worker',runtime:{kind:'native'},model:{provider:'e2e',name:'fixture'},prompt:'Assist',loop:{mode:'once'},workspace:{readOnly:true},tools:[{name:'filesystem.read',paths:['src/**'],requiresApproval:true}]}]}}})).status()).toBe(201);
 }
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 await page.goto('/#/g/navigation-a/canvas');
 await page.getByRole('complementary',{name:'Agents',exact:true}).getByRole('button',{name:'Configure Worker',exact:true}).click();
 const name=page.getByRole('region',{name:'Agent configuration',exact:true}).getByLabel('Name',{exact:true});
 await name.fill('Draft A');
 const openWorker=async(id:string)=>{
  await page.getByRole('button',{name:'Workflows',exact:true}).click();
  await page.getByLabel('Search workflows').fill(id);
  const row=page.getByRole('row').filter({has:page.getByRole('button',{name:id,exact:true})});
  await row.getByText('1 agent', {exact:false}).first().click();
  await row.getByRole('button',{name:'Configure Worker'}).click();
 };
 await openWorker('navigation-b');
 await name.fill('Draft B');
 await openWorker('navigation-a');await expect(name).toHaveValue('Draft A');
 const response=await page.request.get('/api/v1/graphs/navigation-a',{headers});const original=await response.json();
 expect(original.graph.spec.nodes[0].name).toBe('Worker');
 original.graph.metadata.description='External change';
 expect((await page.request.put('/api/v1/graphs/navigation-a',{headers:{...headers,'If-Match':String(original.record.version)},data:original.graph})).ok()).toBe(true);
 await page.getByRole('button',{name:'Workflows',exact:true}).click();
 await page.getByRole('button',{name:'Refresh workflows'}).click();
 await expect(page.getByText('External change',{exact:true})).toBeVisible();
 await openWorker('navigation-a');
 await page.getByRole('button',{name:'Review & save',exact:true}).click();
 await page.getByRole('button',{name:'Save graph draft',exact:true}).click();
 await expect(page.getByText(/conflict/i).first()).toBeVisible();
 await expect(name).toHaveValue('Draft A');
 await openWorker('navigation-b');await expect(name).toHaveValue('Draft B');
 const saved=await (await page.request.get('/api/v1/graphs/navigation-b',{headers})).json();
 expect(saved.graph.spec.nodes[0]).toMatchObject({name:'Worker',workspace:{readOnly:true},tools:[{name:'filesystem.read',paths:['src/**'],requiresApproval:true}]});
});


test('viewer navigates the agent list and workflow catalogue without editing grants',async({page})=>{
 expect((await page.request.post('/api/v1/graphs',{headers,data:{metadata:{id:'navigation-viewer',name:'navigation-viewer'},spec:{nodes:[{id:'worker',name:'Worker',runtime:{kind:'echo'}}]}}})).status()).toBe(201);
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-viewer-token'));
 await page.setViewportSize({width:580,height:850});
 await page.goto('/#/g/navigation-viewer/canvas');
 await expect(page.getByRole('banner').getByRole('button',{name:'Select graph'})).toHaveCount(0);
 await page.getByRole('button',{name:'Workflows',exact:true}).click();
 await page.getByLabel('Search workflows').fill('no-such-graph');
 await expect(page.getByText('No workflows match these filters.')).toBeVisible();
 await page.getByLabel('Search workflows').fill('navigation-viewer');
 await page.getByRole('button',{name:'navigation-viewer',exact:true}).click();
 const agents=page.getByRole('complementary',{name:'Agents',exact:true});
 await agents.getByLabel('Search agents').fill('Worker');
 await agents.getByRole('button',{name:'Configure Worker',exact:true}).click();
 await expect(page.getByRole('region',{name:'Agent configuration',exact:true}).getByLabel('Name',{exact:true})).toBeDisabled();
 await expect(page.getByRole('button',{name:'Review & save',exact:true})).toBeDisabled();
});
