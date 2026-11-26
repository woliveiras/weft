import {expect,test} from '@playwright/test';
import {execFileSync} from 'node:child_process';
import {mkdtempSync, rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';

test('pins each local repository before admitting a multi-repository task',async ({page,request})=>{
  const headers={Authorization:'Bearer e2e-admin-token'};
  const temp=mkdtempSync(join(tmpdir(),'weft-chat-repos-'));
  try {
    const repositories=[];
    for(const alias of ['docs','examples']) {
      const path=join(temp,alias);
      execFileSync('git',['init','-q',path]);
      execFileSync('git',['-C',path,'-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','--allow-empty','-qm',alias]);
      const saved=await request.post('/api/v1/projects',{headers,data:{name:'Chat '+alias,path}});
      expect(saved.status()).toBe(201);
      const project=await saved.json();
      repositories.push({projectId:project.id,baseCommit:project.head,alias});
    }
    const g=await request.post('/api/v1/graphs',{headers,data:{metadata:{id:'chat-repository-start',name:'Repository start fixture'},spec:{nodes:[{id:'reader',runtime:{kind:'echo'},loop:{mode:'once'},isolation:{mode:'docker',image:'fixture:local'},tools:[]}]}}});
    expect(g.status()).toBe(201);
    const saved=await request.post('/api/v1/tasks',{headers,data:{title:'Repository checklist fixture',request:'Read the checklist.',outcome:'review',graphId:'chat-repository-start',graphVersion:1,nodeId:'reader',resultNodeId:'reader',repositories}});
    expect(saved.status()).toBe(201);
    const task=await saved.json();
    const readiness=await (await request.get('/api/v1/tasks/'+task.id+'/review',{headers})).json();
    expect(readiness.canStart,readiness.blockers.join('\n')).toBe(true);
    const settings=await (await request.get('/api/v1/chat/settings',{headers})).json();
    expect((await request.put('/api/v1/chat/settings',{headers:{...headers,'If-Match':String(settings.revision)},data:{provider:'e2e',name:'fixture'}})).ok()).toBe(true);
    await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
    await page.goto('/');
    await page.getByRole('textbox',{name:'Message',exact:true}).fill('Start the repository checklist task.');
    await page.getByRole('button',{name:'Send',exact:true}).click();
    const review=page.getByRole('region',{name:'Start task Repository checklist fixture'});
    for(const repo of repositories) {
      await expect(review).toContainText(repo.alias);
      await expect(review).toContainText(repo.baseCommit);
    }
    await review.getByRole('button',{name:'Start task',exact:true}).click();
    await expect(review).toContainText('Task admission recorded.');
    await expect.poll(async()=> (await (await request.get('/api/v1/tasks/'+task.id+'/progress',{headers})).json()).state).toBe('succeeded');
    const evidence=page.getByRole('region',{name:'Repository evidence'}).last();
    for(const repo of repositories){
      const row=evidence.getByRole('listitem').filter({hasText:repo.alias});
      await expect(row).toContainText(repo.baseCommit);
      await expect(row).toContainText('ready');
    }
  } finally {rmSync(temp,{recursive:true,force:true});}
});
