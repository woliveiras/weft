import { expect, test } from '@playwright/test';
const headers={Authorization:'Bearer e2e-admin-token'};
test('human reviews React and HTTP in a real sandbox before the agent continues',async({page,context})=>{
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 const start=await page.request.post('/api/v1/graphs/fixture-preview/start',{headers});expect(start.ok()).toBeTruthy();
 await page.goto('/#/approvals');const card=page.locator('article').filter({has:page.getByText('Review sandbox React and HTTP',{exact:true})});
 await expect(card).toBeVisible();await expect(card.getByRole('button',{name:'Open preview',exact:true})).toBeEnabled({timeout:60000});
 const before=await page.request.get('/api/v1/graphs/fixture-preview/executions',{headers});expect((await before.json()).length).toBe(1);
 const popup=context.waitForEvent('page');await card.getByRole('button',{name:'Open preview',exact:true}).click();const preview=await popup;
 await expect(preview.getByRole('heading',{name:'Sandbox review'})).toBeVisible();await preview.getByRole('button',{name:'Count: 0'}).click();await preview.getByRole('button',{name:'Save test value'}).click();await expect(preview.getByText('Saved: 1',{exact:true})).toBeVisible();await expect(preview.getByText('preview websocket',{exact:true})).toBeVisible();
 await preview.getByRole('link',{name:'Nested route'}).click();await expect(preview.getByRole('heading',{name:'Sandbox review'})).toBeVisible();
 const cookies=await context.cookies(preview.url());expect(cookies.some(c=>c.name==='application'&&c.value==='tested')).toBeTruthy();
 await preview.screenshot({path:'test-results-real/review-react.png'});
 // No completion while the human interacts with the preview.
 const waiting=await page.request.get('/api/v1/graphs/fixture-preview/executions',{headers});expect((await waiting.json()).length).toBe(1);
 await card.getByLabel('Review feedback').fill('React navigation, HTTP and WebSocket verified');await card.getByRole('button',{name:'Approve candidate'}).click();
 await expect(card).toContainText('approved');await expect.poll(async()=>{const result=await page.request.get('/api/v1/graphs/fixture-preview/executions',{headers});return (await result.json()).some((x:{summary:string})=>x.summary==='Human review consumed; work complete');}).toBeTruthy();
 await preview.reload();await expect(preview.getByText('Preview access expired or denied')).toBeVisible();
});


test('requesting changes returns feedback and requires a new human decision', async ({ page }) => {
 await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
 const stop = await page.request.post('/api/v1/graphs/fixture-preview/stop', { headers });
 expect(stop.ok()).toBeTruthy();
 await expect.poll(async () => {
  const result = await page.request.get('/api/v1/graphs/fixture-preview/status', { headers });
  return (await result.json()).state;
 }).toBe('stopped');
 expect((await page.request.post('/api/v1/graphs/fixture-preview/start', { headers })).ok()).toBeTruthy();
 await page.goto('/#/approvals');
 const pending = page.locator('article').filter({ has: page.getByRole('button', { name: 'Approve candidate', exact: true }) });
 await expect(pending).toHaveCount(1);
 await expect(pending.getByRole('button', { name: 'Open preview', exact: true })).toBeEnabled({ timeout: 60000 });
 await pending.getByLabel('Review feedback').fill('Please submit a new candidate for another review.');
 await pending.getByRole('button', { name: 'Request changes', exact: true }).click();
 await expect(page.locator('article').filter({ hasText: 'changes_requested' })).toHaveCount(1);
 await expect(pending).toHaveCount(1, { timeout: 60000 });
 await expect(pending.getByRole('button', { name: 'Open preview', exact: true })).toBeEnabled({ timeout: 60000 });
 await pending.getByLabel('Review method').selectOption('without_execution');
 await pending.getByRole('button', { name: 'Approve candidate', exact: true }).click();
 await expect(pending).toHaveCount(0);
});
