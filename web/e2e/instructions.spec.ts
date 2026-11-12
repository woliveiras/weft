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
  await page.getByLabel("Skill ID").fill("browser-skill");
  await page.getByLabel("Title", { exact: true }).fill("Browser skill");
  await page.getByLabel("Markdown body").fill("Respect project conventions.");
  await page.getByRole("button", { name: "Save skill", exact: true }).click();
  await expect(page.getByLabel("Skill ID")).toBeDisabled();
  await page
    .getByLabel("Graph", { exact: true })
    .selectOption("instruction-browser");
  await page.getByLabel("Browser skill (browser-skill)").check();
  await page
    .getByLabel("Authorize project steering from this graph workspace")
    .check();
  const saved = page.waitForResponse(
    (r) =>
      r.request().method() === "PUT" &&
      r.url().endsWith("/graphs/instruction-browser"),
  );
  await page.getByRole("button", { name: "Save associations" }).click();
  expect((await saved).ok()).toBeTruthy();
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
  await page.getByLabel("Markdown body").fill("Changed after execution");
  await page.getByRole("button", { name: "Save skill", exact: true }).click();
  await expect(
    page.getByRole("option", { name: /Browser skill · v2/ }),
  ).toBeAttached();
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
  await page.goto("/#/skills");
  await page.getByLabel("Graph", { exact: true }).selectOption(id);
  await page.getByLabel("Discover repository instructions and skills").check();
  const saved = page.waitForResponse((r) => r.request().method() === "PUT" && r.url().endsWith(`/graphs/${id}`));
  await page.getByRole("button", { name: "Save associations" }).click();
  expect((await saved).ok()).toBeTruthy();
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
