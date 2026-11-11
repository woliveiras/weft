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
 const switchGraph=async(id:string)=>{
  await page.getByRole('button',{name:'Select graph',exact:true}).click();
  const selector=page.getByRole('region',{name:'Graph selector'});
  await selector.getByLabel('Search graphs').fill(id);
  await selector.getByRole('button',{name:new RegExp(id+'.*stopped')}).click();
 };
 await switchGraph('navigation-b');
 await page.getByRole('complementary',{name:'Agents',exact:true}).getByRole('button',{name:'Configure Worker',exact:true}).click();
 await name.fill('Draft B');
 await switchGraph('navigation-a');await expect(name).toHaveValue('Draft A');
 const response=await page.request.get('/api/v1/graphs/navigation-a',{headers});const original=await response.json();
 expect(original.graph.spec.nodes[0].name).toBe('Worker');
 original.graph.metadata.description='External change';
 expect((await page.request.put('/api/v1/graphs/navigation-a',{headers:{...headers,'If-Match':String(original.record.version)},data:original.graph})).ok()).toBe(true);
 await page.getByRole('button',{name:'Select graph',exact:true}).click();
 await page.getByRole('button',{name:'Reload graphs',exact:true}).click();
 await expect(page.getByRole('button',{name:'Reload graphs',exact:true})).toBeEnabled();
 await page.getByLabel('Search graphs').press('Escape');
 await page.getByRole('button',{name:'Review & save',exact:true}).click();
 await page.getByRole('button',{name:'Save graph draft',exact:true}).click();
 await expect(page.getByText(/conflict/i).first()).toBeVisible();
 await expect(name).toHaveValue('Draft A');
 await switchGraph('navigation-b');await expect(name).toHaveValue('Draft B');
 const saved=await (await page.request.get('/api/v1/graphs/navigation-b',{headers})).json();
 expect(saved.graph.spec.nodes[0]).toMatchObject({name:'Worker',workspace:{readOnly:true},tools:[{name:'filesystem.read',paths:['src/**'],requiresApproval:true}]});
});


test('viewer navigates the agent list and selector without editing grants',async({page})=>{
 expect((await page.request.post('/api/v1/graphs',{headers,data:{metadata:{id:'navigation-viewer',name:'navigation-viewer'},spec:{nodes:[{id:'worker',name:'Worker',runtime:{kind:'echo'}}]}}})).status()).toBe(201);
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-viewer-token'));
 await page.setViewportSize({width:580,height:850});
 await page.goto('/#/g/navigation-viewer/canvas');
 await page.getByRole('button',{name:'Select graph',exact:true}).click();
 await page.getByLabel('Search graphs').fill('no-such-graph');
 await expect(page.getByText('No graphs match this search.')).toBeVisible();
 await page.getByLabel('Search graphs').press('Escape');
 const agents=page.getByRole('complementary',{name:'Agents',exact:true});
 await agents.getByLabel('Search agents').fill('Worker');
 await agents.getByRole('button',{name:'Configure Worker',exact:true}).click();
 await expect(page.getByRole('region',{name:'Agent configuration',exact:true}).getByLabel('Name',{exact:true})).toBeDisabled();
 await expect(page.getByRole('button',{name:'Review & save',exact:true})).toBeDisabled();
});
