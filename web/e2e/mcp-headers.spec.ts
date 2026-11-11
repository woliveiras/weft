import {test,expect} from '@playwright/test';
const headers={Authorization:'Bearer e2e-admin-token'};
for(const profile of ['dbt','langsmith'] as const){
 test('persists '+profile+' typed transport settings with write-only credentials',async({page})=>{
  await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
  await page.goto('/#/connections/new');
  const id='headers-'+profile;
  await page.getByLabel('Connection ID',{exact:true}).fill(id);
  await page.getByLabel('Connection name',{exact:true}).fill(id);
  await page.getByLabel('MCP endpoint',{exact:true}).fill('https://'+profile+'.fixture.example/mcp');
  if(profile==='langsmith'){
   await page.getByLabel('Credential type',{exact:true}).selectOption('langsmith');
   await page.getByLabel('LangSmith API key',{exact:true}).fill('fixture-mcp-token');
   await page.getByText('LangSmith workspace settings',{exact:true}).click();
   await page.getByLabel('LangSmith workspace ID',{exact:true}).fill('11111111-1111-1111-1111-111111111111');
   await page.getByLabel('LangSmith API region',{exact:true}).selectOption('https://eu.api.smith.langchain.com');
  }else{
   await page.getByLabel('Bearer token',{exact:true}).fill('fixture-mcp-token');
   await page.getByText('dbt environment settings (optional)',{exact:true}).click();
   await page.getByLabel('dbt production environment ID',{exact:true}).fill('123');
  }
  await page.getByRole('button',{name:'Save MCP connection',exact:true}).click();
  await expect(page).toHaveURL(new RegExp('connections/c/'+id+'/overview$'));
  await page.getByRole('link',{name:'Tools',exact:true}).click();
  await expect(page.getByText('mcp.'+id+'.search',{exact:true})).toBeVisible();
  const response=await page.request.get('/api/v1/mcp/connections/'+id,{headers});
  const text=await response.text();expect(text).not.toContain('fixture-mcp-token');
  const saved=JSON.parse(text);expect(saved.status).toBe('ready');expect(saved.metadataHeaders).toBeTruthy();
  expect(await page.evaluate(()=>JSON.stringify(localStorage))).not.toContain('fixture-mcp-token');
  const invalid=await page.request.put('/api/v1/mcp/connections/'+id,{headers:{...headers,'If-Match':String(saved.revision)},data:{id,name:id,service:'generic',endpoint:saved.endpoint,enabled:true,credentialType:saved.credentialType,metadataHeaders:{Host:'private.internal'},credential:{value:'fixture-mcp-token'}}});
  expect(invalid.status()).toBe(422);expect(await invalid.text()).toContain("unsupported metadata header");
  const unchanged=await (await page.request.get('/api/v1/mcp/connections/'+id,{headers})).json();expect(unchanged.metadataHeaders).toEqual(saved.metadataHeaders);expect(unchanged.revision).toBe(saved.revision);
 });
}
