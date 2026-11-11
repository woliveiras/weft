import { test, expect } from '@playwright/test';

test('separates installation destinations and preserves legacy MCP links', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.goto('/#/connections');
  await expect(page.getByRole('heading', { name: 'Connections', exact: true })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Model providers' })).toHaveCount(0);
  await page.getByRole('navigation', { name: 'Main navigation' }).getByRole('button', { name: 'Models', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Add provider' })).toBeVisible();
  await expect(page.getByRole('region', { name: 'MCP connections', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Administration', exact: true }).click();
  await expect(page.getByRole('article', { name: 'Sandbox runtime' })).toBeVisible();
  await page.getByRole('link', { name: 'Network policies', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Installation network policy' })).toBeVisible();
  await expect(page.getByRole('article', { name: 'Sandbox runtime' })).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole('link', { name: 'Network policies' })).toHaveAttribute('aria-current', 'page');
  await page.goBack();
  await expect(page.getByRole('article', { name: 'Sandbox runtime' })).toBeVisible();
  await page.goto('/#/integrations/connect?template=deepwiki&version=1');
  await expect(page.getByRole('heading', { name: 'Connections', exact: true })).toBeVisible();
  await expect(page.getByRole('region', { name: 'MCP connections', exact: true })).toContainText('DeepWiki');
});

test('viewer navigation retains read-only permissions at narrow widths', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-viewer-token'));
  await page.setViewportSize({ width: 580, height: 850 });
  await page.goto('/#/models');
  await expect(page.getByRole('heading', { name: 'Models', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Add provider' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Test all providers' })).toHaveCount(0);
  const connections = page.getByRole('button', { name: 'Connections', exact: true });
  await expect.poll(() => connections.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
  await connections.click();
  await expect(page.getByRole('region', { name: 'MCP connections', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save MCP connection' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Administration', exact: true }).click();
  await page.getByRole('link', { name: 'Network policies', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Review network policy' })).toHaveCount(0);
});
