import {test, expect} from '@playwright/test';

test('groups navigation, supports keyboard disclosure and preserves legacy links', async ({page}) => {
  await page.addInitScript(() => localStorage.setItem('weftToken','e2e-admin-token'));
  await page.goto('/#/integrations');
  const nav = page.getByRole('navigation',{name:'Main navigation'});
  await expect(nav.getByRole('button',{name:'Connections',exact:true})).toHaveAttribute('aria-current','page');
  await expect(nav.getByRole('button',{name:'Tasks',exact:true})).toBeEnabled();
  await expect(nav.getByRole('button',{name:'Projects',exact:true})).toBeEnabled();
  const toggle = nav.getByRole('button',{name:'Configuration',exact:true});
  await toggle.focus(); await page.keyboard.press('Enter');
  await expect(nav.getByRole('button',{name:'Connections',exact:true})).toBeHidden();
  await page.keyboard.press('Space');
  await expect(nav.getByRole('button',{name:'Connections',exact:true})).toBeVisible();
  await nav.getByRole('button',{name:'Workflows',exact:true}).click();
  await expect(page).toHaveURL(/#\/graphs$/);
  await expect(page.getByRole('heading',{name:'Graphs',exact:true})).toBeVisible();
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
