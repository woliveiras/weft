import { expect,test } from '@playwright/test';
const headers={Authorization:'Bearer e2e-admin-token'};
test('registers a remote MCP, discovers tools and persists exact agent permissions',async({page})=>{
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 await page.goto('/#/integrations');const section=page.getByRole('region',{name:'MCP connections',exact:true});
 await section.getByLabel('Connection ID',{exact:true}).fill('team');await section.getByLabel('Connection name',{exact:true}).fill('Team MCP');await section.getByLabel('MCP endpoint',{exact:true}).fill('https://mcp.fixture.invalid/mcp');await section.getByLabel('Bearer token',{exact:true}).fill('fixture-mcp-token');
 await section.getByRole('button',{name:'Save MCP connection',exact:true}).click();
 await section.getByRole('button',{name:'Refresh tools for Team MCP',exact:true}).click();await expect(section).toContainText('mcp.team.search');await expect(section).toContainText('mcp.team.read_issue');
 const response=await page.request.get('/api/v1/mcp/connections',{headers});expect(response.ok()).toBeTruthy();expect(await response.text()).not.toContain('fixture-mcp-token');
 await page.goto('/#/g/journal-capture/canvas?sel=node:worker');
 const grants=page.getByRole('region',{name:'MCP permissions',exact:true});
 await grants.getByRole('checkbox',{name:'mcp.team.search',exact:true}).check();await Promise.all([page.waitForResponse(r=>r.url().endsWith('/api/v1/graphs/journal-capture')&&r.request().method()==='PUT'&&r.status()===200),page.getByRole('button',{name:'Save graph',exact:true}).click()]);
 await expect(page.getByRole('button',{name:'Save graph',exact:true})).toBeDisabled();await page.reload();await expect(grants.getByRole('checkbox',{name:'mcp.team.search',exact:true})).toBeChecked();
 const graph=await page.request.get('/api/v1/graphs/journal-capture',{headers});const saved=await graph.json();expect(saved.graph.spec.nodes[0].tools).toContainEqual({name:'mcp.team.search'});
 await page.goto('/#/integrations');await section.getByRole('button',{name:'Edit Team MCP',exact:true}).click();await expect(section.getByLabel('Bearer token',{exact:true})).toHaveValue('');await section.getByLabel('Connection enabled',{exact:true}).uncheck();await section.getByRole('button',{name:'Save MCP connection',exact:true}).click();await expect(section.getByRole('button',{name:'Refresh tools for Team MCP',exact:true})).toBeDisabled();
 await page.goto('/#/g/journal-capture/canvas?sel=node:worker');await expect(grants).toContainText('Unavailable in the current catalogue');await grants.getByRole('checkbox',{name:/mcp.team.search/}).click();await expect(grants.getByRole('checkbox',{name:/mcp.team.search/})).toHaveCount(0);await Promise.all([page.waitForResponse(r=>r.url().endsWith('/api/v1/graphs/journal-capture')&&r.request().method()==='PUT'&&r.status()===200),page.getByRole('button',{name:'Save graph',exact:true}).click()]);await expect(page.getByRole('button',{name:'Save graph',exact:true})).toBeDisabled();
});
test('reports MCP authentication failure without a discovered tool catalogue', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.goto('/#/integrations');
  const section = page.getByRole('region', { name: 'MCP connections', exact: true });
  await section.getByLabel('Connection ID', { exact: true }).fill('noauth');
  await section.getByLabel('Connection name', { exact: true }).fill('No auth');
  await section.getByLabel('MCP endpoint', { exact: true }).fill('https://mcp.fixture.invalid/mcp');
  await section.getByRole('button', { name: 'Save MCP connection', exact: true }).click();

  const card = section.getByRole('article', { name: 'MCP No auth', exact: true });
  const [response] = await Promise.all([
    page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/mcp/connections/noauth/test' && r.request().method() === 'POST'),
    card.getByRole('button', { name: 'Test No auth', exact: true }).click(),
  ]);
  expect(response.status()).toBe(200);
  expect(await response.json()).toMatchObject({ status: 'unavailable', tools: [] });
  await expect(card.getByRole('alert')).toHaveText(
    'MCP credentials rejected (401). Open the connection and reauthorize OAuth or replace its credential, then test the connection.',
  );
  await expect(card.getByRole('listitem')).toHaveCount(0);
});

test('executes MCP through the agent gateway and denies ungranted and disabled tools',async({page})=>{
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 const create=await page.request.post('/api/v1/mcp/connections',{headers,data:{id:'runtime',name:'Runtime',service:'generic',endpoint:'https://mcp.fixture.invalid/mcp',enabled:true,credential:{value:'fixture-mcp-token'}}});expect(create.status()).toBe(201);
 const refresh=await page.request.post('/api/v1/mcp/connections/runtime/tools/refresh',{headers});expect(refresh.ok()).toBeTruthy();const connection=await refresh.json();
 for(const [id,tools] of [['mcp-allowed',[{name:'mcp.runtime.search'}]],['mcp-denied',[{name:'*'}]]] as const){
  const created=await page.request.post('/api/v1/graphs',{headers,data:{metadata:{id,name:id},spec:{nodes:[{id:'worker',runtime:{kind:'native'},model:{provider:'e2e',name:'fixture'},isolation:{mode:'process'},loop:{mode:'once'},prompt:'mcp-call-fixture',tools}]}}});expect(created.status(),await created.text()).toBe(201);
  await page.goto(`/#/g/${id}/canvas`);await page.getByRole('button',{name:'Run all',exact:true}).click();
  await expect.poll(async()=>{const res=await page.request.get(`/api/v1/graphs/${id}/events`,{headers});return (await res.json()).some((e:{type:string;data:{tool?:string;outcome?:string}})=>e.type===(id==='mcp-allowed'?'tool.completed':'tool.denied')&&e.data.tool==='mcp.runtime.search'&&e.data.outcome===(id==='mcp-allowed'?'ok':'denied'));}).toBe(true);
 }
 await expect.poll(async()=>{const res=await page.request.get('/api/v1/graphs/mcp-allowed/executions',{headers});return (await res.json()).some((x:{status:string})=>x.status==='succeeded');}).toBe(true);
 const execution=(await (await page.request.get('/api/v1/graphs/mcp-allowed/executions',{headers})).json())[0];
 expect(execution.toolCalls).toBe(1);
 await page.goto(`/#/g/mcp-allowed/runs?execution=${execution.id}&section=result`);
 await expect(page.getByText(/1 tool attempts recorded/)).toBeVisible();
 await expect(page.getByRole('region',{name:'Run tool outcomes'})).toContainText('1 tool invocations observed · 2 tool lifecycle events');
 await page.goto('/#/g/mcp-allowed/events');
 for(const width of [1280,580]){
  await page.setViewportSize({width,height:900});
  const row=page.locator('.tl-row').filter({has:page.locator('.tl-type').filter({hasText:'execution.instructions.loaded'})}).first();
  await expect(row).toBeVisible();
  const type=await row.locator('.tl-type').boundingBox(),who=await row.locator('.tl-who').boundingBox();
  expect(type).toBeTruthy();expect(who).toBeTruthy();
  expect(type!.x+type!.width<=who!.x || type!.y+type!.height<=who!.y).toBe(true);
  const details=row.getByRole('button',{name:/data/});if(await details.getAttribute('aria-expanded')!=='true')await details.click();await expect(row.locator('pre')).toBeVisible();
  await page.screenshot({path:`/tmp/weft-ux-events-${width}.png`,fullPage:true});
 }
 await page.setViewportSize({width:1280,height:900});
 const disabled=await page.request.put('/api/v1/mcp/connections/runtime',{headers:{...headers,'If-Match':String(connection.revision)},data:{id:'runtime',name:'Runtime',service:'generic',endpoint:'https://mcp.fixture.invalid/mcp',enabled:false}});expect(disabled.ok()).toBeTruthy();
 await page.goto('/#/g/mcp-allowed/canvas');await page.getByRole('button',{name:'Stop all',exact:true}).click();await expect(page.getByRole('button',{name:'Run all',exact:true})).toBeEnabled();await page.getByRole('button',{name:'Run all',exact:true}).click();
 await expect.poll(async()=>{const res=await page.request.get('/api/v1/graphs/mcp-allowed/events',{headers});return (await res.json()).some((e:{type:string;data:{tool?:string}})=>e.type==='tool.denied'&&e.data.tool==='mcp.runtime.search');}).toBe(true);
});

test('opens a pinned template through login and saves only after review',async({page})=>{
 await page.goto('/#/integrations/connect?template=deepwiki&version=1');
 await page.getByLabel('API token',{exact:true}).fill('e2e-admin-token');
 await page.getByRole('button',{name:'Continue',exact:true}).click();
 const review=page.getByRole('article',{name:'Review MCP template'});
 await expect(review).toContainText('DeepWiki · version 1');
 await expect(page).toHaveURL(/template=deepwiki&version=1/);
 const before=await page.request.get('/api/v1/mcp/connections',{headers});
 expect((await before.json()).some((c:{id:string})=>c.id==='deepwiki')).toBe(false);
 await review.getByRole('button',{name:'Use template defaults'}).click();
 const section=page.getByRole('region',{name:'MCP connections',exact:true});
 await expect(section.getByLabel('MCP endpoint',{exact:true})).toHaveValue('https://mcp.deepwiki.com/mcp');
 await section.getByRole('button',{name:'Save MCP connection',exact:true}).click();
 const card=section.getByRole('article',{name:'MCP DeepWiki',exact:true});
 await expect(card).toContainText('Template: deepwiki v1');await expect(card).toContainText('unavailable');await expect(card).toContainText('Discovery failed');
 const saved=await page.request.get('/api/v1/mcp/connections/deepwiki',{headers});
 expect(await saved.json()).toMatchObject({template:{id:'deepwiki',version:1},tools:[],credentialConfigured:false});
 await page.reload();await expect(card).toContainText('Template: deepwiki v1');
 await section.getByRole('button',{name:'Edit DeepWiki',exact:true}).click();
 await section.getByLabel('Connection name',{exact:true}).fill('DeepWiki reviewed');
 await section.getByRole('button',{name:'Save MCP connection',exact:true}).click();
 await expect(section.getByRole('article',{name:'MCP DeepWiki reviewed',exact:true})).toContainText('Template: deepwiki v1');
});

test('rejects invalid template links and allows catalogue navigation without creating connections',async({page})=>{
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 for(const query of ['template=deepwiki&version=99','template=deepwiki&version=1&token=synthetic']){
  await page.goto('/#/integrations/connect?'+query);
  await expect(page.getByRole('region',{name:'MCP templates'})).toContainText(query.includes('99')?'Unknown MCP template version':'Invalid MCP connection link');
  await expect(page.getByRole('button',{name:'Use template defaults'})).toHaveCount(0);
 }
 await page.getByRole('link',{name:'GitHub read-only · v1',exact:true}).click();
 await expect(page.getByRole('article',{name:'Review MCP template'})).toContainText('https://api.githubcopilot.com/mcp/readonly');
 await expect(page.getByLabel('Connection ID',{exact:true})).toHaveValue('');
});

test('deletes a disabled connection after confirmation and permanently reserves its identity',async({page})=>{
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 const data={id:'delete-browser',name:'Delete browser',service:'generic',endpoint:'https://mcp.fixture.invalid/mcp',enabled:true,credential:{value:'fixture-mcp-token'}};
 expect((await page.request.post('/api/v1/mcp/connections',{headers,data})).status()).toBe(201);
 await page.goto('/#/integrations');const card=page.getByRole('article',{name:'MCP Delete browser',exact:true});
 await expect(card.getByRole('button',{name:'Delete Delete browser',exact:true})).toBeDisabled();
 await card.getByRole('button',{name:'Edit Delete browser',exact:true}).click();await page.getByLabel('Connection enabled',{exact:true}).uncheck();await page.getByRole('button',{name:'Save MCP connection',exact:true}).click();
 await card.getByRole('button',{name:'Delete Delete browser',exact:true}).click();await page.getByRole('button',{name:'Cancel deletion',exact:true}).click();await expect(card).toBeVisible();
 await card.getByRole('button',{name:'Delete Delete browser',exact:true}).click();await expect(page.getByRole('button',{name:'Confirm deletion',exact:true})).toBeDisabled();
 await page.getByLabel('Type connection ID to confirm').fill('delete-browser');await page.getByRole('button',{name:'Confirm deletion',exact:true}).click();await expect(card).toHaveCount(0);
 await page.reload();await expect(card).toHaveCount(0);
 expect((await page.request.post('/api/v1/mcp/connections',{headers,data})).status()).toBe(409);
});

test('recovers an expired session during discovery and before gateway dispatch',async({page})=>{
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 const data={id:'recover-browser',name:'Recover browser',service:'generic',endpoint:'https://recovery.fixture.invalid/mcp',enabled:true,credential:{value:'fixture-mcp-token'}};
 expect((await page.request.post('/api/v1/mcp/connections',{headers,data})).status()).toBe(201);
 await page.goto('/#/integrations');const card=page.getByRole('article',{name:'MCP Recover browser',exact:true});
 await card.getByRole('button',{name:'Refresh tools for Recover browser',exact:true}).click();await expect(card).toContainText('mcp.recover-browser.search');await expect(card).toContainText('ready');
 const created=await page.request.post('/api/v1/graphs',{headers,data:{metadata:{id:'mcp-recovery',name:'MCP recovery'},spec:{nodes:[{id:'worker',runtime:{kind:'native'},model:{provider:'e2e',name:'fixture'},isolation:{mode:'process'},loop:{mode:'once'},prompt:'mcp-recovery-fixture',tools:[{name:'mcp.recover-browser.search'}]}]}}});expect(created.status(),await created.text()).toBe(201);
 await page.goto('/#/g/mcp-recovery/canvas');await page.getByRole('button',{name:'Run all',exact:true}).click();
 await expect.poll(async()=>{const r=await page.request.get('/api/v1/graphs/mcp-recovery/events',{headers});return(await r.json()).some((e:{type:string;data:{tool?:string;outcome?:string}})=>e.type==='tool.completed'&&e.data.tool==='mcp.recover-browser.search'&&e.data.outcome==='ok');}).toBe(true);
 await page.getByRole('tab',{name:'Activity',exact:true}).click();await expect(page.getByRole('region',{name:'Executions and results',exact:true})).toContainText('Completed');
});

test('enforces a shared connection quota and filters approval and result history',async({page})=>{
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 await page.goto('/#/integrations');const section=page.getByRole('region',{name:'MCP connections',exact:true});
 await section.getByLabel('Connection ID',{exact:true}).fill('quota-browser');await section.getByLabel('Connection name',{exact:true}).fill('Quota browser');await section.getByLabel('MCP endpoint',{exact:true}).fill('https://mcp.fixture.invalid/mcp');await section.getByLabel('Bearer token',{exact:true}).fill('fixture-mcp-token');await section.getByLabel('Connection calls per minute',{exact:true}).fill('1');await section.getByRole('button',{name:'Save MCP connection',exact:true}).click();
 const card=section.getByRole('article',{name:'MCP Quota browser',exact:true});await expect(card).toContainText('1 calls per UTC minute');await card.getByRole('button',{name:'Refresh tools for Quota browser',exact:true}).click();await expect(card).toContainText('mcp.quota-browser.search');
 for(const id of ['quota-approved','quota-limited']){
  const created=await page.request.post('/api/v1/graphs',{headers,data:{metadata:{id,name:id},spec:{nodes:[{id:'worker',runtime:{kind:'native'},model:{provider:'e2e',name:'fixture'},isolation:{mode:'process'},loop:{mode:'once'},prompt:'mcp-quota-fixture',tools:[{name:'mcp.quota-browser.search',requiresApproval:id==='quota-approved'}]}]}}});expect(created.status(),await created.text()).toBe(201);
  await page.goto(`/#/g/${id}/activity`);await page.getByRole('button',{name:'Run all',exact:true}).click();
  if(id==='quota-approved'){await page.getByRole('region',{name:'Task approvals',exact:true}).getByRole('button',{name:'Approve',exact:true}).click();await expect(page.getByRole('region',{name:'Executions and results',exact:true})).toContainText('MCP fixture search completed');}
  else {await expect.poll(async()=>{const r=await page.request.get(`/api/v1/graphs/${id}/events`,{headers});return(await r.json()).some((e:{type:string;data:{outcome?:string}})=>e.type==='tool.denied'&&e.data.outcome==='rate_limited');}).toBe(true);}
 }
 await page.goto('/#/integrations');const history=page.getByRole('region',{name:'MCP call history',exact:true});
 await history.getByLabel('History connection ID').fill('quota-browser');await history.getByLabel('History outcome').selectOption('rate_limited');await history.getByRole('button',{name:'Apply history filters'}).click();await expect(history.getByRole('table')).toContainText('Call quota exceeded');await expect(history.getByRole('table')).toContainText('quota-limited');await expect(history.getByRole('table')).not.toContainText('quota-approved');
 await history.getByLabel('History outcome').selectOption('approved');await history.getByLabel('History event type').selectOption('approval.decided');await history.getByRole('button',{name:'Apply history filters'}).click();await expect(history.getByRole('table')).toContainText('Approval granted');
 await history.getByLabel('History outcome').selectOption('ok');await history.getByLabel('History event type').selectOption('tool.completed');await history.getByRole('button',{name:'Apply history filters'}).click();await expect(history.getByRole('table')).toContainText('Call completed successfully');await expect(history).not.toContainText('fixture-mcp-token');
 await page.route('**/api/v1/mcp/history?**',route=>route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({error:'History unavailable',code:'unavailable'})}));await history.getByRole('button',{name:'Apply history filters'}).click();await expect(history).toContainText('History unavailable');await expect(history.getByRole('table')).toContainText('Call completed successfully');await page.unroute('**/api/v1/mcp/history?**');
 await history.getByRole('button',{name:'Apply history filters'}).click();await expect(history).not.toContainText('History unavailable');
});
