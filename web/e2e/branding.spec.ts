import {test, expect, type Page} from '@playwright/test';

const surface = (page: Page) => page.evaluate(() => getComputedStyle(document.documentElement).backgroundColor);
const token = (page: Page, name: string) => page.evaluate(n => getComputedStyle(document.documentElement).getPropertyValue(n).trim(), name);

test('applies Weft brand tokens and persists the theme choice', async ({page}) => {
  await page.emulateMedia({colorScheme: 'light'});
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-viewer-token'));
  await page.goto('/#/graphs');
  await expect(page.getByRole('button', {name: 'Weft', exact: true})).toBeVisible();
  expect(await token(page, '--primary')).toBe('#0f6e56');
  expect(await surface(page)).toBe('rgb(247, 246, 242)');
  await expect(page.locator('meta[name="theme-color"]')).toHaveAttribute('content', '#0f6e56');
  const favicon = await page.request.get('/favicon.svg');
  expect(favicon.ok()).toBe(true);
  expect(await favicon.text()).toContain('#0F6E56');

  await page.getByRole('combobox', {name: 'Theme'}).selectOption('dark');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  expect(await surface(page)).toBe('rgb(20, 20, 19)');
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect(page.getByRole('combobox', {name: 'Theme'})).toHaveValue('dark');
  expect(await token(page, '--primary')).toBe('#5dcaa5');

  await page.getByRole('combobox', {name: 'Theme'}).selectOption('system');
  await expect(page.locator('html')).not.toHaveAttribute('data-theme', /.+/);
  expect(await surface(page)).toBe('rgb(247, 246, 242)');
  await page.emulateMedia({colorScheme: 'dark'});
  expect(await surface(page)).toBe('rgb(20, 20, 19)');
});
