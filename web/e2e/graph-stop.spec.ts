import {test,expect} from '@playwright/test';
const headers={Authorization:'Bearer e2e-admin-token'};
test('repeated graph start and stop preserves the latest operator command',async({page})=>{
 const id='observed-state-stop';
 expect((await page.request.post('/api/v1/graphs',{headers,data:{metadata:{id,name:id},spec:{nodes:[{id:'worker',runtime:{kind:'echo'},loop:{mode:'once'}}]}}})).status()).toBe(201);
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 await page.goto('/#/g/'+id+'/canvas');
 for(let turn=0;turn<5;turn++){
  await page.getByRole('button',{name:'Run all',exact:true}).click();
  await page.getByRole('button',{name:'Stop all',exact:true}).click();
  await expect.poll(async()=>{
   const result=await page.request.get('/api/v1/graphs/'+id,{headers});
   const {record}=await result.json();return [record.desiredState,record.state];
  }).toEqual(['stopped','stopped']);
  await expect(page.getByRole('button',{name:'Run all',exact:true})).toBeEnabled();
 }
});
