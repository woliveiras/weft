import { expect, test } from '@playwright/test';

test.use({ locale: 'pt-BR' });

test('opens help on the workflow creation page and preserves a draft', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.setViewportSize({ width: 580, height: 850 });
  await page.goto('/#/graphs');
  await page.getByRole('button', { name: 'New workflow', exact: true }).click();
  await page.getByRole('button', { name: 'Advanced YAML / JSON',exact:true }).click();
  const workflow = page.getByRole('region', { name: 'Advanced workflow definition', exact: true });
  const document = workflow.getByRole('textbox', { name: 'Workflow document', exact: true });
  const upload = workflow.getByRole('button', { name: 'Upload a workflow document' });
  await expect(upload).toHaveAccessibleDescription('No file selected');
  const chooser = page.waitForEvent('filechooser');
  await upload.press('Enter');
  await (await chooser).setFiles({ name: 'revisão.yaml', mimeType: 'application/yaml', buffer: Buffer.from('name: sample') });
  await expect(upload).toHaveAccessibleDescription('revisão.yaml');
  await expect(document).toHaveValue('name: sample');
  await document.fill('metadata: {name: draft}');
  const help = workflow.getByRole('button', { name: 'Help for Workflow document', exact: true });
  await help.focus();
  await page.keyboard.press('Enter');
  const popup = page.getByRole('dialog', { name: 'Workflow document', exact: true });
  await expect(popup).toContainText('Define the workflow in YAML or JSON');
  await expect(popup.getByRole('button', { name: 'Close', exact: true })).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(popup).toBeHidden();
  await expect(workflow).toBeVisible();
  await expect(help).toBeFocused();
  await expect(document).toHaveValue('metadata: {name: draft}');
});

test('lets a viewer consult installation policy help', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-viewer-token'));
  await page.goto('/#/administration/network');
  const hosts = page.getByRole('textbox', { name: /^Installation allowed hosts/ });
  await expect(hosts).toBeDisabled();
  const help = page.getByRole('button', { name: /^Help for Installation allowed hosts/ });
  await help.click();
  const popup = page.getByRole('dialog', { name: /^Installation allowed hosts/ });
  await expect(popup).toContainText('one exact hostname per line');
  await popup.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(popup).toBeHidden();
  await expect(help).toBeFocused();
  await expect(hosts).toBeDisabled();
});
