import {test,expect} from '@playwright/test';
const admin={Authorization:'Bearer e2e-admin-token'};
test.beforeEach(async({page})=>{await page.addInitScript(()=>{if(!localStorage.getItem('weftToken'))localStorage.setItem('weftToken','e2e-admin-token');});});

test('network review preserves drafts, previews exact changes and persists only after confirmation',async({page})=>{
 const original=await(await page.request.get('/api/v1/network-policy',{headers:admin})).json();
 try{
  await page.goto('/#/administration/network');
  const hosts=page.getByLabel('Installation allowed hosts (one exact DNS name per line)',{exact:true});
  const target=[...(original.network.egressHosts??[]),'example.org'].join('\n');
  await hosts.fill(target);
  page.once('dialog',dialog=>dialog.dismiss());
  await page.getByRole('link',{name:'Tool catalog',exact:true}).click();
  await expect(page).toHaveURL(/administration\/network$/);await expect(hosts).toHaveValue(target);
  page.once('dialog',dialog=>dialog.dismiss());
  await page.getByRole('button',{name:'Reload network policy',exact:true}).click();await expect(hosts).toHaveValue(target);
  await hosts.fill('*.example.org');await page.getByRole('button',{name:'Review network policy',exact:true}).click();
  await expect(page.getByRole('alert')).toContainText('Preview failed');await expect(hosts).toHaveValue('*.example.org');
  await hosts.fill(target);await page.getByRole('button',{name:'Review network policy',exact:true}).click();
  const review=page.getByRole('region',{name:'Confirm network policy',exact:true});
  await expect(review).toBeFocused();await expect(review).toContainText('Added hosts: example.org');
  expect((await(await page.request.get('/api/v1/network-policy',{headers:admin})).json()).revision).toBe(original.revision);
  await page.getByRole('button',{name:'Cancel network changes'}).click();await expect(page.getByRole('button',{name:'Review network policy',exact:true})).toBeFocused();
  await page.getByRole('button',{name:'Review network policy',exact:true}).click();
  await page.getByRole('button',{name:'Confirm network policy',exact:true}).click();
  await expect(page.getByText(/Network policy saved/)).toBeVisible();
  expect((await(await page.request.get('/api/v1/network-policy',{headers:admin})).json()).network.egressHosts).toContain('example.org');
 }finally{
  const latest=await(await page.request.get('/api/v1/network-policy',{headers:admin})).json();
  expect((await page.request.put('/api/v1/network-policy',{headers:admin,data:{network:original.network,revision:latest.revision}})).ok()).toBe(true);
 }
});

test('retention explains reductions and retains edits after a failed save before a new review',async({page})=>{
 const original=await(await page.request.get('/api/v1/blocked-history-retention',{headers:admin})).json();
 try{
  await page.goto('/#/administration/retention');const days=page.getByLabel('Retention days (1–90)',{exact:true});
  await days.fill('1');await page.getByRole('button',{name:'Review retention changes',exact:true}).click();
  await expect(page.getByRole('region',{name:'Confirm history retention'})).toContainText('History limits are being reduced');
  await page.getByRole('button',{name:'Cancel retention changes'}).click();
  await days.fill('9');
  await page.route('**/api/v1/blocked-history-retention',async route=>route.request().method()==='PUT'?route.fulfill({status:503,json:{error:'Temporary write failure',code:'unavailable'}}):route.continue());
  await page.getByRole('button',{name:'Review retention changes',exact:true}).click();await page.getByRole('button',{name:'Confirm retention changes',exact:true}).click();
  await expect(page.getByRole('alert')).toContainText('Temporary write failure');await expect(days).toHaveValue('9');await expect(page.getByRole('button',{name:'Confirm retention changes',exact:true})).toHaveCount(0);
  expect((await(await page.request.get('/api/v1/blocked-history-retention',{headers:admin})).json()).revision).toBe(original.revision);
  await page.unroute('**/api/v1/blocked-history-retention');
  await page.getByRole('button',{name:'Review retention changes',exact:true}).click();await page.getByRole('button',{name:'Confirm retention changes',exact:true}).click();
  await expect(page.getByText(/Retention saved/)).toBeVisible();expect((await(await page.request.get('/api/v1/blocked-history-retention',{headers:admin})).json()).policy.days).toBe(9);
 }finally{
  const latest=await(await page.request.get('/api/v1/blocked-history-retention',{headers:admin})).json();
  expect((await page.request.put('/api/v1/blocked-history-retention',{headers:admin,data:{policy:original.policy,revision:latest.revision,confirm:true}})).ok()).toBe(true);
 }
});

test('catalogue bounds rows, combines filters and adapts to a narrow screen',async({page})=>{
 await page.goto('/#/administration/tools');const table=page.getByRole('table');
 await expect(table.locator('tbody tr')).toHaveCount(20);await page.getByRole('button',{name:'Next',exact:true}).click();
 await expect(page.getByText(/Page 2 of/)).toBeVisible();
 await page.getByRole('searchbox',{name:'Search tools',exact:true}).fill('check.run');
 await expect(table.locator('tbody tr')).toHaveCount(1);await expect(table).toContainText('declared project check');
 await page.getByRole('combobox',{name:'Capability',exact:true}).selectOption('filesystem.read');await expect(page.getByText('No tools match these filters.')).toBeVisible();
 await page.getByRole('button',{name:'Clear filters'}).click();await expect(table.locator('tbody tr')).toHaveCount(20);
 await page.setViewportSize({width:580,height:900});
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 await page.goto('/#/administration/delegation');await page.getByRole('button',{name:'New delegation profile',exact:true}).click();
 await expect(page).toHaveURL(/delegation\/profiles\/new$/);await expect(page.getByRole('dialog')).toHaveCount(0);
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 await page.getByRole('button',{name:'Cancel profile changes'}).click();await expect(page).toHaveURL(/administration\/delegation$/);
});

test('resource preview loads automatically and selection stays explicit across filtered pages',async({page})=>{
 // The disposable backend has no Docker daemon: fixture only the read-only preview boundary.
 let mutations=0;
 await page.route('**/api/v1/environments/cleanup',async route=>{
  const body=route.request().postDataJSON();if(!body.dryRun)mutations++;
  await route.fulfill({json:{generations:Array.from({length:23},(_,i)=>({id:`generation-${String(i).padStart(2,'0')}`,image:`weft/test:${i}`,eligible:i!==0,usedBy:i===0?['workflow-using-image']:[]}))}});
 });
 await page.goto('/#/administration/resources');const table=page.getByRole('table');await expect(table.locator('tbody tr')).toHaveCount(20);
 await expect(page.getByRole('checkbox',{name:'Select generation-00',exact:true})).toBeDisabled();
 await page.getByRole('checkbox',{name:'Select generation-01',exact:true}).check();await page.getByRole('button',{name:'Next',exact:true}).click();
 await page.getByRole('checkbox',{name:'Select generation-22',exact:true}).check();await page.getByRole('searchbox',{name:'Search resources',exact:true}).fill('workflow-using-image');
 await expect(table.locator('tbody tr')).toHaveCount(1);await expect(page.getByText(/2 selected across all pages and filters/)).toBeVisible();
 await page.getByRole('button',{name:'Review cleanup (2)',exact:true}).click();await expect(page.getByRole('region',{name:'Confirm shared resource cleanup'})).toContainText('weft/test:22');
 await page.getByRole('button',{name:'Cancel cleanup'}).click();expect(mutations).toBe(0);
 await page.getByRole('button',{name:'Clear selection'}).click();
 await page.evaluate(()=>localStorage.setItem('weftToken','e2e-viewer-token'));await page.reload();
 await expect(page.getByText('Administrator access is required to inspect and clean shared resources.')).toBeVisible();expect(mutations).toBe(0);
});

test('profile page recovers a real revision conflict without losing edits or changing grants',async({page})=>{
 await page.goto('/#/administration/delegation/profiles/new');
 await page.getByLabel('Profile ID',{exact:true}).fill('admin-review-cas');await page.getByLabel('Profile name',{exact:true}).fill('CAS review');await page.getByLabel('Profile instructions',{exact:true}).fill('Review supplied text without requesting tools.');
 await page.getByRole('button',{name:'Review delegation profile',exact:true}).click();await page.getByRole('button',{name:'Confirm delegation profile',exact:true}).click();
 await expect(page).toHaveURL(/administration\/delegation$/);await page.getByRole('button',{name:'Edit CAS review',exact:true}).click();
 await page.getByLabel('Profile name',{exact:true}).fill('Local review');
 const saved=await(await page.request.get('/api/v1/delegation-profiles',{headers:admin})).json();const source=saved.find((row:{profile:{id:string}})=>row.profile.id==='admin-review-cas');
 expect((await page.request.put('/api/v1/delegation-profiles/admin-review-cas',{headers:{...admin,'If-Match':String(source.revision)},data:{...source.profile,name:'Concurrent review'}})).ok()).toBe(true);
 await page.getByRole('button',{name:'Review delegation profile',exact:true}).click();await page.getByRole('button',{name:'Confirm delegation profile',exact:true}).click();
 await expect(page.getByRole('alert')).toContainText('Save could not be confirmed');await expect(page.getByLabel('Profile name',{exact:true})).toHaveValue('Local review');
 await page.getByRole('button',{name:'Reload saved profile'}).click();await expect(page.getByRole('heading',{name:'Edit Concurrent review'})).toBeVisible();
 await page.getByRole('button',{name:'Review delegation profile',exact:true}).click();await expect(page.getByRole('region',{name:'Changes to review'})).toContainText('Concurrent review');
 await page.getByRole('button',{name:'Confirm delegation profile',exact:true}).click();await expect(page).toHaveURL(/administration\/delegation$/);
 const latest=(await(await page.request.get('/api/v1/delegation-profiles',{headers:admin})).json()).find((row:{profile:{id:string}})=>row.profile.id==='admin-review-cas');
 expect(latest.profile.name).toBe('Local review');expect(latest.revision).toBe(source.revision+2);expect(latest.profile.tools??[]).toEqual([]);
 await page.getByRole('button',{name:'Edit Local review',exact:true}).click();await page.evaluate(()=>localStorage.setItem('weftToken','e2e-viewer-token'));await page.reload();
 await expect(page.getByLabel('Profile name',{exact:true})).toBeDisabled();await expect(page.getByRole('button',{name:'Review delegation profile',exact:true})).toHaveCount(0);
 await page.goto('/#/administration/delegation/profiles/missing-profile/edit');await expect(page.getByText('Delegation profile not found.')).toBeVisible();await expect(page.getByRole('form',{name:'Profile editor'})).toHaveCount(0);
});
