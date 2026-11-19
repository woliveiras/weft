import {test, expect} from '@playwright/test';

test('keeps configuration visible and preserves legacy links', async ({page}) => {
  await page.addInitScript(() => localStorage.setItem('weftToken','e2e-admin-token'));
  await page.goto('/#/integrations');
  const nav = page.getByRole('navigation',{name:'Main navigation'});
  await expect(nav.getByRole('button',{name:'Connections',exact:true})).toHaveAttribute('aria-current','page');
  await expect(nav.getByRole('button',{name:'Tasks',exact:true})).toBeEnabled();
  const config = nav.getByRole('region', {name:'Configuration',exact:true});
  await expect(config.getByRole('button',{name:'Projects',exact:true})).toBeEnabled();
  await expect(nav.getByRole('region', {name:'Work',exact:true}).getByRole('button',{name:'Projects',exact:true})).toHaveCount(0);
  await config.getByRole('button',{name:'Projects',exact:true}).click();
  await expect(page).toHaveURL(/#\/projects$/);
  await expect(config.getByRole('button',{name:'Projects',exact:true})).toHaveAttribute('aria-current','page');
  await nav.getByRole('button',{name:'Connections',exact:true}).click();
  await expect(nav.getByRole('heading',{name:'Configuration',exact:true})).toBeVisible();
  await expect(nav.getByRole('button',{name:'Configuration',exact:true})).toHaveCount(0);
  await expect(nav.getByRole('button',{name:'Connections',exact:true})).toBeVisible();
  await nav.getByRole('button',{name:'Workflows',exact:true}).click();
  await expect(page).toHaveURL(/#\/graphs$/);
  await expect(page.getByRole('heading',{name:'Workflows',exact:true})).toBeVisible();
  await page.goBack();
  await expect(nav.getByRole('button',{name:'Connections',exact:true})).toHaveAttribute('aria-current','page');
  await page.reload();
  await expect(page.getByRole('heading',{name:'Connections',exact:true})).toBeVisible();
});

test('narrow navigation exposes every configuration destination without granting viewer writes', async ({page}) => {
  await page.addInitScript(() => localStorage.setItem('weftToken','e2e-viewer-token'));
  await page.setViewportSize({width:390,height:844});
  await page.goto('/#/models');
  const nav = page.getByRole('navigation',{name:'Main navigation'});
  await expect(page.getByRole('button',{name:'Add provider'})).toHaveCount(0);
  await expect.poll(() => nav.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
  await nav.getByRole('button',{name:'Connections',exact:true}).click();
  await expect(page.getByRole('heading',{name:'Connections',exact:true})).toBeVisible();
  await expect(page.getByRole('button',{name:'Save MCP connection'})).toHaveCount(0);
  const response = await page.request.post('/api/v1/graphs', {headers:{Authorization:'Bearer e2e-viewer-token'},data:{}});
  expect(response.status()).toBe(403);
});

test('compact navigation frees canvas width and preserves a draft across navigation',async({page})=>{
  const id='compact-navigation'; const headers={Authorization:'Bearer e2e-admin-token'};
  expect((await page.request.post('/api/v1/graphs',{headers,data:{metadata:{id,name:'Compact navigation'},spec:{nodes:[{id:'worker',name:'Worker',runtime:{kind:'echo'}}]}}})).ok()).toBeTruthy();
  await page.setViewportSize({width:1600,height:1000});
  await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
  await page.goto(`/#/g/${id}/canvas`);
  const nav=page.getByRole('navigation',{name:'Main navigation'});
  await page.getByRole('complementary',{name:'Agents',exact:true}).getByRole('button',{name:'Configure Worker',exact:true}).click();
  const name=page.getByRole('region',{name:'Agent configuration',exact:true}).getByLabel('Name',{exact:true});
  await name.fill('Draft worker'); await page.getByRole('tab',{name:'Graph',exact:true}).click();
  await page.getByRole('button',{name:'Close panel',exact:true}).click();
  const before=await page.locator('.canvas-wrap').boundingBox();
  await nav.getByRole('button',{name:'Collapse navigation'}).focus(); await page.keyboard.press('Enter');
  await expect(nav.getByRole('button',{name:'Expand navigation'})).toBeFocused();
  const after=await page.locator('.canvas-wrap').boundingBox(); expect(after!.width).toBeGreaterThan(before!.width+80);
  await nav.getByRole('button',{name:'Models',exact:true}).focus();
  await expect(page.getByRole('tooltip')).toHaveText('Models');
  await page.keyboard.press('Escape'); await expect(page.getByRole('tooltip')).toHaveCount(0);
  await page.keyboard.press('Enter'); await expect(page).toHaveURL(/#\/models$/);
  await nav.getByRole('button',{name:'Workflows',exact:true}).click();
  await page.getByRole('button',{name:'Compact navigation',exact:true}).click();
  await page.getByRole('button',{name:'Configure Draft worker',exact:true}).click();
  await expect(name).toHaveValue('Draft worker');
  const saved=await (await page.request.get(`/api/v1/graphs/${id}`,{headers})).json(); expect(saved.graph.spec.nodes[0].name).toBe('Worker');
  await page.reload(); await expect(nav.getByRole('button',{name:'Expand navigation'})).toBeVisible();
  await nav.getByRole('button',{name:'Expand navigation'}).click();
  await expect(nav.getByRole('heading',{name:'Configuration',exact:true})).toBeVisible();
  await expect(nav.getByRole('button',{name:'Workflows',exact:true})).toHaveAttribute('aria-current','page');
});

test('the icon navigation keeps every destination reachable on small viewports',async({page})=>{
  await page.setViewportSize({width:390,height:844});
  await page.addInitScript(()=>{localStorage.setItem('weftToken','e2e-viewer-token');localStorage.setItem('weft.navigation.compact','true');});
  await page.goto('/#/graphs'); const nav=page.getByRole('navigation',{name:'Main navigation'});
  await expect(nav.getByRole('button',{name:'Expand navigation'})).toBeVisible();
  expect(await nav.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true);
  await nav.getByRole('button',{name:'Connections',exact:true}).click();
  await expect(page.getByRole('heading',{name:'Connections',exact:true})).toBeVisible();
  await nav.getByRole('button',{name:'Expand navigation'}).click();
  await expect(nav.getByRole('button',{name:'Administration',exact:true})).toBeVisible();
});
