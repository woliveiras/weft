import { expect, test } from '@playwright/test';

test('uses the available task form width and aligns visible fields and import consent', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.goto('/#/tasks/new');

  const form = page.getByRole('form', { name: 'Task request' });
  const back = page.getByRole('link', { name: 'Back to tasks', exact: true });
  await expect(form).toBeVisible();
  await expect(page.getByText('Save this request to resume it later.', { exact: true })).toHaveCount(0);
  const frame = (await page.getByRole('main').boundingBox())!;
  const card = (await form.boundingBox())!;
  const link = (await back.boundingBox())!;
  expect(frame.width - card.width).toBeLessThanOrEqual(80);
  expect(card.y - link.y - link.height).toBeGreaterThanOrEqual(12);

  const save = (await form.getByRole('button', { name: 'Save draft', exact: true }).boundingBox())!;
  const run = (await form.getByRole('button', { name: 'Run task', exact: true }).boundingBox())!;
  const review = (await form.getByRole('button', { name: 'Review requirements', exact: true }).boundingBox())!;
  expect(run.x - save.x - save.width).toBeGreaterThanOrEqual(12);
  expect(review.x - run.x - run.width).toBeGreaterThanOrEqual(12);

  const goal = (await form.getByRole('combobox', { name: 'Goal', exact: true }).boundingBox())!;
  const title = (await form.getByRole('textbox', { name: 'Title', exact: true }).boundingBox())!;
  expect(title.y).toBe(goal.y);
  expect(title.width).toBe(goal.width);

  const workflow = (await form.getByRole('combobox', { name: 'Workflow', exact: true }).boundingBox())!;
  const project = (await form.getByRole('combobox', { name: 'Project', exact: true }).boundingBox())!;
  const environment = (await form.getByRole('combobox', { name: 'Execution environment', exact: true }).boundingBox())!;
  expect(project.x).toBe(workflow.x);
  expect(project.width).toBe(workflow.width);
  expect(environment.y).toBe(project.y);
  expect(project.y - workflow.y - workflow.height).toBeGreaterThanOrEqual(16);
  const checkIssueAlignment = async () => {
    const delivery = (await form.getByRole('combobox', { name: 'Input delivery', exact: true }).boundingBox())!;
    const legend = (await form.getByText('Issue source (optional)', { exact: true }).boundingBox())!;
    const service = (await form.getByRole('combobox', { name: 'Issue service', exact: true }).boundingBox())!;
    expect(legend.x).toBe(delivery.x);
    expect(service.x).toBe(delivery.x);
    expect(service.width).toBe(delivery.width);
  };
  await checkIssueAlignment();
  await form.getByText('Import repository from URL', { exact: true }).click();
  const checkbox = form.getByRole('checkbox', { name: /Authorize the selected Git authentication/ });
  await expect(checkbox).toBeVisible();
  const clone = (await form.getByRole('textbox', { name: 'Clone URL', exact: true }).boundingBox())!;
  const imported = (await form.getByRole('region', {name:'Import repository'}).boundingBox())!;
  expect(clone.x - imported.x).toBeGreaterThanOrEqual(16);
  expect(imported.width - clone.width).toBeLessThanOrEqual(48);
  const consentGeometry = () => checkbox.evaluate(input => {
    const box = input.getBoundingClientRect();
    const text = input.nextElementSibling!.getBoundingClientRect();
    return { gap: text.left - box.right, topDelta: box.top - text.top };
  });
  expect(await consentGeometry()).toEqual({ gap: 8, topDelta: 3.25 });

  await page.setViewportSize({ width: 390, height: 844 });
  await checkIssueAlignment();
  const narrowSave = (await form.getByRole('button', { name: 'Save draft', exact: true }).boundingBox())!;
  const narrowRun = (await form.getByRole('button', { name: 'Run task', exact: true }).boundingBox())!;
  const narrowReview = (await form.getByRole('button', { name: 'Review requirements', exact: true }).boundingBox())!;
  for (const [before, after] of [[narrowSave, narrowRun], [narrowRun, narrowReview]]) {
    const gap = after.y === before.y ? after.x - before.x - before.width : after.y - before.y - before.height;
    expect(gap).toBeGreaterThanOrEqual(12);
  }
  expect(await consentGeometry()).toEqual({ gap: 8, topDelta: 3.25 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390);
  expect(await page.evaluate(() => document.documentElement.scrollHeight)).toBe(844);
});
