import {expect,test} from '@playwright/test';
import {readFileSync} from 'node:fs';
import {join} from 'node:path';
import {runWriterTask} from './live-writer';

const model=process.env.WEFT_E2E_GEMINI_MODEL;
test('a real Gemini model writes the authorized file through the native gateway',async({page,baseURL})=>{
 test.skip(!model,'set WEFT_E2E_GEMINI_MODEL to opt into a billed Gemini task');
 test.setTimeout(600_000);
 const apiKey=process.env.WEFT_CLOUD_API_KEY;
 if(!apiKey)throw new Error('WEFT_CLOUD_API_KEY is required for the Gemini task');
 const origin=new URL(baseURL!);
 expect(origin.protocol).toBe('http:');
 expect(origin.hostname).toBe('127.0.0.1');
 const token=readFileSync(join(process.env.WEFT_NATIVE_E2E_DATA!,'state','admin.token'),'utf8').trim();
 const headers={Authorization:`Bearer ${token}`};
 await page.goto('/#/models/plugins');
 await page.getByLabel('API token').fill(token);
 await page.getByRole('button',{name:'Continue',exact:true}).click();
 const plugin=page.getByRole('listitem',{name:'Inference plugin Google Gemini'});
 await expect(plugin).toBeVisible();
 const enable=plugin.getByRole('button',{name:'Enable',exact:true});
 if(await enable.isVisible())await enable.click();
 await expect(plugin.getByText('Enabled',{exact:true})).toBeVisible();

 // Use Node's HTTP client so the provider credential never enters browser or Playwright traces.
 const provider='live-gemini';
 const created=await fetch(new URL('/api/v1/providers',origin),{
  method:'POST',headers:{...headers,'Content-Type':'application/json'},redirect:'error',
  body:JSON.stringify({name:provider,type:'gemini',defaultModel:model,apiKey:{value:apiKey}}),
 });
 expect(created.status).toBe(201);
 await page.goto('/#/models/providers');
 await page.getByRole('button',{name:'Refresh providers',exact:true}).click();
 await expect(page.getByRole('article',{name:'Provider '+provider,exact:true})).toContainText(model!);
 await runWriterTask(page,headers,'gemini',provider,model!);
});
