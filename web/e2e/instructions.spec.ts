import { expect, test } from "@playwright/test";
test("admin creates and associates guidance, runs an agent and inspects its immutable manifest", async ({
  page,
}) => {
  const headers = { Authorization: "Bearer e2e-admin-token" };
  await page.addInitScript(() =>
    localStorage.setItem("weftToken", "e2e-admin-token"),
  );
  const response = await page.request.post("/api/v1/graphs", {
    headers,
    data: {
      metadata: { id: "instruction-browser", name: "Instruction browser" },
      spec: {
        nodes: [
          {
            id: "writer",
            prompt: "Perform the original task",
            runtime: { kind: "echo" },
            loop: { mode: "once" },
          },
        ],
      },
    },
  });
  expect(response.ok()).toBeTruthy();
  await page.goto("/#/skills");
  await page.getByRole("link", {name:"New skill", exact:true}).click();
  await page.getByLabel("Skill ID", {exact:true}).fill("browser-skill");
  await page.getByLabel("Title", { exact: true }).fill("Browser skill");
  await page.getByLabel("Markdown body", {exact:true}).fill("Respect project conventions.");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByLabel("Skill ID", {exact:true})).toBeDisabled();
  await page.getByRole("link",{name:"Back to skills",exact:true}).click();
  await page.getByRole("link",{name:"Workflow associations",exact:true}).click();
  await page
    .getByLabel("Workflow", { exact: true })
    .selectOption("instruction-browser");
  await page.getByLabel("Browser skill (browser-skill)").check();
  await page
    .getByLabel("Authorize project steering from this graph workspace", {exact:true})
    .check();
  const saved = page.waitForResponse(
    (r) =>
      r.request().method() === "PUT" &&
      r.url().endsWith("/graphs/instruction-browser"),
  );
  await page.getByRole("button", { name: "Review associations" }).click();
  const review = page.getByRole('dialog', {name:'Review workflow associations'});
  await expect(review).toContainText('expected version 1');
  await expect(review).toContainText('Save creates a new workflow version');
  await review.getByRole('button', {name:'Cancel',exact:true}).click();
  await expect(page.getByLabel("Browser skill (browser-skill)")).toBeChecked();
  await page.getByRole("button", { name: "Review associations" }).click();
  await page.getByRole("button", { name: "Save associations" }).click();
  expect((await saved).ok()).toBeTruthy();
  await page.getByRole("link",{name:"Inspect instructions",exact:true}).click();
  await page
    .getByRole("button", { name: "Inspect saved instructions" })
    .click();
  await expect(page.getByLabel("Instruction manifest")).toContainText(
    "Respect project conventions.",
  );
  await expect(page.getByLabel("Instruction manifest")).toContainText(
    "Omitted: workspace_unavailable",
  );
  const started = await page.request.post(
    "/api/v1/graphs/instruction-browser/start",
    { headers },
  );
  expect(started.ok()).toBeTruthy();
  await expect
    .poll(async () => {
      const res = await page.request.get(
        "/api/v1/graphs/instruction-browser/executions",
        { headers },
      );
      return (await res.json()).some(
        (x: { status: string }) => x.status === "succeeded",
      );
    })
    .toBe(true);
  await page.getByRole("button", { name: "Load executions" }).click();
  await page.getByRole("button", { name: /Inspect writer ·/ }).click();
  await expect(page.getByLabel("Instruction manifest")).toContainText(
    "Execution exe_",
  );
  await expect(page.getByLabel("Instruction manifest")).toContainText(
    "Perform the original task",
  );
  await page.goto("/#/skills/s/browser-skill/content");
  await page.getByLabel("Markdown body", {exact:true}).fill("Changed after execution");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("status")).toHaveText("Skill saved as revision 2.");
  await page.goto("/#/skills/inspect?workflow=instruction-browser&agent=writer");
  await page.getByRole("button", {name:"Load executions"}).click();
  await page.getByRole("button", { name: /Inspect writer ·/ }).click();
  await expect(page.getByLabel("Instruction manifest")).toContainText(
    "Respect project conventions.",
  );
  await expect(page.getByLabel("Instruction manifest")).not.toContainText(
    "Changed after execution",
  );
});

test("Activity shows historical progressive guidance to a viewer", async ({ page }) => {
  const admin = { Authorization: "Bearer e2e-admin-token" };
  const viewer = { Authorization: "Bearer e2e-viewer-token" };
  const id = "repository-guidance-browser";
  const created = await page.request.post("/api/v1/skills", {
    headers: admin,
    data: {
      id: "browser-guidance", title: "Browser guidance", description: "Console review catalogue.",
      body: "Browser console body.", enabled: true,
    },
  });
  expect(created.ok()).toBeTruthy();
  const graph = await page.request.post("/api/v1/graphs", {
    headers: admin,
    data: {
      metadata: { id, name: "Repository guidance browser" },
      spec: {
        instructions: { skillIds: ["browser-guidance"] },
        nodes: [{
          id: "reviewer", prompt: "Run repository-guidance-browser", workspace: { subdir: "src" },
          model: { provider: "e2e", name: "fixture" },
          isolation: { mode: "process" }, loop: { mode: "once" }, tools: ["filesystem.read"],
        }],
      },
    },
  });
  expect(graph.ok()).toBeTruthy();
  await page.addInitScript(() => localStorage.setItem("weftToken", "e2e-admin-token"));
  await page.goto("/#/skills/associations");
  await page.getByLabel("Workflow", { exact: true }).selectOption(id);
  await page.getByLabel("Discover repository instructions and skills", {exact:true}).check();
  const saved = page.waitForResponse((r) => r.request().method() === "PUT" && r.url().endsWith(`/graphs/${id}`));
  await page.getByRole("button", { name: "Review associations" }).click();
  await page.getByRole("button", { name: "Save associations" }).click();
  expect((await saved).ok()).toBeTruthy();
  await page.getByRole("link", {name:"Inspect instructions",exact:true}).click();
  await page.getByRole("button", { name: "Inspect saved instructions" }).click();
  const preview = page.getByLabel("Instruction manifest");
  await expect(preview).toContainText("Inherited from graph");
  await expect(preview).toContainText("Browser review catalogue.");
  const started = await page.request.post(`/api/v1/graphs/${id}/start`, { headers: admin });
  expect(started.ok()).toBeTruthy();
  let executionId = "";
  await expect.poll(async () => {
    const response = await page.request.get(`/api/v1/graphs/${id}/executions`, { headers: viewer });
    expect(response.ok()).toBeTruthy();
    const execution = (await response.json())[0];
    executionId = execution?.id ?? "";
    return execution?.status;
  }).toBe("succeeded");

  const viewerPage = await page.context().newPage();
  await viewerPage.addInitScript(() => localStorage.setItem("weftToken", "e2e-viewer-token"));
  await viewerPage.goto("/#/activity");
  await viewerPage.getByRole("link", { name: executionId, exact: true }).click();
  await viewerPage.getByRole("link", { name: "Guidance", exact: true }).click();
  const manifest = viewerPage.getByLabel("Instruction manifest");
  await expect(manifest).toContainText("repository · AGENTS.md");
  await expect(manifest).toContainText("Omitted: shadowed_by_console");
  await expect(manifest).toContainText("graph · browser-guidance");
  await expect(manifest).toContainText("catalogue · confirmed · v1");
  const reads = manifest.getByLabel("On-demand reads");
  await expect(reads).toContainText("reference · confirmed");
  await expect(reads).toContainText(".agents/skills/browser-review/references/checklist.md");
  await expect(reads).toContainText(/SHA-256 [a-f0-9]{64}/);
  await expect(reads).toContainText("body · confirmed · v1");
  await expect(manifest).not.toContainText("Browser repository body.");
  await expect(manifest).not.toContainText("Browser console body.");
  await viewerPage.reload();
  await expect(viewerPage.getByLabel("On-demand reads")).toContainText("reference · confirmed");
});

test('catalogue filters and full-page editing preserve drafts, conflicts and read-only roles',async({page})=>{
  const headers={Authorization:'Bearer e2e-admin-token'};
  const created:Array<{title:string;at:string}>=[];
  await page.addInitScript(()=>{if(!localStorage.getItem('weftToken'))localStorage.setItem('weftToken','e2e-admin-token');});
  for(let i=0;i<21;i++) {
    const response=await page.request.post('/api/v1/skills',{headers,data:{id:`catalogue-${i}`,title:`Catalogue skill ${i}`,description:'Reusable review guidance',body:'# Review\n**Security** first.\n- Inspect inputs',enabled:i!==0}});
    expect(response.ok()).toBe(true);
    created.push(await response.json());
  }
  expect((await page.request.post('/api/v1/graphs',{headers,data:{metadata:{id:'skill-usage',name:'Skill usage'},spec:{instructions:{skillIds:['catalogue-20']},nodes:[{id:'reviewer',name:'Reviewer',runtime:{kind:'echo'}}]}}})).ok()).toBe(true);
  await page.goto('/#/skills');
  await page.getByLabel('Search skills').fill('Catalogue skill');
  await page.getByRole('combobox',{name:'Origin',exact:true}).selectOption('custom');
  const table=page.getByRole('table');await expect(table.getByRole('row')).toHaveCount(21);
  // API writes can share a millisecond. Any record at the newest timestamp is valid first.
  const newest=Math.max(...created.map(skill=>Date.parse(skill.at)));
  expect(created.filter(skill=>Date.parse(skill.at)===newest).map(skill=>skill.title)).toContain(await table.getByRole('row').nth(1).getByRole('link').innerText());
  await page.getByRole('button',{name:'Next',exact:true}).click();await expect(table.getByRole('row')).toHaveCount(2);
  await page.getByLabel('Skill status').selectOption('disabled');await expect(table.getByRole('row')).toHaveCount(2);
  await page.getByLabel('Skill status').selectOption('current');
  await table.getByRole('link',{name:'Catalogue skill 20',exact:true}).click();
  await expect(page).toHaveURL(/#\/skills\/s\/catalogue-20\/content$/);
  await expect(page.getByRole('dialog')).toHaveCount(0);
  const metadata=await page.locator('.skill-metadata-fields').boundingBox();
  const fullDescription=await page.getByLabel('Description',{exact:true}).boundingBox();
  expect(Math.abs(metadata!.width-fullDescription!.width)).toBeLessThan(2);
  await page.getByLabel('Title',{exact:true}).fill('Reviewed catalogue skill');
  await page.getByRole('button',{name:'Preview',exact:true}).click();
  await expect(page.getByLabel('Markdown preview').getByRole('heading',{name:'Review',exact:true})).toBeVisible();
  await page.getByRole('link',{name:'Workflows',exact:true}).click();
  await expect(page.getByRole('region',{name:'Workflows using this skill'})).toContainText('Skill usage');
  await page.getByRole('link',{name:'Content',exact:true}).click();
  await expect(page.getByLabel('Title',{exact:true})).toHaveValue('Reviewed catalogue skill');
  await expect(page.getByText('Unsaved changes',{exact:true})).toBeVisible();
  await page.route('**/api/v1/skills/catalogue-20',route=>route.fulfill({status:409,json:{error:'Revision changed',code:'conflict'}}));
  await page.getByRole('button',{name:'Save',exact:true}).click();
  await expect(page.getByRole('alert')).toHaveText('Revision changed');
  await expect(page.getByLabel('Title',{exact:true})).toHaveValue('Reviewed catalogue skill');
  await page.unroute('**/api/v1/skills/catalogue-20');
  await page.getByRole('button',{name:'Save',exact:true}).click();
  await expect(page.getByRole('status')).toHaveText('Skill saved as revision 2.');
  await page.getByRole('link',{name:'History',exact:true}).click();
  await expect(page.getByText(/Revision 2 · e2e-admin/)).toBeVisible();
  await page.getByRole('link',{name:'Workflows',exact:true}).click();
  await page.getByRole('link',{name:'Skill usage',exact:true}).click();
  await expect(page).toHaveURL(/#\/skills\/associations\?workflow=skill-usage$/);
  await expect(page.getByLabel('Workflow',{exact:true})).toHaveValue('skill-usage');
  await page.getByText('Reviewer · Inherited skills',{exact:true}).click();
  await expect(page.getByLabel('Inherit graph skills',{exact:true})).toBeChecked();
  await page.setViewportSize({width:580,height:900});
  await expect.poll(()=>page.locator('main').evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true);
  await page.goto('/#/skills/s/catalogue-20/content');
  const description=await page.getByLabel('Description',{exact:true}).boundingBox();const body=await page.getByLabel('Markdown body',{exact:true}).boundingBox();
  expect(Math.abs(description!.x-body!.x)).toBeLessThan(2);expect(Math.abs(description!.width-body!.width)).toBeLessThan(2);
  await page.evaluate(()=>localStorage.setItem('weftToken','e2e-viewer-token'));await page.reload();
  await expect(page.getByLabel('Markdown body',{exact:true})).toBeDisabled();await expect(page.getByRole('button',{name:'Save',exact:true})).toHaveCount(0);
  expect((await page.request.put('/api/v1/skills/catalogue-20',{headers:{Authorization:'Bearer e2e-viewer-token','If-Match':'2'},data:{id:'catalogue-20',title:'Forbidden',body:'Body',enabled:true}})).status()).toBe(403);
});


test('copies a built-in only after review and saving, retaining the original and associations',async({page})=>{
  const headers={Authorization:'Bearer e2e-admin-token'};
  await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
  const original=await (await page.request.get('/api/v1/skills/weft-planner',{headers})).json();
  await page.goto('/#/skills/s/weft-planner/content');
  await expect(page.getByText('Built-in · Read-only',{exact:true})).toBeVisible();
  await expect(page.getByLabel('Markdown body',{exact:true})).toBeDisabled();
  await page.getByRole('link',{name:'Create editable copy',exact:true}).click();
  await expect(page).toHaveURL(/#\/skills\/new\?copyFrom=weft-planner$/);
  await expect(page.getByLabel('Skill ID',{exact:true})).toHaveValue('planner-copy');
  await expect(page.getByLabel('Markdown body',{exact:true})).toHaveValue(original.body);
  expect((await page.request.get('/api/v1/skills/planner-copy',{headers})).status()).toBe(404);
  await page.getByLabel('Title',{exact:true}).fill('My editable planner');
  await page.getByLabel('Markdown body',{exact:true}).fill('Custom planning instructions.');
  await page.getByRole('button',{name:'Save',exact:true}).click();
  await expect(page.getByRole('status')).toHaveText('Skill saved as revision 1.');
  const copy=await (await page.request.get('/api/v1/skills/planner-copy',{headers})).json();
  expect(copy).toMatchObject({id:'planner-copy',title:'My editable planner',body:'Custom planning instructions.',revision:1,global:false});
  expect(copy.builtin).not.toBe(true);
  expect(await (await page.request.get('/api/v1/skills/weft-planner',{headers})).json()).toEqual(original);
  await page.getByRole('link',{name:'Workflows',exact:true}).click();
  await expect(page.getByText('No saved workflow uses this skill.',{exact:true})).toBeVisible();
  await page.goto('/#/skills/s/weft-planner/content');
  await page.getByRole('link',{name:'Create editable copy',exact:true}).click();
  await expect(page.getByLabel('Skill ID',{exact:true})).toHaveValue('planner-copy-2');
  page.once('dialog',dialog=>dialog.accept());
  await page.getByRole('link',{name:'Back to skills',exact:true}).click();
  expect((await page.request.get('/api/v1/skills/planner-copy-2',{headers})).status()).toBe(404);
});
