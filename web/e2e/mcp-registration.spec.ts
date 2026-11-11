import {expect,test} from '@playwright/test';
// Only the disposable OAuth fixture uses self-signed TLS, as in mcp-oauth.spec.ts.
test.use({ignoreHTTPSErrors:true});
const headers={Authorization:'Bearer e2e-admin-token'};
test('reviews DCR, authorizes, reuses registration and offers manual fallback on localhost',async({page})=>{
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 const config=await (await page.request.get('/api/v1/mcp/connections/oauth-fixture-config',{headers})).json();
 await page.goto('/#/integrations');
 await page.getByLabel('Connection ID',{exact:true}).fill('dcr-browser');
 await page.getByLabel('Connection name',{exact:true}).fill('DCR browser');
 await page.getByLabel('MCP endpoint',{exact:true}).fill(config.endpoint);
 await page.getByLabel('Use browser OAuth').check();
 await page.getByLabel('Client registration method').selectOption('auto');
 await expect(page.getByLabel('OAuth client ID',{exact:true})).toHaveCount(0);
 await page.getByLabel('OAuth issuer',{exact:true}).fill(config.oauth.issuer);
 await page.getByLabel('OAuth scopes (space separated)',{exact:true}).fill('read');
 await page.getByRole('button',{name:'Save MCP connection',exact:true}).click();
 const card=page.getByRole('article',{name:'MCP DCR browser',exact:true});
 await card.getByRole('button',{name:'Review OAuth setup DCR browser',exact:true}).click();
 await expect(card).toContainText('Selected mechanism: dcr');
 const stats=async()=> (await (await page.request.get(config.oauth.issuer+'/stats')).json()).registrations;
 const before=await stats();
 await card.getByRole('button',{name:'Cancel OAuth setup',exact:true}).click();expect(await stats()).toBe(before);
 await card.getByRole('button',{name:'Review OAuth setup DCR browser',exact:true}).click();
 await card.getByRole('button',{name:'Confirm dcr and authorize DCR browser',exact:true}).click();
 await page.getByRole('button',{name:'Allow fixture access'}).click();await page.getByRole('button',{name:'Complete OAuth connection'}).click();
 await expect(page.getByRole('region',{name:'OAuth return',exact:true}).getByRole('status')).toContainText('OAuth connected');expect(await stats()).toBe(before+1);
 await expect(card).toContainText('mcp.dcr-browser.search');
 await card.getByRole('button',{name:'Revoke OAuth DCR browser',exact:true}).click();await card.getByRole('button',{name:'Confirm revoke DCR browser',exact:true}).click();
 await card.getByRole('button',{name:'Review OAuth setup DCR browser',exact:true}).click();await expect(card).toContainText('Reuse the saved client registration');
 await card.getByRole('button',{name:'Confirm dcr and authorize DCR browser',exact:true}).click();await page.getByRole('button',{name:'Deny fixture access'}).click();await page.getByRole('button',{name:'Complete OAuth connection'}).click();
 await expect(page.getByText(/authorization cancelled or denied/)).toBeVisible();expect(await stats()).toBe(before+1);
 await page.goto('/#/integrations');await card.getByRole('button',{name:'Edit DCR browser',exact:true}).click();
 await page.getByLabel('Client registration method').selectOption('cimd');await page.getByRole('button',{name:'Save MCP connection',exact:true}).click();
 await card.getByRole('button',{name:'Review OAuth setup DCR browser',exact:true}).click();await expect(card).toContainText('CIMD requires a provider-reachable public HTTPS');
 await card.getByRole('button',{name:'Edit DCR browser',exact:true}).click();await page.getByLabel('Client registration method').selectOption('manual');
 await expect(page.getByLabel('OAuth client ID',{exact:true})).toBeVisible();
});
test('registration dependency failure is visible and never automatically retried; manual remains usable',async({page})=>{
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 const config=await (await page.request.get('/api/v1/mcp/connections/oauth-fixture-config',{headers})).json();
 await page.request.post(config.oauth.issuer+'/registration-failure?enabled=true');
 try {
 const created=await page.request.post('/api/v1/mcp/connections',{headers,data:{id:'dcr-failure',name:'DCR failure',service:'generic',endpoint:config.endpoint,enabled:true,oauth:{issuer:config.oauth.issuer,clientId:'',scopes:['read'],registrationMode:'dcr'}}});expect(created.status()).toBe(201);
 await page.goto('/#/integrations');const card=page.getByRole('article',{name:'MCP DCR failure',exact:true});
 await card.getByRole('button',{name:'Review OAuth setup DCR failure'}).click();await card.getByRole('button',{name:'Confirm dcr and authorize DCR failure'}).click();
 await expect(card).toContainText('Registration rejected or unavailable');
 const count=(await (await page.request.get(config.oauth.issuer+'/stats')).json()).registrations;
 await expect(card).toContainText('Provider HTTP 503');await expect(card.getByRole('button',{name:'Confirm dcr and authorize DCR failure'})).toHaveCount(0);
 await page.reload();await expect(card).toContainText('Provider HTTP 503');await expect(card).toContainText('uncertain');await card.getByRole('button',{name:'Review OAuth setup DCR failure'}).click();await expect(card).toContainText('No automatic retry');expect((await (await page.request.get(config.oauth.issuer+'/stats')).json()).registrations).toBe(count);
 await card.getByRole('button',{name:'Edit DCR failure'}).click();await page.getByLabel('Client registration method').selectOption('manual');await page.getByLabel('OAuth client ID',{exact:true}).fill('fixture-client');await page.getByRole('button',{name:'Save MCP connection',exact:true}).click();
 await card.getByRole('button',{name:'Authorize DCR failure',exact:true}).click();await page.getByRole('button',{name:'Allow fixture access'}).click();await page.getByRole('button',{name:'Complete OAuth connection'}).click();await expect(page.getByRole('region',{name:'OAuth return',exact:true}).getByRole('status')).toContainText('OAuth connected');
 }finally {await page.request.post(config.oauth.issuer+'/registration-failure?enabled=false');}
});
