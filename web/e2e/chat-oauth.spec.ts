import { expect, test } from '@playwright/test';

const headers = { Authorization: 'Bearer e2e-admin-token' };
test.use({ignoreHTTPSErrors: true}); // The loopback consent fixture uses its own certificate.

test('returns from a completed OAuth handoff to the originating chat message', async ({page, request}) => {
  const config = await (await request.get('/api/v1/mcp/connections/oauth-fixture-config', {headers})).json();
  const created = await request.post('/api/v1/mcp/connections', {headers, data: {
    id: 'chat-oauth', name: 'Chat OAuth fixture', service: 'generic', enabled: true, endpoint: config.endpoint,
    oauth: {issuer: config.oauth.issuer, clientId: 'fixture-client', scopes: ['read']},
  }});
  expect(created.status()).toBe(201);
  const settings = await (await request.get('/api/v1/chat/settings', {headers})).json();
  expect((await request.put('/api/v1/chat/settings', {headers: {...headers, 'If-Match': String(settings.revision)}, data: {provider: 'e2e', name: 'fixture'}})).ok()).toBe(true);
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.goto('/');
  await page.getByRole('textbox', {name:'Message', exact:true}).fill('Open the connections list for OAuth setup.');
  await page.getByRole('button', {name:'Send', exact:true}).click();
  const link = page.getByRole('link', {name:'Open Connections',exact:true});
  await expect(link).toBeVisible();
  const original = page.url();
  await link.click();
  await page.locator('a[href="#/connections/c/chat-oauth/overview"]').first().click();
  await page.getByRole('button', {name:'Authorize Chat OAuth fixture',exact:true}).click();
  await page.getByRole('button', {name:'Allow fixture access'}).click();
  await expect(page).toHaveURL(/#\/integrations\/oauth\?state=/);
  await page.reload();
  await page.getByRole('button', {name:'Complete OAuth connection'}).click();
  await expect(page.getByRole('region', {name:'OAuth return'}).getByRole('status')).toContainText('OAuth connected');
  await page.getByRole('link', {name:'Return to chat',exact:true}).click();
  await expect(page).toHaveURL(original);
  await expect(link).toBeFocused();
});
