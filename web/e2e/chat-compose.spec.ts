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
    const box=await send.boundingBox();
    expect(box!.y+box!.height).toBeGreaterThan(800);
    expect(box!.y+box!.height).toBeLessThanOrEqual(900);
    await message.press(width===390?'Meta+Enter':'Control+Enter');
    await expect(page).toHaveURL(/#\/chat\/[a-f0-9]{32}/);
    await expect(page.getByText('/inspect tasks',{exact:true})).toBeVisible();
    await expect(message).toHaveValue('');
  });
}
