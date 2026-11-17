import {expect,test} from '@playwright/test';
// Only the disposable OAuth fixture uses self-signed TLS, as in mcp-oauth.spec.ts.
test.use({ignoreHTTPSErrors:true});
test('hosted CIMD publishes public metadata and completes consent without DCR',async({page})=>{
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 const config=await (await page.request.get('http://127.0.0.1:18090/api/v1/mcp/connections/oauth-fixture-config',{headers:{Authorization:'Bearer e2e-admin-token'}})).json();
 await page.goto('/#/integrations');
 await page.getByLabel('Connection ID',{exact:true}).fill('cimd-browser');await page.getByLabel('Connection name',{exact:true}).fill('CIMD browser');
 await page.getByLabel('MCP endpoint',{exact:true}).fill(config.endpoint);await page.getByLabel('Use browser OAuth').check();await page.getByLabel('Client registration method').selectOption('auto');
 await page.getByLabel('OAuth issuer',{exact:true}).fill(config.oauth.issuer);await page.getByLabel('OAuth scopes (space separated)',{exact:true}).fill('read');await page.getByRole('button',{name:'Save MCP connection',exact:true}).click();
 const card=page.getByRole('article',{name:'MCP CIMD browser',exact:true});await card.getByRole('button',{name:'Review OAuth setup CIMD browser'}).click();await expect(card).toContainText('Selected mechanism: cimd');
 await card.getByRole('button',{name:'Confirm cimd and authorize CIMD browser'}).click();await page.getByRole('button',{name:'Allow fixture access'}).click();await page.getByRole('button',{name:'Complete OAuth connection'}).click();await expect(page.getByRole('region',{name:'OAuth return',exact:true}).getByRole('status')).toContainText('OAuth connected');
 const response=await page.request.get('http://127.0.0.1:18090/api/v1/mcp/oauth/client/cimd-browser');expect(response.status()).toBe(200);const doc=await response.json();expect(doc.client_id).toBe('https://weft.fixture.example:18091/api/v1/mcp/oauth/client/cimd-browser');expect(JSON.stringify(doc)).not.toContain('secret');
 const stats=await (await page.request.get(config.oauth.issuer+'/stats')).json();expect(stats.registrations).toBe(0);expect(stats.refreshes).toBeGreaterThan(0);
 await card.getByRole('button',{name:'Revoke OAuth CIMD browser'}).click();await card.getByRole('button',{name:'Confirm revoke CIMD browser'}).click();await expect(card).toContainText('OAuth: revoked');
});
