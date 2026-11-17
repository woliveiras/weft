import {test,expect} from '@playwright/test';
const headers={Authorization:'Bearer e2e-admin-token'};
for(const [status,guidance] of [[401,'reauthorize OAuth or replace its credential'],[403,'A 403 alone does not identify the cause'],[429,'separate from the Weft connection quota']] as const){
 test(`MCP ${status} guidance survives reload without upstream secrets or automatic retries`,async({page})=>{
  await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
  await page.goto('/#/integrations');
  const form=page.getByRole('region',{name:'MCP connections',exact:true});
  await form.getByLabel('Connection ID',{exact:true}).fill('access-'+status);
  await form.getByLabel('Connection name',{exact:true}).fill('Access '+status);
  await form.getByLabel('MCP endpoint',{exact:true}).fill(`https://access-${status}.fixture.invalid/mcp`);
  await form.getByLabel('Bearer token',{exact:true}).fill('fixture-mcp-token');
  await form.getByRole('button',{name:'Save MCP connection',exact:true}).click();
  const card=form.getByRole('article',{name:'MCP Access '+status,exact:true});
  await expect(card).toContainText(guidance);
  await page.reload();
  await expect(card).toContainText(guidance);
  await expect(card).not.toContainText('private-detail');
  await expect(card).not.toContainText('fixture-mcp-token');
  const response=await page.request.get('/api/v1/mcp/connections',{headers});
  expect(response.status()).toBe(200);
  const data=await response.text();expect(data).not.toContain('fixture-mcp-token');expect(data).not.toContain('private-detail');
  const connection=JSON.parse(data).find((c:{id:string})=>c.id==='access-'+status);
  expect(connection.tools).toEqual([]);expect(connection.status).toBe('unavailable');
 });
}
