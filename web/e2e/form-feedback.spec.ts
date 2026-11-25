import {expect, test} from '@playwright/test';

test.beforeEach(async({page})=>{
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
});

test('native MCP fields reveal errors after submit and clear independently',async({page})=>{
 await page.goto('/#/connections/new');
 const id=page.getByLabel('Connection ID',{exact:true});
 const name=page.getByLabel('Connection name',{exact:true});
 await expect(id).not.toHaveAttribute('aria-invalid');
 await page.getByRole('button',{name:'Save MCP connection',exact:true}).click();
 await expect(id).toHaveAttribute('aria-invalid','true');
 await expect(name).toHaveAttribute('aria-invalid','true');
 const describedBy=await id.getAttribute('aria-describedby');
 expect(describedBy).toBeTruthy();
 await expect(page.locator(`[id="${describedBy}"]`)).toHaveText(/.+/);
 await id.fill('test-connection');
 await expect(id).not.toHaveAttribute('aria-invalid');
 await expect(name).toHaveAttribute('aria-invalid','true');
});

test('budgets point to invalid fields and retain correction without saving',async({page})=>{
 await page.goto('/#/models/budgets');
 const form=page.getByRole('form',{name:'Default budget per task'});
 const warning=form.getByLabel('Default warning level (%)',{exact:true});
 await expect(warning).toBeEnabled();
 const original=await warning.inputValue();
 await expect(warning).not.toHaveAttribute('aria-invalid');
 await warning.fill('101');
 await form.getByRole('button',{name:'Save task defaults'}).click();
 await expect(warning).toHaveAttribute('aria-invalid','true');
 await expect(warning).toBeFocused();
 await expect(form.getByRole('alert')).toContainText('between 1 and 100');
 await warning.fill(original);
 await expect(warning).not.toHaveAttribute('aria-invalid');
});

test('skill validation reveals the editor from preview and retains the draft',async({page})=>{
 await page.goto('/#/skills/new');
 const id=page.getByLabel('Skill ID',{exact:true});
 await id.fill('feedback-unsaved');
 await page.getByRole('button',{name:'Preview',exact:true}).click();
 await page.getByRole('button',{name:'Save',exact:true}).click();
 const title=page.getByLabel('Title',{exact:true});
 const body=page.getByLabel('Markdown body',{exact:true});
 await expect(title).toHaveAttribute('aria-invalid','true');
 await expect(body).toHaveAttribute('aria-invalid','true');
 await expect(title).toBeFocused();
 await title.fill('Feedback example');
 await expect(title).not.toHaveAttribute('aria-invalid');
 await expect(body).toHaveAttribute('aria-invalid','true');
 await expect(id).toHaveValue('feedback-unsaved');
});

test('saving invalid skill edits from another tab reveals the field without writing',async({page})=>{
 const headers={Authorization:'Bearer e2e-admin-token'};
 const created=await page.request.post('/api/v1/skills',{headers,data:{id:'feedback-existing',title:'Feedback existing',body:'Original instructions',enabled:true}});
 expect(created.ok()).toBe(true);
 await page.goto('/#/skills/s/feedback-existing/content');
 const title=page.getByLabel('Title',{exact:true});
 await title.fill('');
 await page.getByRole('link',{name:'Workflows',exact:true}).click();
 await expect(title).toHaveCount(0);
 await page.getByRole('button',{name:'Save',exact:true}).click();
 await expect(title).toHaveAttribute('aria-invalid','true');
 await expect(title).toBeFocused();
 await expect(page.getByRole('alert')).toHaveText('Enter a title.');
 const stored=await (await page.request.get('/api/v1/skills/feedback-existing',{headers})).json();
 expect(stored.title).toBe('Feedback existing');
 expect(stored.revision).toBe(1);
});
