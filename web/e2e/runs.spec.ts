import { expect,test } from '@playwright/test';
const headers={Authorization:'Bearer e2e-admin-token'};
const graph=(id:string)=>({metadata:{id,name:id},spec:{nodes:[{id:'worker',runtime:{kind:'echo'},prompt:'Run result fixture',loop:{mode:'once'},approval:{beforeTurn:true}}]}});
async function createRun(request:import('@playwright/test').APIRequestContext,id:string) {
 expect((await request.post('/api/v1/graphs',{headers,data:graph(id)})).status()).toBe(201);
 expect((await request.post(`/api/v1/graphs/${id}/start`,{headers})).ok()).toBe(true);
 await expect.poll(async()=>{const r=await request.get(`/api/v1/graphs/${id}/executions`,{headers});return (await r.json()).length;}).toBe(1);
 const runs=await (await request.get(`/api/v1/graphs/${id}/executions`,{headers})).json();return runs[0].id as string;
}
test('navigates from runs to approval, completion, linked history and diagnostics',async({page})=>{
 const graphId='runs-browser';const execution=await createRun(page.request,graphId);
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 await page.goto(`/#/g/${graphId}/runs`);await page.getByLabel('Search runs').fill(execution);
 await page.getByRole('link',{name:execution,exact:true}).click();
 const approvals=page.getByRole('region',{name:'Run approvals'});await approvals.getByRole('button',{name:'Approve',exact:true}).click();
 await expect.poll(async()=> (await (await page.request.get(`/api/v1/executions/${execution}`,{headers})).json()).status).toBe('succeeded');
 await page.getByRole('button',{name:'Refresh run',exact:true}).click();await expect(page.getByText('Completed',{exact:true})).toBeVisible();
 const nav=page.getByRole('navigation',{name:'Run sections'});await nav.getByRole('link',{name:'Timeline',exact:true}).click();
 await expect(page.getByRole('region',{name:'Run timeline'})).toContainText('execution.completed');
 await page.reload();await expect(nav.getByRole('link',{name:'Timeline',exact:true})).toHaveAttribute('aria-current','page');
 await nav.getByRole('link',{name:'Messages',exact:true}).click();await expect(page.getByRole('region',{name:'Run messages'})).toContainText('No linked messages');
 await nav.getByRole('link',{name:'Diagnostics',exact:true}).click();await expect(page.getByRole('region',{name:'Run detail'})).toContainText(execution);
 await page.goBack();await expect(nav.getByRole('link',{name:'Messages',exact:true})).toHaveAttribute('aria-current','page');
});
test('viewer cannot decide approvals and missing or mismatched runs are explicit',async({page})=>{
 const graphId='runs-viewer';const execution=await createRun(page.request,graphId);
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-viewer-token'));await page.setViewportSize({width:580,height:850});
 await page.goto(`/#/g/${graphId}/runs?execution=${execution}&section=result`);
 await expect(page.getByRole('region',{name:'Run approvals'})).toContainText('Waiting for an authorized operator');await expect(page.getByRole('button',{name:'Approve',exact:true})).toHaveCount(0);
 await page.goto(`/#/g/journal-capture/runs?execution=${execution}&section=result`);await expect(page.getByRole('alert')).toContainText('does not belong');await expect(page.getByRole('region',{name:'Run approvals'})).toHaveCount(0);
 await page.goto(`/#/g/${graphId}/runs?execution=missing-execution&section=result`);await expect(page.getByRole('alert')).toBeVisible();await expect(page.getByRole('navigation',{name:'Run sections'})).toHaveCount(0);
});
test('recovers a failed timeline request without hiding the run outcome',async({page})=>{
 const graphId='runs-retry';const execution=await createRun(page.request,graphId);
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 await page.route(`**/api/v1/graphs/${graphId}/events?*`,r=>r.fulfill({status:503,contentType:'application/json',body:JSON.stringify({error:'History unavailable',code:'unavailable'})}));
 await page.goto(`/#/g/${graphId}/runs?execution=${execution}&section=timeline`);await expect(page.getByText('History unavailable',{exact:true})).toBeVisible();await expect(page.getByRole('navigation',{name:'Run sections'})).toBeVisible();
 await page.unroute(`**/api/v1/graphs/${graphId}/events?*`);await page.getByRole('button',{name:'Refresh run events',exact:true}).click();await expect(page.getByText('History unavailable',{exact:true})).toHaveCount(0);await expect(page.getByRole('region',{name:'Run timeline'})).toContainText('execution.started');
});
