import { expect, test } from '@playwright/test';

test('uses the available task form width and aligns expanded fields and import consent', async ({ page }) => {
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

  await form.getByText('Review settings', { exact: true }).click();
  const title = (await form.getByRole('textbox', { name: 'Title', exact: true }).boundingBox())!;
  const project = (await form.getByRole('combobox', { name: 'Project', exact: true }).boundingBox())!;
  expect(project.x).toBe(title.x);
  expect(project.width).toBe(title.width);
  expect(project.y - title.y - title.height).toBeGreaterThanOrEqual(16);
  const issue = (await form.getByRole('combobox', { name: 'Issue service', exact: true }).boundingBox())!;
  expect(issue.x).toBe(title.x);
  expect(issue.width).toBe(title.width);

  await form.getByText('Import repository from URL', { exact: true }).click();
  const checkbox = form.getByRole('checkbox', { name: /Authorize the selected Git authentication/ });
  await expect(checkbox).toBeVisible();
  const clone = (await form.getByRole('textbox', { name: 'Clone URL', exact: true }).boundingBox())!;
  expect(clone.x).toBe(title.x);
  expect(clone.width).toBe(title.width);
  const consentGeometry = () => checkbox.evaluate(input => {
    const box = input.getBoundingClientRect();
    const text = input.nextElementSibling!.getBoundingClientRect();
    return { gap: text.left - box.right, topDelta: box.top - text.top };
  });
  expect(await consentGeometry()).toEqual({ gap: 8, topDelta: 4 });

  await page.setViewportSize({ width: 390, height: 844 });
  expect(await consentGeometry()).toEqual({ gap: 8, topDelta: 4 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390);
  expect(await page.evaluate(() => document.documentElement.scrollHeight)).toBe(844);
});
