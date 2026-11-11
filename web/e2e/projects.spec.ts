import {openTaskSettings} from './task-settings';
import {test,expect} from '@playwright/test';
import {mkdtempSync,writeFileSync,readFileSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {execFileSync} from 'node:child_process';
const headers={Authorization:'Bearer e2e-admin-token'};
test('registers a repository and reviews a separate task worktree without touching dirty source files',async({page})=>{
 const repo=mkdtempSync(join(tmpdir(),'weft-project-e2e-'));
 try{
  execFileSync('git',['init','-q',repo]);execFileSync('git',['-C',repo,'-c','user.name=Fixture','-c','user.email=fixture@example.test','commit','--allow-empty','-qm','initial']);
  writeFileSync(join(repo,'dirty.txt'),'keep');
  const graph={metadata:{id:'project-task-flow',name:'Project task flow'},spec:{workspace:{git:true},nodes:[{id:'worker',runtime:{kind:'echo'},loop:{mode:'once'},tools:[]}]}};
  expect((await page.request.post('/api/v1/graphs',{headers,data:graph})).status()).toBe(201);
  await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
  await page.goto('/#/projects');
  await page.getByRole('textbox',{name:'Project name',exact:true}).fill('Local fixture');
  await page.getByRole('textbox',{name:'Repository path',exact:true}).fill(repo);
  await expect(page.getByRole('button',{name:'Register project',exact:true})).toBeDisabled();
  await page.getByRole('checkbox').check();await page.getByRole('button',{name:'Register project',exact:true}).click();
  await expect(page.getByRole('heading',{name:'Local fixture',exact:true})).toBeVisible();
  await page.reload();await page.getByRole('link',{name:'Create task',exact:true}).click();
  await openTaskSettings(page);
 await page.getByRole('textbox',{name:'Title',exact:true}).fill('Review local fixture');await page.getByRole('textbox',{name:'Request',exact:true}).fill('Explain the fixture');
  await openTaskSettings(page);
 await page.getByRole('combobox',{name:'Project',exact:true}).selectOption({label:'Local fixture'});
  await page.getByRole('combobox',{name:'Workflow',exact:true}).selectOption('project-task-flow');
  await openTaskSettings(page);
 await page.getByRole('combobox',{name:'Entry agent',exact:true}).selectOption('worker');await openTaskSettings(page);
 await page.getByRole('combobox',{name:'Result agent',exact:true}).selectOption('worker');
  await page.getByRole('button',{name:'Save draft',exact:true}).click();await page.getByRole('button',{name:'Review requirements'}).click();
  await expect(page.getByRole('article',{name:'Task requirements'})).toContainText(repo);
  await page.getByRole('checkbox',{name:'I reviewed the workflow and authorize a separate execution copy.'}).check();await page.getByRole('button',{name:'Start task',exact:true}).click();
  await expect(page.getByRole('status')).toHaveText('Task status: succeeded');
  await expect(page.getByRole('article',{name:'Task progress'})).toContainText('Task workspace on the Weft server');
  expect(readFileSync(join(repo,'dirty.txt'),'utf8')).toBe('keep');
 }finally{rmSync(repo,{recursive:true,force:true});}
});
