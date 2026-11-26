import {expect,test} from '@playwright/test';

test('reviews, corrects and reopens a saved task draft',async ({page,request})=>{
  const headers={Authorization:'Bearer e2e-admin-token'};
  const graph={metadata:{id:'chat-correction',name:'Correction fixture'},spec:{nodes:[{id:'reader',runtime:{kind:'echo'},loop:{mode:'once'},tools:[]}]}};
  expect((await request.post('/api/v1/graphs',{headers,data:graph})).ok()).toBe(true);
  const created=await request.post('/api/v1/tasks',{headers,data:{title:'Correctable release checklist',request:'Check release notes.',outcome:'review',graphId:'chat-correction',graphVersion:1,nodeId:'reader'}});
  expect(created.ok()).toBe(true);
  const task=await created.json();
  const settings=await (await request.get('/api/v1/chat/settings',{headers})).json();
  expect((await request.put('/api/v1/chat/settings',{headers:{...headers,'If-Match':String(settings.revision)},data:{provider:'e2e',name:'fixture'}})).ok()).toBe(true);
  await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
  await page.goto('/');
  await page.getByRole('textbox',{name:'Message',exact:true}).fill('Correct the release checklist draft to review dates.');
  await page.getByRole('button',{name:'Send',exact:true}).click();
  const review=page.getByRole('region',{name:'Task draft correction'});
  await expect(review).toContainText('Check release notes.');
  await expect(review).toContainText('Review the release dates.');
  await review.getByRole('button',{name:'Save draft correction'}).click();
  await expect(review).toContainText('Task draft revision 2 saved.');
  expect(await (await request.get('/api/v1/tasks/'+task.id,{headers})).json()).toMatchObject({request:'Review the release dates.',revision:2,status:'draft'});
  await review.getByRole('link',{name:'Open task draft'}).click();
  await expect(page).toHaveURL(new RegExp(task.id));
});
