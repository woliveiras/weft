import {test, expect} from '@playwright/test';

// Visual regression for the main destinations in both themes
// (docs/brand/brand-guidelines.md §13). Baselines are rendered by Chromium on
// Linux; regenerate them in the Playwright container with
// `pnpm exec playwright test --config playwright.visual.config.ts --update-snapshots` and review
// the images before committing.
const pages = [
  ['workflows', '/#/graphs', 'Graphs'],
  ['canvas', '/#/g/journal-capture/canvas', undefined],
  ['models', '/#/models', 'Models'],
  ['approvals', '/#/approvals', 'Approvals'],
  ['new-task', '/#/tasks/new', 'New task'],
] as const;

for (const theme of ['light', 'dark'] as const) {
  test.describe(`${theme} theme`, () => {
    test.use({viewport: {width: 1280, height: 800}, colorScheme: theme});
    for (const [name, url, heading] of pages) {
      test(`${name} matches the design baseline`, async ({page}) => {
        await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-viewer-token'));
        await page.goto(url);
        if (heading) await expect(page.getByRole('heading', {name: heading, level: 1, exact: true})).toBeVisible();
        else await expect(page.locator('.agent-node').first()).toBeVisible();
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
