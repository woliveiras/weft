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

test('creates a draft against a reviewed registered checkout', async ({page,request})=>{
  const headers={Authorization:'Bearer e2e-admin-token'};
  const dir=mkdtempSync(join(tmpdir(),'weft-chat-draft-repo-'));
  try {
    execFileSync('git',['init','-q',dir]);
    execFileSync('git',['-C',dir,'-c','user.name=Fixture','-c','user.email=fixture@example.test','commit','--allow-empty','-qm','initial']);
    const registration=await request.post('/api/v1/projects',{headers,data:{name:'Draft repository fixture',path:dir}});
    expect(registration.status()).toBe(201);
    const project=await registration.json();
    expect((await request.post('/api/v1/graphs',{headers,data:{metadata:{id:'draft-project-fixture'},spec:{nodes:[{id:'worker',runtime:{kind:'echo'},loop:{mode:'once'}}]}}})).status()).toBe(201);
    const settings=await (await request.get('/api/v1/chat/settings',{headers})).json();
    expect((await request.put('/api/v1/chat/settings',{headers:{...headers,'If-Match':String(settings.revision)},data:{provider:'e2e',name:'fixture'}})).ok()).toBe(true);
    await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-operator-token'));
    await page.goto('/');
    await page.getByRole('textbox',{name:'Message'}).fill('Draft a comparison of the registered chat project.');
    await page.getByRole('button',{name:'Send',exact:true}).click();
    const review=page.getByRole('region',{name:'Task draft Compare registered project'});
    await expect(review.getByRole('list',{name:'Reviewed repository pins'})).toContainText(project.head);
    await review.getByRole('button',{name:'Create task draft'}).click();
    const href=await review.getByRole('link',{name:'View task draft'}).getAttribute('href');
    const id=href?.match(/#\/tasks\/([^?]+)/)?.[1];
    const draft=await(await request.get('/api/v1/tasks/'+id,{headers})).json();
    expect(draft.status).toBe('draft');
    expect(draft.repositories).toMatchObject([{projectId:project.id,alias:'examples',baseCommit:project.head}]);
  } finally {
    rmSync(dir,{recursive:true,force:true});
  }
});

for (const delivery of ['local', 'pr'] as const) {
test(`reviews one checkout replacement with ${delivery} delivery and preserves the sibling and payload after reload`, async ({page,request})=>{
  const remote = delivery === 'pr';
  const taskTitle = remote ? 'PR checkout swap draft' : 'Checkout swap draft';
  const replacementName = remote ? 'PR Replacement docs' : 'Replacement docs';
  const headers={Authorization:'Bearer e2e-admin-token'};
  const temp=mkdtempSync(join(tmpdir(),'weft-chat-checkout-swap-'));
  try {
    const repos=[];
    for(const [alias,baseName] of [['docs','Original docs'],['app','Unchanged app'],['docs','Replacement docs']]){
      const name = remote ? 'PR ' + baseName : baseName;
      const path=join(temp,name);
      execFileSync('git',['init','-q',path]);
      execFileSync('git',['-C',path,'-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','--allow-empty','-qm',name]);
      const registration=await request.post('/api/v1/projects',{headers,data:{name,path}});
      expect(registration.status()).toBe(201);
      const p=await registration.json();
      const binding = {alias,projectId:p.id,baseCommit:p.head};
      if (remote) {
        const connection = 'swap-' + baseName.split(' ')[0].toLowerCase();
        const repository = 'fixture/' + connection;
        const gitUrl = 'https://git.invalid/' + repository + '.git';
        execFileSync('git',['-C',path,'remote','add','origin',gitUrl]);
        const connected = await request.post('/api/v1/scm/connections',{headers,data:{name:connection,kind:'github',repository,gitUrl,token:'synthetic-e2e'}});
        expect(connected.status()).toBe(201);
        const savedConnection = await connected.json();
        repos.push({...binding,scmConnection:connection,connectionRevision:savedConnection.updatedAt,repository,targetBranch:baseName === 'Replacement docs' ? 'release/docs' : 'main'});
      } else {
        repos.push(binding);
      }
    }
    const graphId='chat-checkout-swap-'+delivery;
    expect((await request.post('/api/v1/graphs',{headers,data:{metadata:{id:graphId},spec:{nodes:[{id:'reader',runtime:{kind:'echo'}}],channels:[{id:'input',to:['reader']}]}}})).status()).toBe(201);
    const payloadJson='{"preserve":["custom",3]}';
    const created=await request.post('/api/v1/tasks',{headers,data:{title:taskTitle,request:'Read the docs',outcome:'review',nodeId:'reader',graphVersion:1,graphId,channelId:'input',messageType:'review',payloadJson,repositories:repos.slice(0,2)}});
    expect(created.status()).toBe(201);
    const draft=await created.json();
    const settings=await (await request.get('/api/v1/chat/settings',{headers})).json();
    expect((await request.put('/api/v1/chat/settings',{headers:{...headers,'If-Match':String(settings.revision)},data:{provider:'e2e',name:'fixture'}})).ok()).toBe(true);
    await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
    await page.goto('/');
    const text = remote ? 'Replace the docs checkout on the PR checkout swap draft with ' + replacementName + ' connection=swap-replacement branch=release/docs' : 'Replace the docs checkout on the checkout swap draft with Replacement docs.';
    await page.getByRole('textbox',{name:'Message',exact:true}).fill(text);
    await page.getByRole('button',{name:'Send',exact:true}).click();
    const card=page.getByRole('region',{name:'Task draft correction'});
    await expect(card.getByRole('region',{name:'Current checkout'})).toContainText(repos[0].baseCommit);
    await expect(card.getByRole('region',{name:'Proposed checkout'})).toContainText(repos[2].baseCommit);
    if (remote) {
      await expect(card.getByRole('region',{name:'Current checkout'})).toContainText('fixture/swap-original · base main');
      await expect(card.getByRole('region',{name:'Proposed checkout'})).toContainText('fixture/swap-replacement · base release/docs');
    }
    const readDraft=async()=> (await request.get('/api/v1/tasks/'+draft.id,{headers})).json();
    expect((await readDraft()).revision).toBe(draft.revision);
    await card.getByRole('button',{name:'Save checkout replacement'}).click();
    await expect(card).toContainText('Task draft revision 2 saved.');
    await page.reload();
    await expect(card).toContainText('Task draft revision 2 saved.');
    const saved=await readDraft();
    expect(saved).toMatchObject({status:'draft',revision:2,payloadJson});
    expect(saved.repositories).toHaveLength(2);
    expect(saved.repositories[0]).toMatchObject(repos[2]);
    expect(saved.repositories[1]).toEqual(draft.repositories[1]);
  } finally {rmSync(temp,{recursive:true,force:true});}
});
}
