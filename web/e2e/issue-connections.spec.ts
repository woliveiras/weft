import {expect, test} from '@playwright/test';

test('native issue connections use dedicated pages, scoped credentials and explicit identity-check feedback', async ({page}) => {
  await page.goto('/#/connections');
  await page.evaluate(()=>localStorage.setItem('weftToken','e2e-admin-token'));
  await page.reload();
  const panel=page.getByRole('region',{name:'Issue trackers',exact:true});
  await panel.getByRole('link',{name:'Add connection',exact:true}).click();
  await expect(page).toHaveURL(/#\/connections\/issues\/new$/);
  const form=page.getByRole('form',{name:'Add issue tracker connection'});
  await form.getByLabel('Connection name',{exact:true}).fill('e2e-native-jira');
  await form.getByLabel('Site URL',{exact:true}).fill('https://jira.fixture.invalid/context');
  await form.getByLabel('Project ID',{exact:true}).fill('10001');
  await form.getByLabel('Personal access token',{exact:true}).fill('synthetic-native-browser-token');
  await expect(form.getByRole('checkbox',{name:/I verified/})).not.toBeChecked();
  await form.getByRole('checkbox',{name:/I verified/}).check();
  await page.setViewportSize({width:390,height:844});
  expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBe(390);
  await page.setViewportSize({width:1280,height:800});
  await form.getByRole('button',{name:'Help for Project ID'}).click();
  const help=page.getByRole('dialog',{name:'Project ID',exact:true});
  await expect(help).toContainText('immutable numeric Jira project ID');
  await help.getByRole('button',{name:'Close',exact:true}).click();
  await form.getByRole('button',{name:'Save connection',exact:true}).click();
  await expect(page).toHaveURL(/#\/connections$/);
  const row=panel.getByRole('row',{name:/e2e-native-jira/});
  await expect(row).toContainText('Project ID 10001');
  await expect(row).toContainText('Not checked in this session');
  let release!:()=>void;
  const gate=new Promise<void>(resolve=>{release=resolve;});
  // Response injection covers browser feedback only; real TLS and role/revision
  // boundaries are covered by the Go API/Runner fixtures. No vendor is called.
  await page.route('**/api/v1/issue-connections/e2e-native-jira/test',async route=>{
    expect(route.request().headers()['if-match']).toBe('1');
    await gate;
    await route.fulfill({status:200,contentType:'application/json',body:JSON.stringify({ok:false,error:'Fixture: identity could not be verified'})});
  });
  await row.getByRole('button',{name:'Test connection',exact:true}).click();
  try {
    await expect(row).toContainText('Checking identity…');
    await expect(row.getByRole('button',{name:'Testing…'})).toBeDisabled();
  } finally {release();}
  await expect(row).toContainText('Check failed');
  await expect(row).toContainText('Fixture: identity could not be verified');
  await row.getByRole('link',{name:'Edit',exact:true}).click();
  const edit=page.getByRole('form',{name:'Edit issue tracker connection'});
  await expect(edit.getByLabel('Personal access token',{exact:true})).toHaveValue('');
  await expect(edit.getByLabel('Connection name',{exact:true})).toBeDisabled();
  await expect(edit.getByRole('checkbox',{name:/I verified/})).toBeChecked();
  await edit.getByLabel('Personal access token',{exact:true}).fill('synthetic-replacement-token');
  await expect(edit.getByRole('checkbox',{name:/I verified/})).not.toBeChecked();
  await edit.getByRole('checkbox',{name:/I verified/}).check();
  await edit.getByLabel('Project ID',{exact:true}).fill('10002');
  await expect(edit.getByLabel('Personal access token',{exact:true})).toHaveValue('');
  await expect(edit.getByRole('checkbox',{name:/I verified/})).not.toBeChecked();
  await expect(edit).toContainText('replacement PAT');
  await page.getByRole('link',{name:'Cancel',exact:true}).click();
  const saved=await (await page.request.get('/api/v1/issue-connections/e2e-native-jira',{headers:{Authorization:'Bearer e2e-admin-token'}})).json();
  expect(saved.projectId).toBe('10001');
  expect(saved).not.toHaveProperty('token');
  expect(saved).not.toHaveProperty('sealedToken');
  await expect(page.locator('body')).not.toContainText('synthetic-native-browser-token');
  await panel.getByRole('searchbox',{name:'Search connections'}).fill('10001');
  await panel.getByRole('combobox',{name:'Connection status'}).selectOption('enabled');
  await expect(row).toBeVisible();
  const search=await panel.getByRole('searchbox').boundingBox();
  const status=await panel.getByRole('combobox').boundingBox();
  expect(search).not.toBeNull();expect(status).not.toBeNull();
  expect(Math.abs(search!.y-status!.y)).toBeLessThan(2);
  await page.setViewportSize({width:390,height:844});
  expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBe(390);
  await page.setViewportSize({width:1280,height:800});
  await row.getByRole('link',{name:'Inspect transitions'}).click();
  await expect(page).toHaveURL(/\/transitions$/);
  let discoveryCalls=0;
  await page.route('**/api/v1/issue-connections/e2e-native-jira/transitions',async route=>{
    discoveryCalls++;
    expect(route.request().postDataJSON()).toEqual({issueKey:'APP-7'});
    expect(route.request().headers()['if-match']).toBe('1');
    await route.fulfill({status:200,contentType:'application/json',body:JSON.stringify({connection:'e2e-native-jira',revision:1,discovery:{site:'https://jira.fixture.invalid/context',projectId:'10001',issueId:'10007',issueKey:'APP-7',issueTypeId:'10002',current:{id:'10',name:'Ready'},updated:'2026-11-20T12:00:00Z',transitions:[{id:'31',name:'Review',to:{id:'20',name:'Review'},fields:{}},{id:'32',name:'Review',to:{id:'21',name:'Review'},fields:null}]}})});
  });
  const inspector=page.getByRole('form',{name:'Inspect issue transitions'});
  await inspector.getByLabel('Issue key',{exact:true}).fill('APP-7');
  await inspector.getByRole('button',{name:'Discover transitions',exact:true}).click();
  const transitionTable=page.getByRole('region',{name:'Transition table'});
  await expect(transitionTable.getByRole('row')).toHaveCount(3);
  await expect(transitionTable).toContainText('Transition ID 31');
  await expect(transitionTable).toContainText('Field metadata unavailable');
  expect(discoveryCalls).toBe(1);
  await page.setViewportSize({width:390,height:844});
  expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBe(390);

});
