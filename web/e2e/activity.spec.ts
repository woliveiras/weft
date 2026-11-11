import { expect, test } from '@playwright/test';

test.beforeEach(async ({page}) => {
  await page.addInitScript(() => localStorage.setItem('weftToken','e2e-admin-token'));
});
test('follows a task through human approval, execution, result, events and files', async ({page}) => {
  await page.goto('/#/g/activity-approve/activity');
  const activity=page.getByLabel('Task activity',{exact:true});
  await expect(activity.getByText(/No executions recorded yet/)).toBeVisible();
  const injected=await page.request.post('/api/v1/graphs/activity-approve/channels/tasks/messages',{headers:{Authorization:'Bearer e2e-admin-token'},data:{type:'inspection.request',payload:{instruction:'Inspect the task workspace'}}});
  expect(injected.ok()).toBeTruthy();
  await page.getByRole('button',{name:'Run all',exact:true}).click();
  const approvals=page.getByRole('region',{name:'Task approvals'});
  await expect(approvals.getByRole('button',{name:'Approve',exact:true})).toBeVisible();
  await approvals.getByRole('button',{name:'Approve',exact:true}).click();
  await expect(page.getByRole('region',{name:'Executions and results'})).toContainText('Inspection task finished');
  await expect(page.getByRole('region',{name:'Task events'})).toContainText('execution.completed');
  await expect(page.getByRole('region',{name:'Task messages'})).toContainText('inspection.request');
  const options=activity.getByRole('combobox',{name:'Execution',exact:true}).locator('option');
  await expect(options).toHaveCount(2);
  await activity.getByRole('combobox',{name:'Execution',exact:true}).selectOption({index:1});
  await expect(page.getByRole('region',{name:'Task events'})).toContainText('execution.completed');
  await page.reload();
  await expect(activity).toContainText('Inspection task finished');
  await activity.getByRole('link',{name:'Inspect files and diff'}).click();
  await expect(page.getByRole('region',{name:'Git inspection'})).toContainText('M result.txt');
  await page.getByRole('tab',{name:'Activity',exact:true}).click();
  await expect(activity).toContainText('Inspection task finished');
});
test('keeps rejection visible without claiming a completed result', async ({page}) => {
  await page.goto('/#/g/activity-reject/activity');
  await page.getByRole('button',{name:'Run all',exact:true}).click();
  const approvals=page.getByRole('region',{name:'Task approvals'});
  await approvals.getByRole('button',{name:'Reject',exact:true}).click();
  await expect(approvals).toContainText('rejected');
  await expect(page.getByRole('region',{name:'Task events'})).toContainText('approval.decided');
  await expect(page.getByRole('region',{name:'Executions and results'})).not.toContainText('Inspection task finished');
});

test('reports a failed history request and recovers without hiding other sections', async ({page}) => {
  await page.route('**/api/v1/graphs/activity-reject/events?*', route => route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({error:'Activity history temporarily unavailable',code:'unavailable'})}));
  await page.goto('/#/g/activity-reject/activity');
  await expect(page.getByText('events: Activity history temporarily unavailable')).toBeVisible();
  await expect(page.getByRole('region',{name:'Graph progress'})).toContainText('Graph:');
  await page.unroute('**/api/v1/graphs/activity-reject/events?*');
  const response=page.waitForResponse(r=>r.url().includes('/graphs/activity-reject/events?') && r.status()===200);
  await page.getByRole('button',{name:'Refresh activity',exact:true}).click();
  await response;
  await expect(page.getByText('events: Activity history temporarily unavailable')).toHaveCount(0);
  await expect(page.getByRole('region',{name:'Task events'})).toContainText('graph.created');
  await expect(page.getByText('reviews: Human reviews are not configured on this installation.')).toBeVisible();
});
