import { expect, test } from '@playwright/test';

test('administrators add an issue trigger to a workflow, preview matching issues and control it', async ({ page }) => {
  await page.goto('/#/g/git-inspect/canvas');
  await page.evaluate(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.reload();
  await page.getByRole('button', { name: 'Graph settings', exact: true }).click();
  const section = page.getByRole('group', { name: 'Issue triggers' });
  await expect(section).toBeVisible();
  await section.getByRole('button', { name: 'Add issue trigger', exact: true }).click();
  const form = section.getByRole('form', { name: 'Add issue trigger' });
  await form.getByLabel('Name').fill('Ready issues');
  await form.getByLabel('MCP connection').fill('e2e-tracker');
  await form.getByLabel('Repository').fill('acme/app');
  await expect(form.getByLabel('Assess issue readiness before work')).toBeChecked();
  await expect(form.getByLabel('Label', { exact: true })).toHaveValue('weft:todo');
  await expect(form).toContainText('Saving records your consent: tasks start as you, with Ask authorization');
  await expect(form).toContainText('16 KiB of input, 512 output tokens and 10 seconds');
  await form.getByRole('button', { name: 'Save trigger', exact: true }).click();
  const card = section.getByRole('article', { name: 'Trigger Ready issues' });
  await expect(card.getByText('Active', { exact: true })).toBeVisible();
  await expect(card).toContainText('Runs as e2e-admin for workflow version');

  await card.getByRole('button', { name: 'Preview matching issues' }).click();
  const matches = card.getByRole('group', { name: 'Matching issues' });
  await expect(matches).toContainText('Next: acme/app#11 — Add CSV export');
  await expect(matches).toContainText('acme/app#12: is blocked');

  await card.getByRole('button', { name: 'Pause', exact: true }).click();
  await expect(card.getByText('Paused', { exact: true })).toBeVisible();
  await expect(card.getByRole('status')).toContainText('Paused by e2e-admin.');
  await card.getByRole('button', { name: 'Resume and record consent' }).click();
  await expect(card.getByText('Active', { exact: true })).toBeVisible();

  await card.getByRole('button', { name: 'Edit', exact: true }).click();
  const edit = section.getByRole('form', { name: 'Edit issue trigger' });
  await edit.getByLabel('Assess issue readiness before work').uncheck();
  await edit.getByRole('button', { name: 'Save trigger', exact: true }).click();
  await expect(card).toContainText('Readiness disabled');
  await expect(card).toContainText('The trigger changed what was consented to');
  page.on('dialog', (d) => void d.accept());
  await card.getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(section.getByRole('article', { name: 'Trigger Ready issues' })).toHaveCount(0);
});

test('shows the same held issue and round in the trigger and Activity', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-viewer-token'));
  await page.goto('/#/activity');
  const holds = page.getByRole('region', { name: 'Issues awaiting readiness' });
  await expect(holds).toContainText('acme/app#11');
  await expect(holds).toContainText('round 1');
  await expect(holds).toContainText('The issue is waiting for clarification.');
  await page.goto('/#/g/git-inspect/canvas');
  await page.getByRole('button', { name: 'Graph settings', exact: true }).click();
  const trigger = page.getByRole('article', { name: 'Trigger Held readiness fixture' });
  await expect(trigger.getByRole('list', { name: 'Held issues' })).toContainText('acme/app#11 · round 1 · The issue is waiting for clarification.');
  await expect(trigger.getByRole('button', { name: 'Resolve hold' })).toHaveCount(0);
});

test('an operator resolves a hold and Activity records why', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-operator-token'));
  await page.goto('/#/g/git-inspect/canvas');
  await page.getByRole('button', { name: 'Graph settings', exact: true }).click();
  const trigger = page.getByRole('article', { name: 'Trigger Held readiness fixture' });
  await trigger.getByRole('button', { name: 'Resolve hold' }).click();
  await expect(trigger.getByRole('list', { name: 'Held issues' })).toHaveCount(0);
  await trigger.getByText('Resolved readiness holds', { exact: true }).click();
  await expect(trigger).toContainText('An operator resolved the readiness hold.');
  await page.goto('/#/activity');
  const resolution = page.getByRole('region', { name: 'Resolved readiness holds' }).getByRole('listitem').filter({ hasText: 'acme/app#11' });
  await expect(resolution).toContainText('e2e-operator');
});

test('forced resolution requires confirmation and records the unpublished comment', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-operator-token'));
  await page.goto('/#/g/git-inspect/canvas');
  await page.getByRole('button', { name: 'Graph settings', exact: true }).click();
  const card = page.getByRole('article', { name: 'Trigger Unpublished readiness fixture' });
  const holds = card.getByRole('list', { name: 'Held issues' });
  const button = card.getByRole('button', { name: 'Force resolve hold' });
  await expect(holds).toContainText('edits 2 of 3');
  const requests: unknown[] = [];
  page.on('request', (request) => {
    if (request.method() === 'POST' && request.url().endsWith('/resolve-hold')) {
      requests.push(request.postDataJSON());
    }
  });

  const dismissal = page.waitForEvent('dialog');
  const firstClick = button.click();
  const warning = await dismissal;
  expect(warning.message()).toContain('The readiness comment has not been published.');
  expect(warning.message()).toContain('current labels and assignment');
  await warning.dismiss();
  await firstClick;
  await expect(holds).toBeVisible();
  expect(requests).toHaveLength(0);

  const acceptance = page.waitForEvent('dialog');
  const secondClick = button.click();
  await (await acceptance).accept();
  await secondClick;
  await expect(holds).toHaveCount(0);
  expect(requests).toEqual([expect.objectContaining({ key: 'acme/app#13', force: true })]);
  await page.goto('/#/activity');
  const resolution = page.getByRole('region', { name: 'Resolved readiness holds' }).getByRole('listitem').filter({ hasText: 'acme/app#13' });
  await expect(resolution).toContainText('The readiness comment has not been published.');
});
