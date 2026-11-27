import {expect,type Page} from '@playwright/test';
import {readFileSync} from 'node:fs';
import {join} from 'node:path';
import type {TaskProgress} from '../src/api/tasks';
import type {WeftEvent} from '../src/api/types';

export async function runWriterTask(page:Page,headers:Record<string,string>,type:'ollama'|'gemini',provider:string,model:string) {
 const graphId=type+'-writer';
 const graph={metadata:{id:graphId},spec:{nodes:[{
  id:'writer',runtime:{kind:'native'},workspace:{mode:'shared'},model:{provider,name:model},loop:{mode:'once'},limits:{maxToolCalls:4,turnTimeout:'8m'},
  prompt:'Call the provided write tool to create hello.txt containing exactly ok. After the tool succeeds, answer done.',
  tools:[{name:'filesystem.write',paths:['hello.txt']}],
 }]}};
 expect((await page.request.post('/api/v1/graphs',{headers,data:graph})).status()).toBe(201);
 const created=await page.request.post('/api/v1/tasks',{headers,data:{
  title:'Write hello',request:'Create hello.txt as instructed.',outcome:'implement',graphId,graphVersion:1,nodeId:'writer',resultNodeId:'writer',
 }});
 expect(created.status()).toBe(201);
 const task=await created.json();
 const endpoint=`/api/v1/tasks/${task.id}`;
 const review=await(await page.request.get(endpoint+'/authorization',{headers})).json();
 expect((await page.request.put(endpoint+'/authorization',{headers:{...headers,'If-Match':'0'},data:{mode:'full',scope:review.scope,confirm:true}})).ok()).toBe(true);
 expect((await page.request.post(endpoint+'/start',{headers:{...headers,'If-Match':String(task.revision)},data:{confirm:true}})).status()).toBe(202);
 await page.goto('/#/tasks/'+task.id);
 await expect(page.getByRole('region',{name:'Task status',exact:true})).toContainText('Task status: succeeded',{timeout:480_000});

 const progress:TaskProgress=await(await page.request.get(endpoint+'/progress',{headers})).json();
 expect(progress.result).toMatchObject({nodeId:'writer',status:'succeeded'});
 const run=progress.result!;
 const response=await page.request.get(`/api/v1/graphs/${run.graphId}/events?types=tool.completed,cost.recorded`,{headers});
 expect(response.ok()).toBe(true);
 const events:WeftEvent[]=await response.json();
 const records=events.filter(event=>event.executionId===run.id);
 expect(records).toContainEqual(expect.objectContaining({type:'tool.completed',data:expect.objectContaining({
  tool:'filesystem.write',outcome:'ok',args:expect.objectContaining({path:'hello.txt'}),
 })}));
 const file=join(process.env.WEFT_NATIVE_E2E_DATA!,'state','workspaces',run.graphId,'hello.txt');
 expect(readFileSync(file,'utf8').trim()).toBe('ok');
 const costs=records.filter(event=>event.type==='cost.recorded');
 expect(costs.length).toBeGreaterThan(0);
 for(const event of costs){
  expect(event.data).toMatchObject({source:'gateway',type,provider,model});
  const data=event.data as {usage:{inputTokens:number;outputTokens:number}};
  expect(data.usage.inputTokens).toBeGreaterThan(0);
  expect(data.usage.outputTokens).toBeGreaterThan(0);
  if(type==='ollama')expect(event.data).toMatchObject({rateSource:'local',costMicros:0});
 }
}
