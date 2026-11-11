import { expect,test } from '@playwright/test';
const headers={Authorization:'Bearer e2e-admin-token'};
// Only the disposable OAuth provider uses a self-signed TLS certificate.
test.use({ignoreHTTPSErrors:true});
test('connects from a template link through OAuth consent, renewal and revocation',async({page})=>{
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 const config=await (await page.request.get('/api/v1/mcp/connections/oauth-fixture-config',{headers})).json();
 await page.goto('/#/integrations/connect?template=deepwiki&version=1');
 await page.getByRole('button',{name:'Use template defaults'}).click();
 await page.getByRole('button',{name:'Customize without template'}).click();
 await page.getByLabel('Connection ID',{exact:true}).fill('oauth-browser');await page.getByLabel('Connection name',{exact:true}).fill('Browser OAuth');
 await page.getByLabel('MCP endpoint',{exact:true}).fill(config.endpoint);
 await page.getByLabel('Use browser OAuth').check();
 await page.getByLabel('OAuth issuer',{exact:true}).fill(config.oauth.issuer);await page.getByLabel('OAuth client ID',{exact:true}).fill('fixture-client');await page.getByLabel('OAuth scopes (space separated)',{exact:true}).fill('read');
 await page.getByRole('button',{name:'Save MCP connection',exact:true}).click();
 await page.getByRole('button',{name:'Authorize Browser OAuth',exact:true}).click();
 await expect(page.getByRole('heading',{name:'Fixture OAuth consent'})).toBeVisible();
 await page.getByRole('button',{name:'Allow fixture access'}).click();
 await expect(page).toHaveURL(/#\/integrations\/oauth\?state=/);expect(page.url()).not.toContain('code=');
 await page.reload();
 await page.getByRole('button',{name:'Complete OAuth connection'}).click();
 await expect(page.getByRole('region',{name:'OAuth return',exact:true}).getByRole('status')).toContainText('OAuth connected.');
 const card=page.getByRole('article',{name:'MCP Browser OAuth',exact:true});await expect(card).toContainText('mcp.oauth-browser.search');await expect(card).toContainText('OAuth: authorized');
 const stats=await (await page.request.get(config.oauth.issuer+'/stats')).json();expect(stats.refreshes).toBeGreaterThan(0);
 const saved=await page.request.get('/api/v1/mcp/connections/oauth-browser',{headers});const raw=await saved.text();expect(raw).not.toContain('fixture-mcp-token');expect(raw).not.toContain('fixture-refresh-token');
 const graphID='oauth-browser-execution';
 const graph=await page.request.post('/api/v1/graphs',{headers,data:{metadata:{id:graphID,name:graphID},spec:{nodes:[{id:'worker',runtime:{kind:'native'},model:{provider:'e2e',name:'fixture'},isolation:{mode:'process'},loop:{mode:'once'},prompt:'mcp-oauth-call-fixture',tools:[]}]}}});expect(graph.status()).toBe(201);
 await page.goto(`/#/g/${graphID}/canvas?sel=node:worker`);
 const grants=page.getByRole('region',{name:'MCP permissions',exact:true});
 const permission=grants.getByRole('checkbox',{name:'mcp.oauth-browser.search',exact:true});await expect(permission).not.toBeChecked();await permission.check();
 await Promise.all([page.waitForResponse(r=>r.url().endsWith('/api/v1/graphs/'+graphID)&&r.request().method()==='PUT'&&r.status()===200),page.getByRole('button',{name:'Save graph',exact:true}).click()]);
 await expect(page.getByRole('button',{name:'Save graph',exact:true})).toBeDisabled();await page.getByRole('button',{name:'Run all',exact:true}).click();
 await expect.poll(async()=>{const events=await (await page.request.get(`/api/v1/graphs/${graphID}/events`,{headers})).json();return events.some((e:{type:string;data:{tool?:string;outcome?:string}})=>e.type==='tool.completed'&&e.data.tool==='mcp.oauth-browser.search'&&e.data.outcome==='ok');}).toBe(true);
 await page.goto('/#/integrations');
 await card.getByRole('button',{name:'Revoke OAuth Browser OAuth',exact:true}).click();
 await card.getByRole('button',{name:'Confirm revoke Browser OAuth',exact:true}).click();await expect(card).toContainText('OAuth: revoked');await expect(card).toContainText('Provider revocation: requested');await expect(card).not.toContainText('mcp.oauth-browser.search');
 await page.reload();await expect(card).toContainText('OAuth: revoked');
 await page.goto(`/#/g/${graphID}/canvas`);await page.getByRole('button',{name:'Stop all',exact:true}).click();await expect(page.getByRole('button',{name:'Run all',exact:true})).toBeEnabled();await page.getByRole('button',{name:'Run all',exact:true}).click();
 await expect.poll(async()=>{const events=await (await page.request.get(`/api/v1/graphs/${graphID}/events`,{headers})).json();return events.some((e:{type:string;data:{tool?:string}})=>e.type==='tool.denied'&&e.data.tool==='mcp.oauth-browser.search');}).toBe(true);
});
test('handles denied consent, renewal failure and unsuccessful upstream revocation',async({page})=>{
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 const config=await (await page.request.get('/api/v1/mcp/connections/oauth-fixture-config',{headers})).json();
 for(const [id,client] of [['oauth-denied','fixture-client'],['oauth-renew-failed','fail-refresh'],['oauth-revoke-failed','fail-revoke']]){
  const created=await page.request.post('/api/v1/mcp/connections',{headers,data:{id,name:id,service:'generic',enabled:true,endpoint:config.endpoint,oauth:{...config.oauth,clientId:client,status:undefined,clientSecretConfigured:undefined}}});expect(created.status()).toBe(201);
  await page.goto('/#/integrations');await page.reload();await page.getByRole('button',{name:'Authorize '+id,exact:true}).click();
  await page.getByRole('button',{name:id==='oauth-denied'?'Deny fixture access':'Allow fixture access'}).click();
  await page.getByRole('button',{name:'Complete OAuth connection'}).click();
  if(id==='oauth-denied'){await expect(page.getByText(/authorization cancelled or denied by provider/)).toBeVisible();}
  else if(id==='oauth-renew-failed'){await expect(page.getByText(/OAuth request rejected/)).toBeVisible();await page.reload();await expect(page.getByRole('article',{name:'MCP '+id,exact:true})).toContainText('reauthorization_required');}
  else{await expect(page.getByRole('region',{name:'OAuth return',exact:true}).getByRole('status')).toContainText('OAuth connected.');const card=page.getByRole('article',{name:'MCP '+id,exact:true});await card.getByRole('button',{name:'Revoke OAuth '+id,exact:true}).click();await card.getByRole('button',{name:'Confirm revoke '+id,exact:true}).click();await expect(card).toContainText('Provider revocation: failed');await expect(card).toContainText('OAuth: revoked');}
 }
 await page.goto('/api/v1/mcp/oauth/callback?state=invalid&code=synthetic');await expect(page.getByText(/Invalid OAuth return/)).toBeVisible();
});

test('GitHub contract: confidential consent, comma scopes, renewal, errors and local revocation',async({page})=>{
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 const config=await (await page.request.get('/api/v1/mcp/connections/github-oauth-fixture-config',{headers})).json();
 // Browser consent is served by our TLS fixture, never by a real GitHub account.
 await page.route('https://github.com/login/oauth/authorize?**',async route=>{
  const response=await route.fetch({url:config.name+'/login/oauth/authorize'+new URL(route.request().url()).search,maxRedirects:0});
  await route.fulfill({response});
 });
 await page.goto('/#/integrations');
 await page.getByLabel('Connection ID',{exact:true}).fill('github-oauth-browser');
 await page.getByLabel('Connection name',{exact:true}).fill('GitHub OAuth browser');
 await page.getByRole('combobox',{name:/^Service/}).selectOption('github');
 await page.getByLabel('MCP endpoint',{exact:true}).fill(config.endpoint);
 await page.getByLabel('Use browser OAuth').check();
 await page.getByLabel('OAuth issuer',{exact:true}).fill(config.oauth.issuer);
 await page.getByLabel('OAuth client ID',{exact:true}).fill('fixture-client');
 await page.getByLabel('OAuth scopes (space separated)',{exact:true}).fill('read:user read:org');
 await page.getByRole('button',{name:'Save MCP connection',exact:true}).click();
 await page.getByRole('button',{name:'Authorize GitHub OAuth browser',exact:true}).click();
 await expect(page.getByText(/GitHub OAuth requires a registered client secret/)).toBeVisible();
 await page.getByRole('button',{name:'Edit GitHub OAuth browser',exact:true}).click();
 await page.getByLabel('OAuth client secret (optional)',{exact:true}).fill('wrong-fixture-secret');
 await page.getByRole('button',{name:'Save MCP connection',exact:true}).click();
 await page.getByRole('button',{name:'Authorize GitHub OAuth browser',exact:true}).click();
 await page.getByRole('button',{name:'Allow fixture access'}).click();
 await page.getByRole('button',{name:'Complete OAuth connection'}).click();
 await expect(page.getByText(/OAuth client registration rejected/)).toBeVisible();
 await page.goto('/#/integrations'); await page.reload();
 await page.getByRole('button',{name:'Edit GitHub OAuth browser',exact:true}).click();
 await page.getByLabel('OAuth client secret (optional)',{exact:true}).fill('fixture-client-secret');
 await page.getByRole('button',{name:'Save MCP connection',exact:true}).click();
 await page.getByRole('button',{name:'Authorize GitHub OAuth browser',exact:true}).click();
 await page.getByRole('button',{name:'Allow fixture access'}).click();
 await page.getByRole('button',{name:'Complete OAuth connection'}).click();
 await expect(page.getByRole('region',{name:'OAuth return',exact:true}).getByRole('status')).toContainText('OAuth connected.');
 const card=page.getByRole('article',{name:'MCP GitHub OAuth browser',exact:true});
 await expect(card).toContainText('mcp.github-oauth-browser.search');
 await expect(card).toContainText('read:user'); await expect(card).toContainText('read:org');
 const stats=await (await page.request.get(config.name+'/stats')).json(); expect(stats.refreshes).toBeGreaterThan(0);
 const raw=await (await page.request.get('/api/v1/mcp/connections/github-oauth-browser',{headers})).text();
 for(const secret of ['fixture-client-secret','fixture-mcp-token','fixture-refresh-token']) expect(raw).not.toContain(secret);
 await card.getByRole('button',{name:'Revoke OAuth GitHub OAuth browser',exact:true}).click();
 await card.getByRole('button',{name:'Confirm revoke GitHub OAuth browser',exact:true}).click();
 await expect(card).toContainText('OAuth: revoked'); await expect(card).toContainText('Provider revocation: unsupported');
 await expect(card).not.toContainText('mcp.github-oauth-browser.search');
});

test('keeps personal, corporate and GitLab OAuth connections and agent grants independent',async({page})=>{
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 const configurations:Record<string,{name:string;endpoint:string;oauth:{issuer:string}}>= {};
 for(const service of ['github','atlassian','gitlab']){
  const config=await (await page.request.get(`/api/v1/mcp/connections/${service}-oauth-fixture-config`,{headers})).json();configurations[service]=config;
  const authorization=service==='github'?'https://github.com/login/oauth/authorize':service==='atlassian'?'https://auth.atlassian.com/authorize':'https://gitlab.fixture.example/oauth/authorize';
  await page.route(authorization+'?**',async route=>{
   const url=new URL(route.request().url());const response=await route.fetch({url:config.name+url.pathname+url.search,maxRedirects:0});await route.fulfill({response});
  });
 }
 const connections=[['personal-mcp','Personal GitHub','github'],['work-mcp','Work Atlassian','atlassian'],['work-two-mcp','Other Work Atlassian','atlassian'],['gitlab-mcp','Team GitLab','gitlab']];
 for(const [id,name,service] of connections){
  await page.goto('/#/integrations');await page.reload();
  await page.getByRole('combobox',{name:/^Service/}).selectOption(service);
  if(service==='gitlab')await page.getByLabel('GitLab instance URL').fill('https://gitlab.fixture.example');
  await page.getByRole('button',{name:'Use OAuth defaults',exact:true}).click();
  await expect(page.getByLabel('MCP endpoint',{exact:true})).toHaveValue(configurations[service].endpoint+(service==='atlassian'?'?tools=all':''));
  await expect(page.getByText(new URL('/api/v1/mcp/oauth/callback',page.url()).href,{exact:true})).toBeVisible();
  await page.getByLabel('Connection ID',{exact:true}).fill(id);await page.getByLabel('Connection name',{exact:true}).fill(name);
  await page.getByLabel('OAuth client ID',{exact:true}).fill(id);
  if(service==='github'){
   await page.getByLabel('OAuth client secret (optional)',{exact:true}).fill('fixture-client-secret');
   await page.getByLabel('OAuth scopes (space separated)',{exact:true}).fill('read:user read:org');
  }
  await page.getByRole('button',{name:'Save MCP connection',exact:true}).click();
  await page.getByRole('button',{name:'Authorize '+name,exact:true}).click();
  await page.getByRole('button',{name:'Allow fixture access'}).click();
  await expect(page).toHaveURL(/#\/integrations\/oauth\?state=/);
  await page.getByRole('button',{name:'Complete OAuth connection'}).click();
  await expect(page.getByRole('region',{name:'OAuth return',exact:true}).getByRole('status')).toContainText('OAuth connected.');
  await expect(page.getByRole('article',{name:'MCP '+name,exact:true})).toContainText(`mcp.${id}.${service==='atlassian'?'getJiraIssue':'search'}`);
 }
 const before=await (await page.request.get('/api/v1/mcp/connections/personal-mcp',{headers})).json();
 const graphID='multi-mcp-grants';
 expect((await page.request.post('/api/v1/graphs',{headers,data:{metadata:{id:graphID,name:graphID},spec:{nodes:[{id:'worker',runtime:{kind:'native'},model:{provider:'e2e',name:'fixture'},isolation:{mode:'process'},loop:{mode:'once'},prompt:'test',tools:[]}]}}})).status()).toBe(201);
 await page.goto(`/#/g/${graphID}/canvas?sel=node:worker`);
 const grants=page.getByRole('region',{name:'MCP permissions',exact:true});
 for(const [id,,service] of connections)await expect(grants.getByRole('checkbox',{name:`mcp.${id}.${service==='atlassian'?'getJiraIssue':'search'}`,exact:true})).not.toBeChecked();
 await grants.getByRole('checkbox',{name:'mcp.personal-mcp.search',exact:true}).check();
 await Promise.all([page.waitForResponse(r=>r.url().endsWith('/api/v1/graphs/'+graphID)&&r.request().method()==='PUT'&&r.status()===200),page.getByRole('button',{name:'Save graph',exact:true}).click()]);
 await page.goto('/#/integrations');
 const work=page.getByRole('article',{name:'MCP Work Atlassian',exact:true});
 await work.getByRole('button',{name:'Revoke OAuth Work Atlassian',exact:true}).click();await work.getByRole('button',{name:'Confirm revoke Work Atlassian',exact:true}).click();
 await expect(work).toContainText('OAuth: revoked');await expect(work).not.toContainText('mcp.work-mcp.getJiraIssue');
 for(const name of ['Personal GitHub','Other Work Atlassian','Team GitLab'])await expect(page.getByRole('article',{name:'MCP '+name,exact:true})).toContainText('OAuth: authorized');
 expect(await (await page.request.get('/api/v1/mcp/connections/personal-mcp',{headers})).json()).toEqual(before);
 await work.getByRole('button',{name:'Reauthorize Work Atlassian',exact:true}).click();await page.getByRole('button',{name:'Deny fixture access'}).click();await page.getByRole('button',{name:'Complete OAuth connection'}).click();await expect(page.getByText(/authorization cancelled or denied/)).toBeVisible();
 await page.goto('/#/integrations');await page.reload();await expect(page.getByRole('article',{name:'MCP Personal GitHub',exact:true})).toContainText('OAuth: authorized');
 const stored=await page.evaluate(()=>JSON.stringify({local:{...localStorage},session:{...sessionStorage}}));
 for(const secret of ['fixture-client-secret','fixture-refresh-token','fixture-mcp-token'])expect(stored).not.toContain(secret);
});
