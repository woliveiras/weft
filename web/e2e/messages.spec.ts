import { expect, test } from '@playwright/test';

test('opens a message trace from the card or keyboard without intercepting payload inspection', async ({page}) => {
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  const injected = await page.request.post('/api/v1/graphs/activity-approve/channels/tasks/messages', {
    headers: {Authorization:'Bearer e2e-admin-token'},
    data: {type:'trace.card.requested', payload:{instruction:'Inspect this trace'}},
  });
  expect(injected.ok()).toBe(true);
  const {id} = await injected.json();
  await page.goto('/#/g/activity-approve/messages');

  const card = page.getByRole('complementary').getByRole('listitem').filter({ hasText:'trace.card.requested' });
  const correlation = page.getByRole('textbox', { name:'Correlation id' });
  const payload = card.getByRole('button', {name:'payload'});
  await payload.click();
  await expect(payload).toHaveAttribute('aria-expanded', 'true');
  await expect(correlation).toHaveValue('');

  await card.getByText('trace.card.requested', {exact:true}).click();
  await expect(correlation).toHaveValue(id);
  await expect(page.locator('.tree')).toContainText('trace.card.requested');

  await page.goBack();
  await expect(correlation).toHaveValue('');
  await card.getByRole('button', {name:/trace workflow/}).press('Enter');
  await expect(correlation).toHaveValue(id);
});
