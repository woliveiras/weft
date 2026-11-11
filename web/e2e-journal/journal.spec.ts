import { expect, test } from '@playwright/test';

test('replays real process and transient events captured while the controller was offline', async ({page}) => {
 await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
 await page.goto('/#/g/journal-capture/files');
 const git=page.getByRole('region',{name:'Git inspection'});
 await expect.poll(async () => {
  const response=await page.request.get('/api/v1/graphs/journal-capture/events?types=workspace.observed',{headers:{Authorization:'Bearer e2e-admin-token'}});
  const events=await response.json();
  const kernel=events.filter((event:{data:{source?:string}})=>event.data.source==='kernel-journal');
  const operations=kernel.flatMap((event:{data:{changes:{path:string;operation:string}[]}})=>event.data.changes.filter(change=>change.path==='transient').map(change=>change.operation));
  return operations.includes('created')&&operations.includes('deleted');
 }).toBe(true);
 await git.getByRole('button',{name:'Refresh Git'}).click();
 await expect(git).toContainText('transient (created)');
 await expect(git).toContainText('transient (deleted)');
 await expect(git).toContainText('kernel PID');
 await expect(git).toContainText('execution unknown');
 await expect(git).toContainText('coverage gap');
 const response=await page.request.get('/api/v1/graphs/journal-capture/events?types=workspace.observed',{headers:{Authorization:'Bearer e2e-admin-token'}});
 const events=await response.json();
 const kernel=events.filter((event:{data:{source?:string}})=>event.data.source==='kernel-journal');
 expect(new Set(kernel.map((event:{id:string})=>event.id)).size).toBe(kernel.length);
 expect(kernel.every((event:{executionId?:string})=>!event.executionId)).toBe(true);
});
