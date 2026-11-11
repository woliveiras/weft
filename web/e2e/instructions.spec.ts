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
