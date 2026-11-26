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
  await form.getByRole('combobox',{name:'Model ID'}).selectOption('fixture');
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

test('changes the model from a conversation and preserves the pending request', async ({page}) => {
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.goto('/#/chat');
  const composer = page.getByRole('textbox',{name:'Message',exact:true});
  await composer.fill('/inspect chat-model');
  await page.getByRole('button',{name:'Send',exact:true}).click();
  await expect(page).toHaveURL(/#\/chat\/[a-z0-9]+$/);
  const session = page.url();
  await composer.fill('Keep this request');
  await page.getByRole('button',{name:'Choose Chat model here'}).last().click();
  const form = page.getByRole('form',{name:'Chat model selection'});
  const field = form.getByLabel('Model ID');
  const before = await field.inputValue();
  await form.getByLabel('Provider').selectOption('e2e');
  await field.selectOption(before === 'fixture' ? 'fixture-other' : 'fixture');
  await form.getByRole('button',{name:'Save Chat model'}).click();
  await expect(page.getByText('Chat model saved.')).toBeVisible();
  await page.getByRole('button',{name:'Close model settings'}).click();
  await expect(page).toHaveURL(session);
  await expect(composer).toHaveValue('Keep this request');
});
