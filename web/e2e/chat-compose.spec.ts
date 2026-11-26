import {expect,test} from '@playwright/test';

for (const width of [1440,390]) {
  test('completes slash commands from the bottom composer at '+width+'px', async ({page})=>{
    await page.setViewportSize({width,height:900});
    await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
    await page.goto('/#/chat');
    const message=page.getByRole('textbox',{name:'Message',exact:true});
    const send=page.getByRole('button',{name:'Send',exact:true});
    await message.fill('/ins');
    await page.getByRole('option',{name:/^\/inspect /}).click();
    await page.getByRole('option',{name:'/inspect tasks',exact:true}).click();
    await expect(message).toHaveValue('/inspect tasks');
    if (width===390) expect((await page.getByRole('navigation',{name:'Main navigation'}).boundingBox())!.height).toBeLessThan(80);
    const box=await send.boundingBox();
    expect(box!.y+box!.height).toBeGreaterThan(800);
    expect(box!.y+box!.height).toBeLessThanOrEqual(900);
    await message.press(width===390?'Meta+Enter':'Control+Enter');
    await expect(page).toHaveURL(/#\/chat\/[a-f0-9]{32}/);
    await expect(page.getByText('/inspect tasks',{exact:true})).toBeVisible();
    await expect(message).toHaveValue('');
  });
}

test('retries a saved request in place with a distinct action below its text',async({page,request})=>{
  const headers={Authorization:'Bearer e2e-admin-token'};
  const {revision}=await(await request.get('/api/v1/chat/settings',{headers})).json();
  expect((await request.put('/api/v1/chat/settings',{headers:{...headers,'If-Match':String(revision)},data:{provider:'e2e',name:'fixture'}})).ok()).toBe(true);
  const id='c6f04fb16cd3474f8fa4bc3a5fb6699b';
  const text='A request outside the fixture model';
  expect((await request.post('/api/v1/chat/sessions',{headers,data:{requestId:id,text}})).ok()).toBe(true);
  await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
  await page.goto('/#/chat/'+id);
  const bubble=page.getByRole('article').filter({has:page.getByText(text,{exact:true})});
  const retry=bubble.getByRole('button',{name:'Retry',exact:true});
  await expect(retry).toBeVisible();
  const button=await retry.boundingBox();
  const content=await bubble.getByText(text,{exact:true}).boundingBox();
  expect(button!.y-content!.y-content!.height).toBeGreaterThanOrEqual(10);
  const response=page.waitForResponse(r=>r.url().endsWith('/messages/'+id+'/retry') && r.request().method()==='POST');
  await retry.click();
  expect((await response).ok()).toBe(true);
  await expect(bubble).toHaveCount(1);
});
