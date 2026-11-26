import {expect,test,type Page,type Locator} from '@playwright/test';
import {execFileSync} from 'node:child_process';
import {mkdtempSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {openTaskSettings} from './task-settings';

const headers={Authorization:'Bearer e2e-admin-token'};

async function selectAcrossSearch(page:Page,names:string[],editor:Locator|Page=page){
  const field=editor.getByRole('combobox',{name:/^(Suggested repositories|Repositories)$/});
  await expect(field).toHaveCount(1);
  await field.focus();
  await page.keyboard.press('ArrowDown');
  const list=editor.getByRole('listbox');
  await expect(list).toBeVisible();
  for(const name of names){
    await field.fill(name);
    const option=list.getByRole('option',{name:new RegExp(name)});
    const selected=await option.getAttribute('aria-selected');
    await page.keyboard.press('Enter');
    await expect(option).toHaveAttribute('aria-selected',selected==='true'?'false':'true');
  }
  await field.fill('absent-99999');
  await expect(editor.getByText('No matching repositories.')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(list).toHaveCount(0);
  await expect(field).toBeFocused();
}

test('saves versioned repository suggestions and edits independent task selections',async({page})=>{
  const sources:string[]=[];
  const ids:Record<string,string>={};
  const graphId='multirepo-'+Date.now().toString(36);
  const names=['infra','backend','frontend'].map(n=>graphId+'-'+n);
  try {
    for(const name of names){
      const path=mkdtempSync(join(tmpdir(),'weft-member-'));sources.push(path);
      execFileSync('git',['init','-q',path]);
      execFileSync('git',['-C',path,'-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','--allow-empty','-qm','initial']);
      const result=await page.request.post('/api/v1/projects',{headers,data:{name,path}});
      expect(result.status()).toBe(201);
      ids[name]=(await result.json()).id;
    }
    const response=await page.request.post('/api/v1/graphs',{headers,data:{metadata:{id:graphId,name:graphId},spec:{nodes:[{id:'worker',runtime:{kind:'echo'},loop:{mode:'once'}}]}}});
    expect(response.status()).toBe(201);
    await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
    const settings='/#/g/'+graphId+'/settings/task';
    await page.goto(settings);
    await selectAcrossSearch(page,names.slice(1),page.getByRole('group',{name:'Workflow task defaults'}));
    const newTask=page.getByRole('button',{name:'New task with these defaults'});
    await expect(newTask).toBeDisabled();
    await page.getByRole('button',{name:'Save graph',exact:true}).click();
    await expect(newTask).toBeEnabled();
    await newTask.click();
    const task=page.getByRole('form',{name:'Task request'});
    await openTaskSettings(page);
    await expect(task.getByRole('combobox',{name:'Repositories'})).toHaveValue(names.slice(1).join(', '));
    await selectAcrossSearch(page,[names[0]],task);
    await expect(task.getByRole('combobox',{name:'Repositories'})).toHaveValue([...names.slice(1),names[0]].join(', '));
    await page.getByRole('textbox',{name:'Request',exact:true}).fill('Update the health endpoint');
    await page.getByRole('button',{name:'Save draft',exact:true}).click();
    await expect(page).toHaveURL(/#\/tasks\/tsk_/);
    const first=page.url();
    const firstId=first.split('/').pop();
    const firstDraft=await (await page.request.get('/api/v1/tasks/'+firstId,{headers})).json();
    expect(firstDraft.repositories.map((r:{projectId:string})=>r.projectId)).toEqual([ids[names[1]],ids[names[2]],ids[names[0]]]);
    await page.goto(settings);await newTask.click();await openTaskSettings(page);
    await expect(task.getByRole('combobox',{name:'Repositories'})).toHaveValue(names.slice(1).join(', '));
    await selectAcrossSearch(page,names.slice(1),task);
    await page.getByRole('textbox',{name:'Request',exact:true}).fill('Investigate without a checkout');
    await page.getByRole('button',{name:'Save draft',exact:true}).click();
    await expect(page).toHaveURL(/#\/tasks\/tsk_/);
    let releaseCatalog!: () => void;
    const catalogGate = new Promise<void>(resolve => { releaseCatalog = resolve; });
    await page.route('**/api/v1/projects', async route => {
      await catalogGate;
      await route.continue();
    });
    await page.reload();
    await openTaskSettings(page);
    await expect(task.getByRole('combobox',{name:'Repositories'})).toHaveValue('');
    const catalogLoaded = page.waitForResponse(response => response.url().endsWith('/api/v1/projects'));
    releaseCatalog();
    await catalogLoaded;
    await expect(task.getByRole('combobox',{name:'Repositories'})).toHaveValue('');
    await page.unroute('**/api/v1/projects');
    await page.goto(settings);
    const defaults = page.getByRole('group',{name:'Workflow task defaults'});
    await selectAcrossSearch(page,[names[2]],defaults);
    await page.getByRole('button',{name:'Save graph',exact:true}).click();
    await newTask.click();
    await openTaskSettings(page);
    await expect(task.getByRole('combobox',{name:'Repositories'})).toHaveValue(names[1]);
    await expect(task.getByRole('region',{name:names[1],exact:true})).toBeVisible();
    await page.goto(first);await openTaskSettings(page);
    await expect(task.getByRole('combobox',{name:'Repositories'})).toHaveValue([...names.slice(1),names[0]].join(', '));

    await page.goto('/#/tasks/new?workflow='+graphId+'&project='+ids[names[0]]);
    await openTaskSettings(page);
    await expect(task.getByRole('combobox',{name:'Repositories',exact:true})).toHaveValue(names[0]);
    await page.goto(settings);

    const missingGraph = graphId+'-missing';
    const missingId = 'prj_unavailable';
    const graph = await page.request.post('/api/v1/graphs',{headers,data:{
      metadata:{id:missingGraph,name:missingGraph},
      spec:{task:{projectIds:[ids[names[1]],missingId]},nodes:[{
        id:'worker',runtime:{kind:'echo'},isolation:{mode:'docker',image:'fixture:local'},loop:{mode:'once'}
      }]}
    }});
    expect(graph.status()).toBe(201);
    await page.goto('/#/tasks/new?workflow='+missingGraph);
    await page.reload();
    await openTaskSettings(page);
    await expect(task.getByText('Repository unavailable. Replace or remove it.')).toBeVisible();
    await task.getByRole('combobox',{name:'Repositories'}).click();
    await task.getByRole('option',{name:new RegExp(missingId)}).click();
    await page.keyboard.press('Tab');
    await expect(task.getByRole('region',{name:names[1],exact:true})).toBeVisible();
    await expect(task.getByText('Repository unavailable. Replace or remove it.')).toHaveCount(0);
    await task.getByRole('textbox',{name:'Request',exact:true}).fill('Review the remaining repository');
    await task.getByRole('button',{name:'Review task',exact:true}).click();
    await expect(page).toHaveURL(/#\/tasks\/tsk_/);
    const correctedId = page.url().split('/').pop();
    const corrected = await (await page.request.get('/api/v1/tasks/'+correctedId+'/review',{headers})).json();
    expect(corrected.task.repositories.map((r:{projectId:string})=>r.projectId)).toEqual([ids[names[1]]]);
    expect(corrected.canStart).toBe(true);
  } finally {
    for(const dir of sources)rmSync(dir,{recursive:true,force:true});
  }
});
