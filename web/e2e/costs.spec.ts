import { expect, test } from '@playwright/test';

test('administrators price models and set budgets; viewers only read them', async ({ page }) => {
  await page.goto('/#/models/prices');
  await page.evaluate(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.reload();
  const prices = page.getByRole('form', { name: 'Model prices' });
  await expect(prices).toContainText(/No prices saved yet|Version/);
  await prices.getByRole('button', { name: 'Add price', exact: true }).click();
  const row = prices.getByRole('group').last();
  await row.getByLabel('Provider name').fill('e2e');
  await row.getByLabel(/^Input/).fill('2.5');
  await row.getByLabel(/^Output/).fill('10');
  await prices.getByRole('button', { name: 'Save prices', exact: true }).click();
  await expect(prices.getByRole('status')).toContainText(/Prices saved as version \d+\./);

  await page.getByRole('link',{name:'Budgets',exact:true}).click();
  const defaults = page.getByRole('form', {name:'Default budget per task'});
  await defaults.getByLabel(/^Task run budget/).fill('1.5');
  await defaults.getByRole('button',{name:'Save task defaults'}).click();
  await expect(defaults.getByRole('status')).toContainText('Defaults saved.');
  await page.getByRole('button', { name: 'Add monthly budget', exact: true }).click();
  const budgets = page.getByRole('form',{name:'Add monthly budget'});
  await budgets.getByLabel(/^Monthly limit/).fill('20');
  await budgets.getByRole('button', { name: 'Save monthly budget', exact: true }).click();
  await expect(page.getByRole('region',{name:'Current month budgets'}).getByRole('status')).toHaveText('Monthly budget added with a limit of $20.00.');
  await page.getByRole('link',{name:'Usage & costs',exact:true}).click();
  await expect(page.getByRole('group', { name: /^Spend in \d{4}-\d{2}$/ })).toBeVisible();
  await page.getByRole('link',{name:'Budgets',exact:true}).click();

  // Persisted on the server, not only in the page.
  await page.reload();
  await expect(page.getByRole('form', { name: 'Default budget per task' }).getByLabel(/^Task run budget/)).toHaveValue('1.5');
  await page.getByRole('link',{name:'Model prices',exact:true}).click();
  const saved = page.getByRole('form', { name: 'Model prices' }).getByRole('group').last();
  await expect(saved.getByLabel('Provider name')).toHaveValue('e2e');
  await expect(saved.getByLabel(/^Output/)).toHaveValue('10');

  // New tasks show the budget they are held to.
  await page.goto('/#/tasks/new');
  await page.getByText('Review settings').click();
  await page.getByRole('combobox', { name: 'Workflow', exact: true }).selectOption('git-inspect');
  await expect(page.getByRole('region', { name: 'Budget' })).toContainText('This run may spend up to $1.50');

  // Viewers see the same data with every control disabled.
  await page.evaluate(() => localStorage.setItem('weftToken', 'e2e-viewer-token'));
  await page.goto('/#/models/budgets');
  await page.reload();
  await page.getByRole('link',{name:'Model prices',exact:true}).click();
  await expect(page.getByRole('form', { name: 'Model prices' }).getByRole('button', { name: 'Save prices' })).toBeDisabled();
  await page.getByRole('link',{name:'Budgets',exact:true}).click();
  await expect(page.getByRole('form', { name: 'Default budget per task' }).getByRole('button', { name: 'Save task defaults' })).toBeDisabled();

  await expect(page.getByRole('button',{name:'Edit budget'})).toHaveCount(0);

  // Restore the disposable installation for later journeys.
  await page.evaluate(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  const reset = await page.request.put('/api/v1/cost/policy', { headers: { Authorization: 'Bearer e2e-admin-token' }, data: { taskRunBudgetMicros: 0, warningPercent: 80 } });
  expect(reset.ok()).toBe(true);
  expect((await page.request.put('/api/v1/cost/budgets', { headers: { Authorization: 'Bearer e2e-admin-token' }, data: { budgets: [] } })).ok()).toBe(true);
});

test('pending budget resumes survive a page reload and clear after recovery', async ({ page }) => {
  const graph = 'task-run-lease-fixture';
  const ledger = {
    scope: 'run', id: graph, currency: 'USD', limitMicros: 1_000_000, chargedMicros: 1_000_000,
    estimatedMicros: 0, reservedMicros: 0, remainingMicros: 0, reached: true, paused: true,
    notResumed: [] as string[],
  };
  // The API integration test holds and releases a real graph lease. Here
  // drive the browser with the corresponding persisted ledger responses.
  await page.route('**/api/v1/cost/ledgers', route => route.fulfill({ json: ledger.paused ? [ledger] : [] }));
  await page.route(`**/api/v1/cost/budgets/run/${graph}/raise`, route => {
    expect(route.request().postDataJSON()).toEqual({ limitMicros: 2_000_000 });
    Object.assign(ledger, { limitMicros: 2_000_000, remainingMicros: 1_000_000, reached: false, notResumed: [graph] });
    return route.fulfill({ json: ledger });
  });
  await page.goto('/#/models/budgets');
  await page.evaluate(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.reload();
  const budget = page.getByRole('listitem', { name: `Budget Task run ${graph}` });
  await expect(budget).toContainText('Runs paused');
  page.once('dialog', dialog => dialog.accept('2'));
  await budget.getByRole('button', { name: 'Raise budget' }).click();
  const pending = budget.getByRole('status');
  await expect(pending).toHaveText(`Resume pending for ${graph}. Weft will retry automatically.`);
  await page.reload();
  await expect(pending).toBeVisible();
  Object.assign(ledger, { paused: false, notResumed: [] });
  await page.reload();
  await expect(page.getByText('No budget has been used this month.')).toBeVisible();
  await expect(budget).toHaveCount(0);
});

test('an invalid ledger stays visible beside usable budgets', async ({ page }) => {
  const ledger = {
    scope: 'run', currency: 'USD', limitMicros: 1_000_000, chargedMicros: 1_000_000,
    estimatedMicros: 0, reservedMicros: 0, remainingMicros: 0, reached: true, paused: true,
  };
  await page.route('**/api/v1/cost/ledgers', route => route.fulfill({ json: [
    { ...ledger, id: 'task-run-invalid', invalid: true, revision: 7 },
    { ...ledger, id: 'task-run-valid' },
  ] }));
  await page.goto('/#/models/budgets');
  await page.evaluate(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.reload();

  const invalid = page.getByRole('listitem', { name: 'Budget Task run task-run-invalid' });
  await expect(invalid.getByRole('alert')).toContainText('Invalid budget ledger (revision 7)');
  await expect(invalid.getByRole('button', { name: 'Raise budget' })).toHaveCount(0);
  const valid = page.getByRole('listitem', { name: 'Budget Task run task-run-valid' });
  await expect(valid.getByRole('button', { name: 'Raise budget' })).toBeEnabled();
});

test('an insufficient raise leaves the hold visible without promising a retry', async ({ page }) => {
  const graph = 'task-run-insufficient-raise';
  const ledger = {
    scope: 'run', id: graph, currency: 'USD', limitMicros: 1_000_000, chargedMicros: 3_000_000,
    estimatedMicros: 0, reservedMicros: 0, remainingMicros: 0, reached: true, paused: true,
    raisedBy: '',
  };
  await page.route('**/api/v1/cost/ledgers', route => route.fulfill({ json: [ledger] }));
  await page.route(`**/api/v1/cost/budgets/run/${graph}/raise`, route => {
    expect(route.request().postDataJSON()).toEqual({ limitMicros: 2_000_000 });
    Object.assign(ledger, { limitMicros: 2_000_000, raisedBy: 'admin' });
    return route.fulfill({ json: ledger });
  });
  await page.goto('/#/models/budgets');
  await page.evaluate(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.reload();
  const budget = page.getByRole('listitem', { name: `Budget Task run ${graph}` });
  page.once('dialog', dialog => dialog.accept('2'));
  await budget.getByRole('button', { name: 'Raise budget' }).click();
  await expect(budget.getByRole('status')).toHaveText('The raised limit is still at or below current spend and reservations.');
  await expect(budget).toContainText('Runs paused');
  await expect(budget.getByText(/Resume pending/)).toHaveCount(0);
});

test('the graph card shows which budget blocked Resume', async ({ page }) => {
  const graph = 'budget-resume-fixture';
  const message = `Resume blocked by run budget ${graph}. Ask an administrator to raise it in Models → Costs.`;
  await page.route('**/api/v1/graphs', route => route.fulfill({ json: [{
    graph: { metadata: { id: graph, name: 'Budget resume fixture' }, spec: { nodes: [] } },
    record: { id: graph, version: 1, desiredState: 'paused', state: 'paused' },
  }] }));
  await page.route(`**/api/v1/graphs/${graph}/resume`, route => route.fulfill({
    status: 409, json: { code: 'budget_resume_blocked', error: message, details: { scope: 'run', id: graph } },
  }));
  await page.goto('/#/graphs');
  await page.evaluate(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.reload();
  const card = page.locator('.workflow-row').filter({ has: page.getByRole('button', { name: 'Budget resume fixture' }) });
  await card.getByRole('button', { name: 'Resume', exact: true }).click();
  await expect(card.getByRole('alert')).toHaveText(message);
});

test('edits a current monthly budget beside its real ledger, preserves spend and keeps task defaults independent',async({page})=>{
  const headers={Authorization:'Bearer e2e-admin-token'};
  expect((await page.request.put('/api/v1/cost/budgets',{headers,data:{budgets:[{scope:'installation',id:'*',limitMicros:10_000_000}]}})).ok()).toBe(true);
  // One deterministic in-process model call creates a real current-month ledger.
  const graph='budget-inline-edit';
  expect((await page.request.post('/api/v1/graphs',{headers,data:{metadata:{id:graph,name:'Budget edit fixture'},spec:{nodes:[{id:'worker',runtime:{kind:'native'},model:{provider:'e2e',name:'fixture'},prompt:'budget-edit-fixture',loop:{mode:'once'},limits:{maxTokens:128}}]}}})).status()).toBe(201);
  expect((await page.request.put('/api/v1/cost/prices',{headers,data:{currency:'USD',unpriced:'allow',prices:[{provider:'e2e',model:'*',inputMicrosPerMTok:2_500_000,outputMicrosPerMTok:10_000_000}]}})).ok()).toBe(true);
  const created=await page.request.post('/api/v1/tasks',{headers,data:{title:'Budget edit task',request:'budget-edit-fixture',outcome:'freeform',graphId:graph,graphVersion:1,nodeId:'worker',resultNodeId:'worker'}});
  expect(created.status()).toBe(201);const task=await created.json();
  expect((await page.request.post(`/api/v1/tasks/${task.id}/start`,{headers:{...headers,'If-Match':String(task.revision)},data:{confirm:true}})).status()).toBe(202);
  const run=`task-run-${task.id.slice(4)}`;
  await expect.poll(async()=>{
    const list=await (await page.request.get(`/api/v1/graphs/${run}/executions`,{headers})).json();
    return list[0]?.status;
  }).toBe('succeeded');
  const ledgers=await (await page.request.get('/api/v1/cost/ledgers',{headers})).json();
  const before=ledgers.find((b:{scope:string})=>b.scope==='installation');
  expect(before.chargedMicros).toBeGreaterThan(0);expect(before.reservedMicros).toBe(0);
  let policyWrites=0;
  page.on('request',request=>{if(request.method()==='PUT' && request.url().endsWith('/cost/policy')) policyWrites++;});
  await page.goto('/#/models/budgets');
  await page.evaluate(()=>localStorage.setItem('weftToken','e2e-admin-token'));await page.reload();
  const card=page.getByRole('listitem',{name:/Budget Installation ·/});
  const edit=card.getByRole('button',{name:'Edit budget'});
  await edit.click();
  const form=card.getByRole('form',{name:'Edit Installation budget'});
  const input=form.getByLabel(/^Monthly limit/);
  await expect(input).toHaveValue('10');await expect(input).toBeFocused();
  await input.fill('15');await form.getByRole('button',{name:'Cancel'}).click();
  await expect(edit).toBeFocused();await expect(form).toHaveCount(0);
  await edit.click();await expect(input).toHaveValue('10');await input.fill('20');
  await page.route('**/api/v1/cost/budgets',route=>route.request().method()==='PUT'?route.fulfill({status:503,json:{error:'Budget service unavailable',code:'unavailable'}}):route.continue());
  await form.getByRole('button',{name:'Save monthly budget'}).click();
  await expect(form.getByRole('alert')).toHaveText('Budget service unavailable');await expect(input).toHaveValue('20');
  expect((await (await page.request.get('/api/v1/cost/budgets',{headers})).json()).budgets[0].limitMicros).toBe(10_000_000);
  await page.unroute('**/api/v1/cost/budgets');
  await form.getByRole('button',{name:'Save monthly budget'}).click();
  await expect(page.getByRole('status')).toHaveText('Monthly limit updated from $10.00 to $20.00.');
  await expect(card.locator('.budget-amounts')).toContainText('$20.00');await expect(edit).toBeFocused();
  const after=(await (await page.request.get('/api/v1/cost/ledgers',{headers})).json()).find((b:{scope:string})=>b.scope==='installation');
  expect(after.chargedMicros).toBe(before.chargedMicros);expect(after.limitMicros).toBe(20_000_000);expect(policyWrites).toBe(0);
  await page.reload();await edit.click();await expect(input).toHaveValue('20');
  await page.setViewportSize({width:580,height:900});
  const scopeBox=await form.getByLabel('Scope',{exact:true}).boundingBox();const limitBox=await input.boundingBox();
  expect(Math.abs(scopeBox!.x-limitBox!.x)).toBeLessThan(2);expect(limitBox!.y).toBeGreaterThan(scopeBox!.y);
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBe(true);
  await form.getByRole('button',{name:'Cancel'}).click();
  await page.evaluate(()=>localStorage.setItem('weftToken','e2e-viewer-token'));await page.reload();
  await expect(page.getByRole('button',{name:'Edit budget'})).toHaveCount(0);
  await expect(page.getByRole('button',{name:'Save task defaults'})).toBeDisabled();
  expect((await page.request.put('/api/v1/cost/budgets',{headers:{Authorization:'Bearer e2e-viewer-token'},data:{budgets:[]}})).status()).toBe(403);
  expect((await page.request.put('/api/v1/cost/budgets',{headers,data:{budgets:[]}})).ok()).toBe(true);
});
