import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { fakeApi, apiError } from "../test/fakeApi";
import { SkillsView } from "./SkillsView";
import { InstructionManifest } from "./InstructionManifest";
import type { Skill } from "../api/instructions";
const skill: Skill = {
  id: "review",
  title: "Review",
  description: "",
  body: "Review code",
  enabled: true,
  global: false,
  revision: 1,
  actor: "admin",
  at: "2026-10-16",
  deleted: false,
  history: [],
};
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
it("renders omissions and text without executing Markdown/HTML", () => {
  render(
    <InstructionManifest
      snapshot={{
        graphId: "g",
        nodeId: "n",
        budget: 10,
        entries: [
          {
            scope: "project",
            source: "bad.md",
            included: false,
            reason: "unsafe_path",
          },
          {
            scope: "graph",
            source: "review",
            body: "<script>bad()</script>",
            included: true,
            sha256: "abc",
          },
        ],
      }}
    />,
  );
  expect(screen.getByText("Omitted: unsafe_path")).toBeTruthy();
  expect(screen.getByText("<script>bad()</script>")).toBeTruthy();
  expect(document.querySelector("script")).toBeNull();
});
it("creates, versions and deletes through the API client with CAS and retains conflict edits", async () => {
  const routes = fakeApi({
    "GET /api/v1/skills": () => [skill],
    "GET /api/v1/graphs": () => [],
    "POST /api/v1/skills": () => skill,
    "PUT /api/v1/skills/review": () =>
      apiError(409, "Revision changed", "conflict"),
    "DELETE /api/v1/skills/review": () => ({
      ...skill,
      revision: 2,
      deleted: true,
      enabled: false,
      history: [skill],
    }),
  });
  render(<SkillsView role="admin" onChanged={() => {}} />);
  await screen.findByRole("option", { name: /Review · v1/ });
  fireEvent.change(screen.getByLabelText("Skill ID"), {
    target: { value: "review" },
  });
  fireEvent.change(screen.getByLabelText("Title"), {
    target: { value: "Review" },
  });
  fireEvent.change(screen.getByLabelText("Markdown body"), {
    target: { value: "Review code" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save skill" }));
  await waitFor(() =>
    expect(routes.find("POST", "/api/v1/skills")).toHaveLength(1),
  );
  await waitFor(() =>
    expect(
      (screen.getByLabelText("Skill ID") as HTMLInputElement).disabled,
    ).toBe(true),
  );
  fireEvent.change(screen.getByLabelText("Markdown body"), {
    target: { value: "Unsaved edit" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save skill" }));
  await screen.findByText("Revision changed");
  expect(
    (screen.getByLabelText("Markdown body") as HTMLTextAreaElement).value,
  ).toBe("Unsaved edit");
  expect(
    routes.find("PUT", "/api/v1/skills/review")[0].headers["If-Match"],
  ).toBe("1");
  fireEvent.click(
    screen.getByRole("button", { name: "Delete skill (keep history)" }),
  );
  await screen.findByText("v2 · admin · 2026-10-16");
});
it("associates graph and narrowed agents and inspects saved/execution manifests", async () => {
  const graph = {
    apiVersion: "weft.io/v1",
    kind: "Graph",
    metadata: { id: "g", name: "Graph", version: 3 },
    spec: { nodes: [{ id: "n", prompt: "task" }] },
  };
  const snapshot = {
    graphId: "g",
    nodeId: "n",
    budget: 8,
    entries: [
      { scope: "graph", source: "review", included: true, body: "Review code" },
    ],
  };
  const routes = fakeApi({
    "GET /api/v1/skills": () => [skill],
    "GET /api/v1/graphs": () => [{ graph }],
    "PUT /api/v1/graphs/g": ({ body }) => ({ graph: body }),
    "GET /api/v1/graphs/g/instructions": () => snapshot,
    "GET /api/v1/graphs/g/executions": () => [{ id: "exec", nodeId: "n" }],
    "GET /api/v1/executions/exec/instructions": () => ({
      ...snapshot,
      executionId: "exec",
    }),
  });
  render(<SkillsView role="admin" onChanged={() => {}} />);
  await screen.findByRole("option", { name: "Graph" });
  fireEvent.change(screen.getByLabelText("Graph"), { target: { value: "g" } });
  fireEvent.click(screen.getByLabelText("Review (review)"));
  fireEvent.click(screen.getByLabelText("Inherit graph skills"));
  fireEvent.click(screen.getByRole("button", { name: "Save associations" }));
  await waitFor(() =>
    expect(routes.find("PUT", "/api/v1/graphs/g")).toHaveLength(1),
  );
  const req = routes.find("PUT", "/api/v1/graphs/g")[0];
  expect(req.headers["If-Match"]).toBe("3");
  expect(
    (req.body as typeof graph & { spec: { nodes: { skillIds: string[] }[] } })
      .spec.nodes[0].skillIds,
  ).toEqual([]);
  await waitFor(() =>
    expect(
      (
        screen.getByRole("button", {
          name: "Inspect saved instructions",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(false),
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Inspect saved instructions" }),
  );
  await screen.findByText("Current saved configuration (not an execution)");
  fireEvent.click(screen.getByRole("button", { name: "Load executions" }));
  fireEvent.click(
    await screen.findByRole("button", { name: "Inspect n · exec" }),
  );
  await screen.findByText("Execution exec");
});
it("keeps writes disabled for viewers", async () => {
  fakeApi({ "GET /api/v1/skills": () => [], "GET /api/v1/graphs": () => [] });
  render(<SkillsView role="viewer" onChanged={() => {}} />);
  expect(
    screen.getByRole("button", { name: "Save skill" }).closest("fieldset")
      ?.disabled,
  ).toBe(true);
});
it("blocks selection changes during requests and keeps operator associations read-only", async () => {
  let resolve!: (value: unknown) => void;
  const graph = {
    metadata: { id: "g", name: "Graph", version: 1 },
    spec: { nodes: [{ id: "n" }] },
  };
  fakeApi({
    "GET /api/v1/skills": () => [skill],
    "GET /api/v1/graphs": () => [{ graph }],
    "GET /api/v1/graphs/g/instructions": () =>
      new Promise((r) => {
        resolve = r;
      }),
  });
  render(<SkillsView role="operator" onChanged={() => {}} />);
  await screen.findByRole("option", { name: "Graph" });
  fireEvent.change(screen.getByLabelText("Graph"), { target: { value: "g" } });
  expect(
    screen
      .getByRole("button", { name: "Save associations" })
      .closest("fieldset")?.disabled,
  ).toBe(true);
  fireEvent.click(
    screen.getByRole("button", { name: "Inspect saved instructions" }),
  );
  await waitFor(() =>
    expect((screen.getByLabelText("Graph") as HTMLSelectElement).disabled).toBe(
      true,
    ),
  );
  expect((screen.getByLabelText("Agent") as HTMLSelectElement).disabled).toBe(
    true,
  );
  expect(
    (screen.getByLabelText("Existing skill") as HTMLSelectElement).disabled,
  ).toBe(true);
  expect(
    (screen.getByRole("button", { name: "New skill" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  resolve({ graphId: "g", nodeId: "n", budget: 100, entries: [] });
  await screen.findByText("Current saved configuration (not an execution)");
  await waitFor(() =>
    expect((screen.getByLabelText("Graph") as HTMLSelectElement).disabled).toBe(
      false,
    ),
  );
});
it("shows built-in role skills as read-only even for administrators", async () => {
  const builtin: Skill = { ...skill, id: "weft-planner", title: "Planner role", actor: "weft", builtin: true };
  const routes = fakeApi({ "GET /api/v1/skills": () => [builtin, skill], "GET /api/v1/graphs": () => [] });
  render(<SkillsView role="admin" onChanged={() => {}} />);
  const select = await screen.findByLabelText("Existing skill");
  await screen.findByRole("option", { name: /Planner role · built-in/ });
  fireEvent.change(select, { target: { value: "weft-planner" } });
  expect(screen.getByRole("note").textContent).toContain("read-only");
  expect(screen.getByRole("button", { name: "Save skill" }).closest("fieldset")?.disabled).toBe(true);
  fireEvent.change(select, { target: { value: "review" } });
  expect(screen.getByRole("button", { name: "Save skill" }).closest("fieldset")?.disabled).toBe(false);
  expect(routes.calls.every((c) => c.method === "GET")).toBe(true);
});
