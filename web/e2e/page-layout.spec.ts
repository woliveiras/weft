import {test,expect} from '@playwright/test';
const destinations=['Activity','Tasks','Approvals','Projects','Workflows','Agents','Connections','Models','Skills & instructions','Administration'];

for(const width of [2560,1440,390])test(`main destinations share content edges at ${width}px`,async({page})=>{
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-viewer-token'));
 await page.setViewportSize({width,height:1000});await page.goto('/#/activity');
 const nav=page.getByRole('navigation',{name:'Main navigation'});
 for(const name of destinations){
  await nav.getByRole('button',{name,exact:true}).click();await expect(page.getByRole('heading',{name,level:1,exact:true})).toBeVisible();
  const box=await page.locator('main').evaluate(main=>{
   const root=main.querySelector(':scope > .page')!;
   const frame=main.getBoundingClientRect();const content=root.getBoundingClientRect();const heading=root.querySelector('h1')!.getBoundingClientRect();const style=getComputedStyle(root);
   return {left:content.left-frame.left,width:content.width,available:main.clientWidth,heading:heading.left-frame.left,paddingRight:parseFloat(style.paddingRight),overflow:document.documentElement.scrollWidth>innerWidth};
  });
  expect(box.left,name).toBeCloseTo(0,0);expect(box.width,name).toBeCloseTo(box.available,0);
  expect(box.heading,name).toBe(width<=720?16:32);expect(box.paddingRight,name).toBe(width<=720?16:32);expect(box.overflow,name).toBe(false);
 }
 if(width===2560){
  await nav.getByRole('button',{name:'Collapse navigation',exact:true}).click();
  await nav.getByRole('button',{name:'Activity',exact:true}).click();await expect(page.getByRole('heading',{name:'Activity',exact:true})).toBeVisible();
  const padding=await page.locator('main > .page').evaluate(el=>parseFloat(getComputedStyle(el).paddingLeft));expect(padding).toBe(32);
 }
});

test('wide destinations retain alignment on failure and constrain explanatory text separately',async({page})=>{
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-viewer-token'));
 await page.setViewportSize({width:2560,height:1000});
 await page.route('**/api/v1/graphs',route=>route.fulfill({status:503,json:{error:'Layout fixture unavailable',code:'unavailable'}}));
 await page.goto('/#/graphs');await expect(page.getByText('Layout fixture unavailable',{exact:true})).toBeVisible();
 const box=await page.locator('main > .page').evaluate(el=>({width:el.getBoundingClientRect().width,available:el.parentElement!.clientWidth,left:parseFloat(getComputedStyle(el).paddingLeft)}));
 expect(box.width).toBe(box.available);expect(box.left).toBe(32);
 const copy=await page.locator('.page-head p').evaluate(el=>({limit:parseFloat(getComputedStyle(el).maxWidth),available:el.parentElement!.parentElement!.getBoundingClientRect().width}));expect(copy.limit).toBeGreaterThan(0);expect(copy.limit).toBeLessThan(copy.available);
});
