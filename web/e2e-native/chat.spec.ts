import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { expect, test } from '@playwright/test';

test('opens an unconfigured native Chat and restores an archived conversation', async ({ page }) => {
  const fixture = process.env.WEFT_NATIVE_E2E_DATA!;
  const token = readFileSync(join(fixture, 'state', 'admin.token'), 'utf8').trim();
  await page.goto('/');
  await page.getByLabel('API token').fill(token);
  await page.getByRole('button', {name:'Continue',exact:true}).click();
  await expect(page.getByText('No Chat model selected.', {exact:false})).toBeVisible();
  await page.getByRole('textbox', {name:'Message',exact:true}).fill('/inspect workflows');
  await page.getByRole('button', {name:'Send',exact:true}).click();
  await expect(page.locator('.chat-message-assistant')).toContainText('software-factory');
  const rowActions = page.getByRole('button', {name:'Actions for Untitled chat',exact:true});
  await rowActions.click();
  await page.getByRole('button', {name:'Archive',exact:true}).click();
  await page.getByRole('button', {name:'Confirm',exact:true}).click();
  await expect(page.getByRole('region', {name:'Archived'})).toBeVisible();
  await page.getByRole('button', {name:'New chat',exact:true}).click();
  await page.getByRole('region',{name:'Recents'}).getByRole('button', {name:/^Archived/}).click();
  await page.getByRole('region',{name:'Archived'}).getByRole('button', {name:'Untitled chat',exact:true}).click();
  await expect(page.locator('.chat-message-assistant')).toContainText('software-factory');
  await rowActions.click();
  await page.getByRole('button', {name:'Unarchive',exact:true}).click();
  await expect(page.getByRole('region', {name:'Recents'}).getByRole('button', {name:'Untitled chat',exact:true})).toBeVisible();
  await expect(page.locator('.chat-message-assistant')).toContainText('software-factory');
});
