import {test,expect} from '@playwright/test';
const headers={Authorization:'Bearer e2e-admin-token'};
for(const decision of ['approve','reject'])test(`task journey shares approval identity and shows ${decision} outcome`,async({page})=>{
 const id=`journey-${decision}`;
 expect((await page.request.post('/api/v1/graphs',{headers,data:{metadata:{id},spec:{nodes:[{id:'worker',runtime:{kind:'echo'},loop:{mode:'event'}}],channels:[{id:'requests',to:['worker'],approval:true}]}}})).status()).toBe(201);
 const task=await(await page.request.post('/api/v1/tasks',{headers,data:{title:`Journey ${decision}`,request:'Explain the fixture',outcome:'freeform',graphId:id,graphVersion:1,nodeId:'worker',resultNodeId:'worker',channelId:'requests',messageType:'feature.request',payloadJson:'{}'}})).json();
 expect((await page.request.post(`/api/v1/tasks/${task.id}/start`,{headers:{...headers,'If-Match':String(task.revision)},data:{confirm:true}})).status()).toBe(202);
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));await page.goto(`/#/tasks/${task.id}`);
 await expect(page.getByRole('heading',{name:'Needs your decision',exact:true})).toBeVisible();
 const progress=await(await page.request.get(`/api/v1/tasks/${task.id}/progress`,{headers})).json();const approval=progress.pendingApprovals[0];
 const inbox=await(await page.request.get('/api/v1/approvals',{headers})).json();expect(inbox.some((a:{id:string})=>a.id===approval.id)).toBe(true);
 await page.getByRole('region',{name:'Task decisions'}).getByRole('button',{name:decision==='approve'?'Approve':'Reject',exact:true}).click();
 await expect(page.getByRole('heading',{name:decision==='approve'?'Ready for review':'Work needs attention',exact:true})).toBeVisible();
 await page.reload();await page.getByText('Decision history',{exact:true}).click();await expect(page.getByRole('region',{name:'Task journey'})).toContainText(decision==='approve'?'approved':'rejected');
 const after=await(await page.request.get(`/api/v1/tasks/${task.id}/progress`,{headers})).json();expect(after.decisions[0].id).toBe(approval.id);expect(after.pendingApprovals).toHaveLength(0);
});
