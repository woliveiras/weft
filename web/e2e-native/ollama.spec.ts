import {test,expect} from '@playwright/test';
import {readFileSync,existsSync} from 'node:fs';
import {join} from 'node:path';

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
 await page.goto('/#/models');await page.getByLabel('API token').fill(token);await page.getByRole('button',{name:'Continue',exact:true}).click();
 const plugin=page.getByRole('listitem',{name:'Inference plugin Ollama'});
 await expect(plugin).toContainText('Local');
 if(await plugin.getByRole('button',{name:'Enable',exact:true}).isVisible())await plugin.getByRole('button',{name:'Enable',exact:true}).click();
 await expect(plugin.getByText('Enabled',{exact:true})).toBeVisible();
 await page.getByRole('button',{name:'Add provider',exact:true}).click();
 const form=page.getByRole('dialog',{name:'Add model provider'});
 await form.getByLabel(/^Name/).fill('local-ollama');
 await form.getByRole('combobox',{name:/^Type/}).selectOption('ollama');
 await form.getByLabel(/^Base URL/).fill(ollama);
 await form.getByLabel(/^Default model/).fill(model);
 await form.getByRole('button',{name:'Test settings',exact:true}).click();
 await expect(form.getByRole('status')).toContainText(`Inference works with ${model}`,{timeout:300_000});
 await form.getByRole('button',{name:'Add provider',exact:true}).click();
 const card=page.getByRole('article',{name:'Provider local-ollama',exact:true});
 await expect(card.getByText('Default',{exact:true})).toBeVisible();

 const graph={metadata:{id:'ollama-writer',name:'Ollama writer'},spec:{nodes:[{id:'writer',loop:{mode:'once'},limits:{maxToolCalls:4,turnTimeout:'8m'},
  prompt:'Use the filesystem.write tool to create the file hello.txt whose content is exactly the word ok. Then answer with the single word done.',
  tools:[{name:'filesystem.write',paths:['hello.txt']}]}]}};
 expect((await page.request.post('/api/v1/graphs',{headers,data:graph})).status()).toBe(201);
 const created=await page.request.post('/api/v1/tasks',{headers,data:{title:'Write hello',request:'Create hello.txt as instructed.',outcome:'implement',graphId:'ollama-writer',graphVersion:1,nodeId:'writer',resultNodeId:'writer'}});
 expect(created.status()).toBe(201);const task=await created.json();
 const review=await(await page.request.get(`/api/v1/tasks/${task.id}/authorization`,{headers})).json();
 expect((await page.request.put(`/api/v1/tasks/${task.id}/authorization`,{headers:{...headers,'If-Match':'0'},data:{mode:'full',scope:review.scope,confirm:true}})).ok()).toBe(true);
 expect((await page.request.post(`/api/v1/tasks/${task.id}/start`,{headers:{...headers,'If-Match':String(task.revision)},data:{confirm:true}})).status()).toBe(202);
 await page.goto(`/#/tasks/${task.id}`);
 await expect(page.getByRole('status').filter({hasText:'Task status:'})).toHaveText('Task status: succeeded',{timeout:480_000});
 const graphId=`task-run-${task.id.slice(4)}`;
 const candidates=[join(root,'state','workspaces',graphId,'hello.txt'),join(root,'state','workspaces','.worktrees',graphId,'writer-0','hello.txt')];
 const file=candidates.find(p=>existsSync(p));
 expect(file,`hello.txt in ${candidates.join(' or ')}`).toBeTruthy();
 expect(readFileSync(file!,'utf8').trim().toLowerCase()).toContain('ok');
 // The call was recorded for cost accounting at the local zero price.
 const events=await(await page.request.get(`/api/v1/graphs/${graphId}/events?types=cost.recorded`,{headers})).json();
 expect(JSON.stringify(events)).toContain('"rateSource":"local"');
});
