import {test,expect} from '@playwright/test';
import {readFileSync} from 'node:fs';
import {join} from 'node:path';
import {runWriterTask} from './live-writer';

// Opt-in: a real local model through the Ollama inference plugin. Set
// WEFT_E2E_OLLAMA_URL (and optionally WEFT_E2E_OLLAMA_MODEL); the model
// must support tool calling. Never runs against a hosted service.
const ollama=process.env.WEFT_E2E_OLLAMA_URL??'';
const model=process.env.WEFT_E2E_OLLAMA_MODEL??'llama3.2:latest';

test('a real Ollama model is enabled, configured and runs a task through the gateway',async({page})=>{
 test.skip(!ollama,'set WEFT_E2E_OLLAMA_URL to run against a local Ollama');
 test.setTimeout(600_000);
 const root=process.env.WEFT_NATIVE_E2E_DATA!;
 const token=readFileSync(join(root,'state','admin.token'),'utf8').trim();
 const headers={Authorization:`Bearer ${token}`};
 await page.goto('/#/models/plugins');await page.getByLabel('API token').fill(token);await page.getByRole('button',{name:'Continue',exact:true}).click();
 const plugin=page.getByRole('listitem',{name:'Inference plugin Ollama'});
 await expect(plugin).toContainText('Local');
 if(await plugin.getByRole('button',{name:'Enable',exact:true}).isVisible())await plugin.getByRole('button',{name:'Enable',exact:true}).click();
 await expect(plugin.getByText('Enabled',{exact:true})).toBeVisible();
 await page.goto('/#/models/providers');
 await page.getByRole('button',{name:'Add provider',exact:true}).click();
 const form=page.getByRole('form',{name:'Provider settings'});
 await form.getByLabel(/^Name/).fill('local-ollama');
 await form.getByRole('combobox',{name:/^Type/}).selectOption('ollama');
 await form.getByLabel(/^Base URL/).fill(ollama);
 await form.getByLabel(/^Default model/).fill(model);
 await form.getByRole('button',{name:'Test settings',exact:true}).click();
 await expect(form.getByRole('status')).toContainText(`Inference works with ${model}`,{timeout:300_000});
 await form.getByRole('button',{name:'Add provider',exact:true}).click();
 const card=page.getByRole('article',{name:'Provider local-ollama',exact:true});
 await expect(card).toBeVisible();
 const makeDefault=card.getByRole('button',{name:'Make default',exact:true});
 if(await makeDefault.isVisible())await makeDefault.click();
 await expect(card.getByText('Default',{exact:true})).toBeVisible();

 await runWriterTask(page,headers,'ollama','local-ollama',model);
});
