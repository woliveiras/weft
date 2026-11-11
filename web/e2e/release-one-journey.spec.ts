import {test,expect} from '@playwright/test';
const headers={Authorization:'Bearer e2e-admin-token'};
for(const mode of ['required','automatic'] as const){
 test('integrated MCP, environment, delegation and file review with '+mode+' approval',async({page})=>{
  const id='journey-'+mode;
  const original=await(await page.request.get('/api/v1/delegation-policy',{headers})).json();
  try {
   await page.goto('/#/integrations');
   await page.getByLabel('API token',{exact:true}).fill('e2e-admin-token');
   await Promise.all([
    page.waitForEvent('load'),
    page.getByRole('button',{name:'Continue',exact:true}).click(),
   ]);
   await expect(page.getByRole('dialog',{name:'Sign in'})).toHaveCount(0);
   await page.goto('/#/administration/delegation');
   await page.getByRole('combobox',{name:'Approval mode',exact:true}).selectOption(mode);
   await page.getByRole('button',{name:'Review delegation policy',exact:true}).click();
   await page.getByRole('button',{name:'Confirm delegation policy',exact:true}).click();
   await expect(page.getByText('Delegation policy saved. New requests use these settings.')).toBeVisible();
   await page.goto('/#/integrations');
   const connections=page.getByRole('region',{name:'MCP connections',exact:true});
   await connections.getByLabel('Connection ID',{exact:true}).fill(id);
   await connections.getByLabel('Connection name',{exact:true}).fill(id);
   await connections.getByLabel('MCP endpoint',{exact:true}).fill('https://mcp.fixture.invalid/mcp');
   await connections.getByLabel('Bearer token',{exact:true}).fill('fixture-mcp-token');
   await connections.getByRole('button',{name:'Save MCP connection',exact:true}).click();
   await expect(connections.getByRole('article',{name:'MCP '+id,exact:true})).toContainText('mcp.'+id+'.search');
   const before=await(await page.request.get('/api/v1/graphs/'+id,{headers})).json();
   expect(before.graph.spec.nodes[0].tools.some((g:{name:string})=>g.name.startsWith('mcp.'))).toBe(false);
   await page.goto('/#/g/'+id+'/canvas?sel=node:worker');
   await page.getByRole('region',{name:'MCP permissions',exact:true}).getByRole('checkbox',{name:'mcp.'+id+'.search',exact:true}).check();
   await page.getByRole('button',{name:'Save graph',exact:true}).click();
   await expect(page.getByRole('button',{name:'Save graph',exact:true})).toBeDisabled();
   await page.getByRole('button',{name:'Graph settings',exact:true}).click();
   await page.getByRole('button',{name:'Discover environment',exact:true}).click();
   await page.getByRole('combobox',{name:'Repository definition',exact:true}).selectOption({label:'Dockerfile'});
   await page.getByRole('button',{name:'Review environment',exact:true}).click();
   await page.getByRole('button',{name:'Accept into draft',exact:true}).click();
   await page.getByRole('button',{name:'Save graph',exact:true}).click();
   await page.getByRole('button',{name:'Build',exact:true}).click();
   await expect.poll(async()=>(await(await page.request.get('/api/v1/graphs/'+id+'/environment',{headers})).json()).state).toBe('ready');
   await page.getByRole('button',{name:'Run all',exact:true}).click();
   await page.goto('/#/approvals');
   if(mode==='required'){
    const child=page.locator('.approval').filter({hasText:id}).filter({hasText:'tool-free analysis'});
    await expect(child).toBeVisible();
    await child.getByRole('button',{name:'Approve',exact:true}).click();
   }
   const write=page.locator('.approval').filter({hasText:id}).filter({hasText:'filesystem.write'});
   await expect(write).toBeVisible();
   const content=await page.request.get('/api/v1/graphs/'+id+'/files/content?path=journey.txt',{headers});
   expect(content.ok()).toBe(true);expect(await content.text()).toContain('Baseline journey');
   await write.getByRole('button',{name:'Approve',exact:true}).click();
   let root:{id:string;status:string}|undefined;
   await expect.poll(async()=>{
    const xs=await(await page.request.get('/api/v1/graphs/'+id+'/executions',{headers})).json();
    root=xs.find((x:{parentExecutionId?:string})=>!x.parentExecutionId);return root?.status;
   }).toBe('succeeded');
   await page.goto('/#/g/'+id+'/runs?execution='+root!.id+'&section=result');
   await expect(page.getByRole('region',{name:'Run detail'})).toContainText('Integrated journey completed');
   await page.getByRole('tab',{name:'Files',exact:true}).click();
   await page.getByRole('button',{name:/journey.txt/}).click();
   await expect(page.getByRole('region',{name:'File contents'})).toContainText('Reviewed fixture result');
   const git=page.getByRole('region',{name:'Git inspection'});
   await git.getByLabel('Git view').selectOption('diff');
   await expect(git).toContainText('+Reviewed fixture result');
   await expect(git).toContainText('-Baseline journey');
   const approvals=await(await page.request.get('/api/v1/approvals?graph='+id,{headers})).json();
   expect(approvals.filter((a:{kind:string})=>a.kind==='delegation')).toHaveLength(mode==='required'?1:0);
   const saved=await(await page.request.get('/api/v1/graphs/'+id,{headers})).json();
   expect(saved.graph.spec.nodes).toHaveLength(1);
   expect(saved.graph.spec.nodes[0].tools).toContainEqual({name:'filesystem.write',paths:['journey.txt'],requiresApproval:true});
   expect(JSON.stringify(await(await page.request.get('/api/v1/mcp/connections',{headers})).json())).not.toContain('fixture-mcp-token');
  }finally{
   const latest=await(await page.request.get('/api/v1/delegation-policy',{headers})).json();
   expect((await page.request.put('/api/v1/delegation-policy',{headers:{...headers,'If-Match':String(latest.revision)},data:original.policy})).status()).toBe(200);
  }
 });
}
