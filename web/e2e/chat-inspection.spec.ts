import { expect, test } from '@playwright/test';

test('reads the current workflow inventory in a conversation', async ({page, request}) => {
  const headers = {Authorization: 'Bearer e2e-admin-token'};
  const graph = await request.post('/api/v1/graphs', {headers, data: {
    metadata: {id:'inspect-journey'}, spec:{nodes:[{id:'reader',runtime:{kind:'echo'}}]},
  }});
  expect(graph.status()).toBe(201);
  await page.addInitScript(() => localStorage.setItem('weftToken','e2e-viewer-token'));
  await page.goto('/');
  await page.getByRole('textbox',{name:'Message',exact:true}).fill('/inspect workflows');
  await page.getByRole('button',{name:'Send',exact:true}).click();
  const link = page.getByRole('link',{name:'Open Workflows'});
  await expect(link).toBeVisible();
  // On shared fixture servers the list can be truncated as earlier tests create workflows.
  await expect(page.locator('.chat-message-assistant')).toContainText(/version \d+ · (stopped|running|paused)/);
  await link.click();
  await expect(page).toHaveURL(/#\/graphs$/);
  await page.goBack();
  await expect(link).toBeFocused();
});

test('downloads installation YAML from the original export route without putting YAML into Chat', async ({page}) => {
  await page.addInitScript(() => localStorage.setItem('weftToken','e2e-admin-token'));
  await page.goto('/');
  await page.getByRole('textbox',{name:'Message'}).fill('/export');
  await page.getByRole('button',{name:'Send', exact:true}).click();
  const downloaded = page.waitForEvent('download');
  const response = page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/config/export');
  await page.getByRole('button',{name:'Download installation YAML'}).click();
  expect((await downloaded).suggestedFilename()).toBe('installation.yaml');
  expect((await response).status()).toBe(200);
  await expect(page.getByLabel('Conversation',{exact:true})).not.toContainText('apiVersion:');
});
