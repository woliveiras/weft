import {test, expect} from '@playwright/test';
import type {GraphSummary} from '../src/api/types';

// Visual regression for the main destinations in both themes
// (docs/brand/brand-guidelines.md §13). Render baselines on the target platform with
// `pnpm exec playwright test --config playwright.visual.config.ts --update-snapshots` and review
// the images before committing.
const pages = [
  ['workflows', '/#/graphs', 'Workflows'],
  ['canvas', '/#/g/journal-capture/canvas', undefined],
  ['skills', '/#/skills', 'Skills & instructions'],
  ['models', '/#/models', 'Models'],
  ['approvals', '/#/approvals', 'Approvals'],
  ['new-task', '/#/tasks/new', 'New task'],
] as const;

for (const theme of ['light', 'dark'] as const) {
  test.describe(`${theme} theme`, () => {
    test.use({viewport: {width: 1280, height: 800}, colorScheme: theme, locale: 'en-US', timezoneId: 'UTC'});
    for (const [name, url, heading] of pages) {
      test(`${name} matches the design baseline`, async ({page}) => {
        await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-viewer-token'));
        if (name === 'workflows') {
          // Fixture workflows can be seeded in the same millisecond. Fix their update
          // times so the list uses its stable ID tie-breaker on every platform.
          await page.route('**/api/v1/graphs', async route => {
            const response = await route.fetch();
            expect(response.ok()).toBe(true);
            const graphs: GraphSummary[] = await response.json();
            for (const {record} of graphs) record.updatedAt = '2026-01-01T12:00:00Z';
            await route.fulfill({response, json: graphs});
          });
        }
        await page.goto(url);
        if (heading) await expect(page.getByRole('heading', {name: heading, level: 1, exact: true})).toBeVisible();
        else await expect(page.locator('.agent-node').first()).toBeVisible();
        if (name === 'workflows') await expect(page.getByRole('region', {name:'Workflows'}).getByRole('row').nth(1)).toBeVisible();
        await page.evaluate(() => document.fonts.ready);
        await expect(page).toHaveScreenshot(`${name}-${theme}.png`, {
          animations: 'disabled',
          caret: 'hide',
          // Connection state and infrastructure details vary between runs.
          mask: [page.locator('.conn'), page.locator('.infra')],
          maxDiffPixelRatio: 0.01,
        });
      });
    }
  });
}
