import { expect, test } from '@playwright/test';

test('runs a task then inspects its files, diff, history and execution context', async ({ page }) => {
 await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
 await page.goto('/#/g/git-inspect/canvas');
 await page.getByRole('button', { name: 'Run all', exact: true }).click();
 await expect.poll(async () => {
  const response = await page.request.get('/api/v1/graphs/git-inspect/executions', { headers: { Authorization: 'Bearer e2e-admin-token' } });
  return (await response.json()).some((run: {status:string}) => run.status === 'succeeded');
 }).toBe(true);
 await expect.poll(async () => {
  const response = await page.request.get('/api/v1/graphs/git-inspect/events?types=workspace.observed', {headers:{Authorization:'Bearer e2e-admin-token'}});
  const events = await response.json();
  const monitored = events.filter((event:{executionId?:string; data:{source?:string}}) => !event.executionId && event.data.source === 'monitor');
  const paths = monitored.flatMap((event:{data:{changes:{path:string}[]}}) => event.data.changes.map(change => change.path));
  return ['background-one.txt','background-two.txt','concurrent.txt'].every(path => paths.includes(path));
 }).toBe(true);
 await page.getByRole('tab', { name: 'Files', exact: true }).click();
 const git = page.getByRole('region', {name:'Git inspection'});
 await expect(git).toContainText('M result.txt');
 await expect(git).toContainText('Inspection task finished');
 await expect(git).toContainText('filesystem.write');
 await expect(git).toContainText('shell.exec');
 await expect(git).toContainText('background-one.txt (created)');
 await expect(git).toContainText('author unknown');
 await expect(git).toContainText('execution unknown');
 await expect(git).toContainText('shell.txt (created)');
 await expect(git).toContainText('Change attribution is inconclusive');
 await page.getByRole('button', {name:/result.txt/}).click();
 await expect(page.getByRole('region', {name:'File contents'})).toContainText('updated by fixture task');
 await git.getByLabel('Git view').selectOption('diff');
 await expect(git).toContainText('+updated by fixture task');
 await expect(git).toContainText('-baseline result');
 await git.getByLabel('Git view').selectOption('history');
 await expect(git).toContainText('inspection baseline');
 for (const endpoint of ['files','files/content','git/diff']) {
  const response = await page.request.delete(`/api/v1/graphs/git-inspect/${endpoint}`, {headers:{Authorization:'Bearer e2e-admin-token'}});
  expect(response.status()).toBe(405);
 }
});

test('read-only agent cannot write through filesystem or shell despite its grants', async ({page}) => {
 await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
 await page.goto('/#/g/git-inspect-readonly/canvas');
 await page.getByRole('button', {name:'Run all',exact:true}).click();
 await expect.poll(async () => {
  const response = await page.request.get('/api/v1/graphs/git-inspect-readonly/events?types=tool.denied', {headers:{Authorization:'Bearer e2e-admin-token'}});
  const events = await response.json();
  return events.filter((event:{data:{error?:string}}) => event.data.error?.includes('read-only')).length;
 }).toBe(2);
 await page.getByRole('tab', {name:'Files',exact:true}).click();
 await expect(page.getByRole('region',{name:'Git inspection'})).toContainText('Working tree clean.');
 await page.getByRole('button', {name:/result.txt/}).click();
 await expect(page.getByRole('region',{name:'File contents'})).toContainText('baseline result');
});

test('late file responses cannot replace a newer selection',async({page})=>{
 const headers={Authorization:'Bearer e2e-admin-token'};
 expect((await page.request.post('/api/v1/graphs/git-inspect/stop',{headers})).ok()).toBe(true);
 await expect.poll(async()=>((await (await page.request.get('/api/v1/graphs/git-inspect',{headers})).json()).record.state)).toBe('stopped');
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 await page.goto('/#/g/git-inspect/canvas');
 await page.getByRole('button',{name:'Run all',exact:true}).click();
 await expect.poll(async()=>{
  const response=await page.request.get('/api/v1/graphs/git-inspect/files',{headers});
  return (await response.json()).some((file:{name:string})=>file.name==='shell.txt');
 }).toBe(true);
 await page.getByRole('tab',{name:'Files',exact:true}).click();
 let release!:()=>void;
 const gate=new Promise<void>(resolve=>release=resolve);
 let intercepted=false;
 let delivered=false;
 await page.route('**/api/v1/graphs/git-inspect/files/content?*',async route=>{
  if(new URL(route.request().url()).searchParams.get('path')!=='result.txt') return route.continue();
  const response=await route.fetch();
  intercepted=true;
  await gate;
  await route.fulfill({response});
  delivered=true;
 });
 await page.getByRole('button',{name:/result.txt/}).click();
 await expect.poll(()=>intercepted).toBe(true);
 await page.getByRole('button',{name:/shell.txt/}).click();
 const content=page.getByRole('region',{name:'File contents'}).locator('pre').last();
 await expect(content).toHaveText('shell change\n');
 release();
 await expect.poll(()=>delivered).toBe(true);
 await expect(content).toHaveText('shell change\n');
 for(const method of ['POST','PUT','PATCH','DELETE']){
  for(const endpoint of ['files','files/content','git/diff']){
   const response=await page.request.fetch('/api/v1/graphs/git-inspect/'+endpoint,{method,headers,data:{path:'shell.txt',content:'forbidden'}});
   expect([404,405],method+' '+endpoint).toContain(response.status());
  }
 }
 const persisted=await page.request.get('/api/v1/graphs/git-inspect/files/content?path=shell.txt',{headers});
 expect(await persisted.text()).toBe('shell change\n');
});
