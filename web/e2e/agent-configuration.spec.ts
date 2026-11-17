import { test, expect } from '@playwright/test';
const headers={Authorization:'Bearer e2e-admin-token'};
const graph=(id:string)=>({apiVersion:'weft.io/v1',kind:'Graph',metadata:{id,name:'Configuration fixture'},spec:{nodes:[
 {id:'reviewer',name:'Reviewer',runtime:{kind:'native'},model:{provider:'e2e',name:'fixture'},prompt:'Review repositories',loop:{mode:'once'},workspace:{readOnly:true},tools:[{name:'filesystem.read',paths:['src/**'],requiresApproval:true}],config:{keep:'unchanged'}},
 {id:'helper',name:'Helper',runtime:{kind:'echo'},prompt:'Assist',loop:{mode:'once'},tools:[]}
]}});

test('edits sections and multiple agents, reviews the whole graph and preserves grants',async({page})=>{
 const id='agent-settings-browser';const created=await page.request.post('/api/v1/graphs',{headers,data:graph(id)});expect(created.status()).toBe(201);
 expect((await page.request.post('/api/v1/mcp/connections',{headers,data:{id:'agent-pages',name:'Agent pages fixture',service:'generic',endpoint:'https://mcp.fixture.invalid/mcp',enabled:true,credential:{value:'fixture-mcp-token'}}})).status()).toBe(201);
 expect((await page.request.post('/api/v1/mcp/connections/agent-pages/tools/refresh',{headers})).ok()).toBe(true);
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 await page.goto(`/#/g/${id}/canvas?sel=node:reviewer`);await page.getByRole('button',{name:'Configure agent',exact:true}).click();
 await expect(page).toHaveURL(/\/agent\?agent=reviewer&section=general$/);
 await expect(page.getByRole('button',{name:'Run all',exact:true})).toHaveCount(0);
 const config=page.getByRole('region',{name:'Agent configuration',exact:true});
 await config.getByLabel('Name',{exact:true}).fill('Reviewer updated');
 const sections=page.getByRole('navigation',{name:'Agent configuration sections'});
 await sections.getByRole('button',{name:'Instructions',exact:true}).click();await page.getByLabel('Agent instructions',{exact:true}).fill('Review changes carefully');
 await sections.getByRole('button',{name:'Tools & permissions',exact:true}).click();
 await expect(page.getByRole('complementary',{name:'Execution boundaries'})).toContainText('Read-only');
 await expect(page.getByRole('checkbox',{name:'Require approval for filesystem.read',exact:true})).toBeChecked();
 await page.getByRole('checkbox',{name:'mcp.agent-pages.read_issue',exact:true}).check();
 await sections.getByRole('button',{name:'Advanced',exact:true}).click();await config.getByLabel('Max tool calls',{exact:true}).fill('12');
 await page.getByLabel('Agent',{exact:true}).selectOption('helper');await config.getByLabel('Name',{exact:true}).fill('Helper updated');
 await page.getByRole('button',{name:'Review & save',exact:true}).click();
 const review=page.getByRole('dialog',{name:'Review graph changes'});
 await expect(review).toContainText('spec.nodes[0].prompt');await expect(review).toContainText('spec.nodes[1].name');
 await review.getByRole('button',{name:'Continue editing',exact:true}).click();
 const before=await (await page.request.get(`/api/v1/graphs/${id}`,{headers})).json();expect(before.graph.spec.nodes[0].name).toBe('Reviewer');
 await page.getByRole('button',{name:'Review & save',exact:true}).click();await Promise.all([page.waitForResponse(r=>r.url().endsWith(`/api/v1/graphs/${id}`)&&r.request().method()==='PUT'&&r.status()===200),page.getByRole('button',{name:'Save graph draft',exact:true}).click()]);
 await expect(page.getByText('Saved in graph version 2',{exact:true})).toBeVisible();
 await expect(page.getByRole('button',{name:'Review & save',exact:true})).toBeDisabled();
 const saved=await (await page.request.get(`/api/v1/graphs/${id}`,{headers})).json();
 expect(saved.graph.spec.nodes[0]).toMatchObject({name:'Reviewer updated',prompt:'Review changes carefully',limits:{maxToolCalls:12},workspace:{readOnly:true},tools:[...graph(id).spec.nodes[0].tools,{name:'mcp.agent-pages.read_issue'}],config:{keep:'unchanged'}});
 expect(saved.graph.spec.nodes[1].name).toBe('Helper updated');
 await page.getByLabel('Agent',{exact:true}).selectOption('reviewer');await sections.getByRole('button',{name:'Instructions',exact:true}).click();await page.reload();await expect(page.getByLabel('Agent instructions',{exact:true})).toHaveValue('Review changes carefully');
 await sections.getByRole('button',{name:'Tools & permissions',exact:true}).click();await page.goBack();await expect(sections.getByRole('button',{name:'Instructions',exact:true})).toHaveAttribute('aria-current','page');
});

test('reports invalid instructions and keeps the draft for correction',async({page})=>{
 const id='agent-settings-invalid';expect((await page.request.post('/api/v1/graphs',{headers,data:graph(id)})).status()).toBe(201);
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));await page.goto(`/#/g/${id}/agent?agent=reviewer&section=instructions`);
 await page.getByLabel('Agent instructions',{exact:true}).fill('');await page.getByRole('button',{name:'Review & save',exact:true}).click();await page.getByRole('button',{name:'Save graph draft',exact:true}).click();
 await expect(page.getByText(/native agents need a prompt/).first()).toBeVisible();await expect(page.getByLabel('Agent instructions',{exact:true})).toHaveValue('');
 await page.getByLabel('Agent instructions',{exact:true}).fill('Corrected instructions');await page.getByRole('button',{name:'Review & save',exact:true}).click();await page.getByRole('button',{name:'Save graph draft',exact:true}).click();await expect(page.getByRole('button',{name:'Review & save',exact:true})).toBeDisabled();
});

test('viewer configuration stays read-only on a narrow viewport',async({page})=>{
 const id='agent-settings-viewer';expect((await page.request.post('/api/v1/graphs',{headers,data:graph(id)})).status()).toBe(201);
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-viewer-token'));await page.setViewportSize({width:580,height:850});await page.goto(`/#/g/${id}/agent?agent=reviewer&section=general`);
 await expect(page.getByRole('region',{name:'Agent configuration',exact:true}).getByLabel('Name',{exact:true})).toBeDisabled();
 await expect(page.getByRole('button',{name:'Review & save',exact:true})).toBeDisabled();
 await page.getByRole('navigation',{name:'Agent configuration sections'}).getByRole('button',{name:'Instructions',exact:true}).click();await expect(page.getByLabel('Agent instructions',{exact:true})).toHaveAttribute('readonly','');
 await page.goto(`/#/g/${id}/agent?agent=missing&section=general`);await expect(page.getByText('Select an agent from this graph. The requested agent may have been removed.')).toBeVisible();
});
