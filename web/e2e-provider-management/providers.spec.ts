import {test,expect} from '@playwright/test';
import {createServer} from 'node:http';
import type {AddressInfo} from 'node:net';

test('configures a provider in the browser and keeps stored keys out of success and failure responses',async({page})=>{
 const key='synthetic-browser-provider-key';
 let fail=false;
 const bodies:string[]=[];
 const upstream=createServer(async(req,res)=>{
  res.setHeader('Content-Type','application/json');
  if(fail||req.headers.authorization!=='Bearer '+key){
   res.writeHead(401);res.end(JSON.stringify({error:{message:'Invalid key '+key}}));return;
  }
  if(req.url==='/v1/models'){res.end(JSON.stringify({data:[{id:'fixture-small',object:'model'}]}));return;}
  if(req.url!=='/v1/chat/completions'){res.writeHead(404);res.end('{}');return;}
  let raw='';for await(const chunk of req)raw+=chunk;
  bodies.push(raw);
  res.end(JSON.stringify({model:'fixture-small',choices:[{message:{role:'assistant',content:'OK'},finish_reason:'stop'}],usage:{prompt_tokens:12,completion_tokens:1}}));
 });
 await new Promise<void>(resolve=>upstream.listen(0,'127.0.0.1',resolve));
 try{
  await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
  await page.goto('/#/models/plugins');
  const openaiPlugin=page.getByRole('listitem',{name:'Inference plugin OpenAI'});await expect(openaiPlugin).toBeVisible();
  if(await openaiPlugin.getByRole('button',{name:'Enable',exact:true}).isVisible())await openaiPlugin.getByRole('button',{name:'Enable',exact:true}).click();
  await expect(openaiPlugin.getByText('Enabled',{exact:true})).toBeVisible();
  await page.getByRole('link',{name:'Providers',exact:true}).click();
  await page.getByRole('button',{name:'Add provider',exact:true}).click();
  const form=page.getByRole('form',{name:'Provider settings'});
  await form.getByLabel(/^Name/).fill('browser-provider');
  await form.getByRole('combobox',{name:/^Type/}).selectOption('openai');
  await form.getByLabel(/^Base URL/).fill('http://127.0.0.1:'+(upstream.address() as AddressInfo).port+'/v1');
  await form.getByLabel(/^Default model/).fill('fixture-small');
  await form.getByLabel(/^API key/).fill(key);
  await form.getByRole('button',{name:'Add provider',exact:true}).click();
  const card=page.getByRole('article',{name:'Provider browser-provider',exact:true});
  await expect(card).toBeVisible();
  await card.getByRole('button',{name:'Test connection',exact:true}).click();
  await expect(card).toContainText('Inference works with fixture-small');
  expect(bodies.length).toBeGreaterThan(0);
  expect(bodies.join('')).not.toContain(key);
  const headers={Authorization:'Bearer e2e-admin-token'};
  const list=await page.request.get('/api/v1/providers',{headers});
  expect(list.ok()).toBe(true);expect(await list.text()).not.toContain(key);
  await card.getByRole('button',{name:'Edit',exact:true}).click();
  await expect(page.getByRole('form',{name:'Provider settings'}).getByLabel(/^API key/)).toHaveValue('');
  await page.getByRole('form',{name:'Provider settings'}).getByRole('button',{name:'Cancel',exact:true}).click();
  await card.getByRole('button',{name:'Edit',exact:true}).click();
  const editor=page.getByRole('form',{name:'Provider settings'});
  await expect(page).toHaveURL(/models\/providers\/browser-provider\/edit$/);
  await page.reload();
  await expect(editor.getByLabel('API key',{exact:true})).toHaveValue('');
  await editor.getByText('Limits',{exact:true}).click();
  await editor.getByLabel('Context window (tokens)',{exact:true}).fill('65536');
  await page.route('**/api/v1/providers/browser-provider',route=>route.request().method()==='PUT'
    ? route.fulfill({status:503,json:{error:'Provider update temporarily unavailable'}}) : route.continue());
  await editor.getByRole('button',{name:'Save changes',exact:true}).click();
  await expect(editor.getByRole('alert')).toContainText('Provider update temporarily unavailable');
  await expect(editor.getByLabel('Context window (tokens)',{exact:true})).toHaveValue('65536');
  await page.unroute('**/api/v1/providers/browser-provider');
  await editor.getByRole('button',{name:'Save changes',exact:true}).click();
  await expect(card).toContainText('65,536 tokens');
  const updated=await page.request.get('/api/v1/providers',{headers});
  expect(await updated.text()).not.toContain(key);

  fail=true;
  const response=page.waitForResponse(r=>r.url().endsWith('/providers/browser-provider/test'));
  await card.getByRole('button',{name:'Test connection',exact:true}).click();
  expect(await(await response).text()).not.toContain(key);
  await expect(card).toContainText('Inference failed');
  await expect(page.locator('body')).not.toContainText(key);
  expect(await page.evaluate(()=>JSON.stringify({...localStorage}))).not.toContain(key);
  expect(await page.evaluate(()=>JSON.stringify({...sessionStorage}))).not.toContain(key);
  await page.reload();
  await expect(card).toBeVisible();
 }finally{
  await new Promise<void>((resolve,reject)=>upstream.close(error=>error?reject(error):resolve()));
 }
});
