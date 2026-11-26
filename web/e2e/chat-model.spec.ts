import { expect, test } from '@playwright/test';

test('an administrator selects the Chat model and a viewer sees the persisted choice', async ({ page, browser, request }) => {
  const headers = {Authorization: 'Bearer e2e-admin-token'};
  const settings = await request.get('/api/v1/chat/settings', {headers});
  expect(settings.ok()).toBe(true);
  const {revision} = await settings.json();
  const initial = await request.put('/api/v1/chat/settings', {
    headers: {...headers, 'If-Match': String(revision)},
    data: {provider: 'e2e', name: 'fixture-before-selection'},
  });
  expect(initial.ok()).toBe(true);
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.goto('/#/models');
  await page.getByRole('link', { name: 'Chat model', exact: true }).click();
  const form = page.getByRole('form', { name: 'Chat model selection' });
  await form.getByLabel('Provider').selectOption('e2e');
  await form.getByLabel('Model ID').fill('fixture');
  await form.getByRole('button', { name: 'Save Chat model' }).click();
  await expect(page.getByText('Chat model saved.')).toBeVisible();

  // A fresh viewer context reads the same installation setting from the server.
  const viewer = await browser.newContext({ baseURL: new URL(page.url()).origin });
  try {
    await viewer.addInitScript(() => localStorage.setItem('weftToken', 'e2e-viewer-token'));
    const view = await viewer.newPage();
    await view.goto('/#/models/chat');
    const section = view.getByRole('region', { name: 'Chat model' });
    await expect(section.getByText('e2e · fixture')).toBeVisible();
    await expect(section.getByRole('button', { name: 'Save Chat model' })).toHaveCount(0);
  } finally {
    await viewer.close();
  }
});
