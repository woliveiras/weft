import { expect, test } from '@playwright/test';
const headers = {Authorization: 'Bearer e2e-admin-token'};
test('saves a disabled managed skill from its reviewed text', async ({page, request}) => {
  const settings = await (await request.get('/api/v1/chat/settings', {headers})).json();
  expect((await request.put('/api/v1/chat/settings', {headers:{...headers,'If-Match':String(settings.revision)},data:{provider:'e2e',name:'fixture'}})).ok()).toBe(true);
  await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
  await page.goto('/');
  await page.getByRole('textbox',{name:'Message',exact:true}).fill('Create an editorial skill for checking release notes.');
  await page.getByRole('button',{name:'Send',exact:true}).click();
  const review=page.getByRole('region',{name:'Managed skill revision'});
  await expect(review).toContainText('Check the release notes for dates.');
  expect((await request.get('/api/v1/skills/editorial-notes',{headers})).status()).toBe(404);
  await review.getByRole('button',{name:'Save skill revision'}).click();
  await expect(review).toContainText('Skill revision 1 saved.');
  expect(await (await request.get('/api/v1/skills/editorial-notes',{headers})).json()).toMatchObject({revision:1,enabled:false,global:false});
  await review.getByRole('link',{name:'View skill'}).click();
  await expect(page).toHaveURL(/#\/skills\/s\/editorial-notes\/content/);
});
