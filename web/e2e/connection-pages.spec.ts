import { test, expect } from '@playwright/test';
const headers={Authorization:'Bearer e2e-admin-token'};
test('keeps the new connection form compact and opens a template for review', async ({ page }) => {
 await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
 await page.setViewportSize({ width: 1280, height: 720 });
 await page.goto('/#/connections/new');
 await expect(page.getByRole('heading', { name: 'New connection', level: 1 })).toBeVisible();
 await expect(page.getByRole('spinbutton', { name: 'Connection calls per minute' })).toBeHidden();
 const pane = page.locator('.installation-page');
 expect(await pane.evaluate(el => el.scrollHeight)).toBe(await pane.evaluate(el => el.clientHeight));
 await page.setViewportSize({ width: 1700, height: 470 });
 const layout = await pane.evaluate(el => ({
  right: el.getBoundingClientRect().right,
  mainRight: el.closest('main')?.getBoundingClientRect().right,
  overflows: el.scrollHeight > el.clientHeight,
 }));
 expect(layout.right).toBe(layout.mainRight);
 expect(layout.overflows).toBe(true);
 await page.setViewportSize({ width: 390, height: 844 });
 await expect(page.getByRole('form', { name: 'New MCP connection' })).toBeVisible();
 expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390);
 await page.getByText('Browse MCP templates', { exact: true }).click();
 await expect(page.getByRole('link', { name: 'DeepWiki · v1', exact: true })).toBeVisible();
 const cardHeights = await page.locator('.mcp-template-list a').evaluateAll(links =>
  links.map(link => Math.round(link.getBoundingClientRect().height)),
 );
 expect(cardHeights.length).toBeGreaterThan(1);
 expect(new Set(cardHeights).size).toBe(1);
 await page.getByRole('link', { name: 'DeepWiki · v1', exact: true }).click();
 await expect(page.getByRole('article', { name: 'Review MCP template' })).toBeVisible();
 await expect(page.getByLabel('Connection ID', { exact: true })).toHaveValue('');
});

test('creates a connection and navigates scoped tools, history and settings', async({page})=>{
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 await page.goto('/#/connections');await page.getByRole('region',{name:'MCP connections',exact:true}).getByRole('link',{name:'Add connection',exact:true}).click();
 await page.getByLabel('Connection ID',{exact:true}).fill('pages');
 await page.getByLabel('Connection name',{exact:true}).fill('Pages fixture');
 await page.getByLabel('MCP endpoint',{exact:true}).fill('https://mcp.fixture.invalid/mcp');
 await page.getByLabel('Bearer token',{exact:true}).fill('fixture-mcp-token');
 await page.getByRole('button',{name:'Save MCP connection',exact:true}).click();
 await expect(page).toHaveURL(/connections\/c\/pages\/overview$/);
 await expect(page.getByRole('region',{name:'Connection agent grants'})).toContainText('No saved agent grants');
 await expect(page.getByRole('region',{name:'Connection readiness'})).toContainText('tools discovered');
 await expect(page.getByRole('region',{name:'Connection overview'})).toContainText('Configuration revision');
 await page.getByRole('link',{name:'Tools',exact:true}).click();
 await expect(page.getByRole('region',{name:'Connection overview'})).toHaveCount(0);
 await expect(page.getByText('mcp.pages.search',{exact:true})).toBeVisible();
 await page.getByLabel('Search tools',{exact:true}).fill('MCP.PAGES.READ_ISSUE');
 await expect(page.getByText('mcp.pages.search',{exact:true})).toHaveCount(0);
 await expect(page.getByText('mcp.pages.read_issue',{exact:true})).toBeVisible();
 const catalogue = page.getByRole('region', { name: 'Discovered tools' });
 await catalogue.getByRole('button', { name: 'Input schema' }).click();
 await expect(catalogue.locator('pre')).toBeVisible();
 await page.reload();await expect(page.getByRole('link',{name:'Tools',exact:true})).toHaveAttribute('aria-current','page');
 await page.getByRole('link',{name:'Usage & history',exact:true}).click();
 await page.getByRole('button',{name:'Clear history filters'}).click();
 await expect(page.getByLabel('History connection ID')).toHaveValue('pages');
 const history=page.waitForResponse(r=>r.url().includes('/mcp/history?'));
 await page.getByRole('button',{name:'Apply history filters'}).click();
 expect(new URL((await history).url()).searchParams.get('connection')).toBe('pages');
 await page.setViewportSize({width:390,height:844});
 await expect(page.getByRole('region',{name:'MCP call history'})).toBeVisible();
 expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390);
 await page.setViewportSize({width:1280,height:720});
 await page.getByRole('link',{name:'Settings',exact:true}).click();
 await expect(page.getByRole('region',{name:'Delete connection'}).getByRole('button',{name:'Delete Pages fixture'})).toBeDisabled();
 await page.getByRole('button',{name:'Edit Pages fixture',exact:true}).click();
 await expect(page.getByLabel('Bearer token',{exact:true})).toHaveValue('');
 await page.getByLabel('Connection enabled',{exact:true}).uncheck();
 await page.getByRole('button',{name:'Save MCP connection',exact:true}).click();
 await expect(page).toHaveURL(/connections\/c\/pages\/overview$/);
 await expect(page.getByRole('button',{name:'Test Pages fixture',exact:true})).toBeDisabled();
 await page.getByRole('link',{name:'Connections',exact:true}).click();
 const mcp = page.getByRole('region', {name:'MCP connections',exact:true});
 await mcp.getByLabel('Search connections').fill('Pages fixture');
 await mcp.getByLabel('Connection status').selectOption('disabled');
 await expect(page.getByRole('link',{name:'Pages fixture',exact:true})).toBeVisible();
 await mcp.getByLabel('Search connections').fill('Missing connection');
 await expect(page.getByText('No connections match these filters.')).toBeVisible();
 await mcp.getByRole('button',{name:'Clear filters',exact:true}).click();
 await expect(page.getByRole('link',{name:'Pages fixture',exact:true})).toBeVisible();
 await expect(mcp.getByLabel('Search connections')).toBeFocused();
 await page.setViewportSize({width:390,height:844});
 await expect(page.getByRole('region',{name:'MCP connection list'})).toBeVisible();
 expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390);
 await page.getByRole('link',{name:'View connection history',exact:true}).click();
 await expect(page).toHaveURL(/connections\/history$/);
 const saved=await page.request.get('/api/v1/mcp/connections/pages',{headers});
 expect(await saved.text()).not.toContain('fixture-mcp-token');
});
test('viewer detail is read-only and unknown connections are explicit at narrow widths',async({page})=>{
 const created=await page.request.post('/api/v1/mcp/connections',{headers,data:{id:'pages-view',name:'Viewer fixture',service:'generic',endpoint:'https://mcp.fixture.invalid/mcp',enabled:true}});expect(created.status()).toBe(201);
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-viewer-token'));
 await page.setViewportSize({width:580,height:850});
 await page.goto('/#/connections/c/pages-view/settings');
 await expect(page.getByText('Only administrators can edit connection settings.')).toBeVisible();
 await expect(page.getByRole('button',{name:'Edit Viewer fixture'})).toHaveCount(0);
 await page.getByRole('link',{name:'Overview',exact:true}).click();
 await expect(page.getByRole('button',{name:'Test Viewer fixture'})).toHaveCount(0);
 await page.goto('/#/connections/c/nonexistent/overview');
 await expect(page.getByRole('alert')).toContainText('Connection not found');
 await expect(page.getByRole('button',{name:'Save MCP connection'})).toHaveCount(0);
});

test('configures each Bitbucket edition through the real SCM API without contacting the vendor', async ({page}) => {
 await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
 await page.setViewportSize({width: 600, height: 850});
 for (const kind of ['bitbucket-cloud', 'bitbucket-dc']) {
  await page.goto('/#/connections/source-control/new');
  await page.getByLabel('Kind', {exact:true}).selectOption(kind);
  await page.getByLabel('Name', {exact:true}).fill(kind);
  await page.getByLabel('Repository', {exact:true}).fill('ACME/app');
  if (kind === 'bitbucket-dc') await page.getByLabel('API URL', {exact:true}).fill('https://bitbucket.fixture.invalid/context');
  await page.getByLabel('Token', {exact:true}).fill('synthetic-contract-token');
  await page.getByRole('button', {name:'Save connection', exact:true}).click();
  await expect(page).toHaveURL(/#\/connections$/);
  const response = await page.request.get('/api/v1/scm/connections', {headers});
  const saved = (await response.json()).find((c: {name:string}) => c.name === kind);
  expect(saved.kind).toBe(kind);
  expect(saved.tokenConfigured).toBe(true);
  expect(saved.token).toBeUndefined();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(600);
 }
});
