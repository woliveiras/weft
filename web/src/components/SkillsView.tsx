import { useState } from "react";
import { api, errorMessage } from "../api/client";
import type {
  Skill,
  SkillInput,
  InstructionSnapshot,
} from "../api/instructions";
import type { Graph } from "../api/types";
import { useAsync } from "../hooks/useData";
import { Btn, ContextHelp, ErrorBox, PageHeading } from "./common";

const blank: SkillInput = {
  id: "",
  title: "",
  description: "",
  body: "",
  enabled: true,
  global: false,
};
export function SkillsView({
  role,
  onChanged,
}: {
  role?: string;
  onChanged: () => void;
}) {
  const skills = useAsync(() => api.skills(), []);
  const graphs = useAsync(() => api.listGraphs(), []);
  const [selected, setSelected] = useState<Skill>();
  const [form, setForm] = useState<SkillInput>(blank);
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [graph, setGraph] = useState<Graph>();
  const [node, setNode] = useState("");
  const [snapshot, setSnapshot] = useState<InstructionSnapshot>();
  const [executions, setExecutions] = useState<
    { id: string; nodeId: string }[]
  >([]);
  const admin = role === "admin";
  const edit = admin;
  async function run(fn: () => Promise<void>) {
    setBusy(true);
    setError(undefined);
    try {
      await fn();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  function choose(skill?: Skill) {
    setSelected(skill);
    setForm(
      skill
        ? {
            id: skill.id,
            title: skill.title,
            description: skill.description,
            body: skill.body,
            enabled: skill.enabled,
            global: skill.global,
          }
        : blank,
    );
  }
  function updateGraph(next: Graph) {
    setGraph(next);
    setSnapshot(undefined);
  }
  const refs = graph?.spec.instructions ?? {};
  const available = skills.data ?? [];
  return (
    <div className="page skills-page">
      <PageHeading title="Skills & steering" description="Manage reusable guidance and its graph and agent associations."/>
      <p className="security-note">Guidance never grants tools, credentials or permissions.</p>
      <ContextHelp title="How instruction precedence works"><p>Later scopes take precedence: installation → graph → project → agent → task.</p></ContextHelp>
      <ErrorBox error={error ?? skills.error ?? graphs.error} />
      <section className="form" aria-label="Skill administration">
        <h2>Installation skills</h2>
        <Btn type="button" disabled={busy} onClick={() => choose()}>
          New skill
        </Btn>
        <label>
          Existing skill
          <select
            disabled={busy}
            aria-label="Existing skill"
            value={selected?.id ?? ""}
            onChange={(e) =>
              choose(available.find((s) => s.id === e.target.value))
            }
          >
            <option value="">New skill</option>
            {available.map((s) => (
              <option key={s.id} value={s.id}>
                {s.title} · {s.builtin ? "built-in" : `v${s.revision}`}
                {s.deleted ? " · deleted" : !s.enabled ? " · disabled" : ""}
              </option>
            ))}
          </select>
        </label>
        {selected?.builtin && (
          <p role="note">Built-in role skill shipped with Weft. It is read-only; add its ID ({selected.id}) to a graph or agent to use it.</p>
        )}
        <fieldset disabled={!admin || busy || !!selected?.builtin}>
          <label>
            Skill ID
            <input
              value={form.id}
              disabled={!!selected}
              onChange={(e) => setForm({ ...form, id: e.target.value })}
            />
          </label>
          <label>
            Title
            <input
              value={form.title}
              maxLength={200}
              onChange={(e) => setForm({ ...form, title: e.target.value })}
            />
          </label>
          <label>
            Description
            <input
              value={form.description}
              maxLength={1000}
              onChange={(e) =>
                setForm({ ...form, description: e.target.value })
              }
            />
          </label>
          <label>
            Markdown body
            <textarea
              rows={8}
              value={form.body}
              onChange={(e) => setForm({ ...form, body: e.target.value })}
            />
          </label>
          <label>
            <input
              type="checkbox"
              checked={form.enabled}
              onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
            />
            Enabled
          </label>
          <label>
            <input
              type="checkbox"
              checked={form.global}
              onChange={(e) => setForm({ ...form, global: e.target.checked })}
            />
            Installation default (graphs must opt in)
          </label>
          <Btn tone="primary"
            type="button"
            onClick={() =>
              void run(async () => {
                const s = await api.saveSkill(form, selected?.revision);
                choose(s);
                await skills.reload();
              })
            }
          >
            Save skill
          </Btn>
          {selected && (
            <Btn tone="danger"
              type="button"
              onClick={() =>
                void run(async () => {
                  choose(await api.deleteSkill(selected.id, selected.revision));
                  await skills.reload();
                })
              }
            >
              Delete skill (keep history)
            </Btn>
          )}
        </fieldset>
        <p>
          UTF-8 Markdown only; 16 KiB per body. No files or scripts are
          executed. Revisions are immutable; deletion keeps history.
        </p>
        {selected && (
          <details>
            <summary>Revision history</summary>
            {[...selected.history, selected].map((v) => (
              <article key={v.revision}>
                <h3>
                  v{v.revision} · {v.actor} · {v.at}
                </h3>
                <pre>{v.body}</pre>
              </article>
            ))}
          </details>
        )}
      </section>
      <section className="form" aria-label="Instruction associations">
        <h2>Graph and agent associations</h2>
        <label>
          Graph
          <select
            disabled={busy}
            aria-label="Graph"
            value={graph?.metadata.id ?? ""}
            onChange={(e) => {
              const g = graphs.data?.find(
                (s) => s.graph.metadata.id === e.target.value,
              )?.graph;
              setGraph(g);
              setNode(g?.spec.nodes[0]?.id ?? "");
              setSnapshot(undefined);
              setExecutions([]);
            }}
          >
            <option value="">Select graph</option>
            {graphs.data?.map((s) => (
              <option key={s.graph.metadata.id} value={s.graph.metadata.id}>
                {s.graph.metadata.name}
              </option>
            ))}
          </select>
        </label>
        {graph && (
          <>
            <fieldset disabled={!edit || busy}>
              <label>
                <input
                  type="checkbox"
                  checked={!!refs.installationDefaults}
                  onChange={(e) =>
                    updateGraph({
                      ...graph,
                      spec: {
                        ...graph.spec,
                        instructions: {
                          ...refs,
                          installationDefaults: e.target.checked,
                        },
                      },
                    })
                  }
                />
                Load installation defaults
              </label>
              <label>
                <input
                  type="checkbox"
                  checked={!!refs.projectSteering}
                  onChange={(e) =>
                    updateGraph({
                      ...graph,
                      spec: {
                        ...graph.spec,
                        instructions: {
                          ...refs,
                          projectSteering: e.target.checked,
                        },
                      },
                    })
                  }
                />
                Authorize project steering from this graph workspace
              </label>
              <p>
                Workspace: {graph.spec.workspace?.path || graph.metadata.id};
                only .weft/steering/*.md. No ancestor or home-directory
                search.
              </p>
              {available.map((s) => (
                <label key={s.id}>
                  <input
                    type="checkbox"
                    checked={refs.skillIds?.includes(s.id) ?? false}
                    onChange={(e) => {
                      const ids = e.target.checked
                        ? [...(refs.skillIds ?? []), s.id]
                        : (refs.skillIds ?? []).filter((id) => id !== s.id);
                      updateGraph({
                        ...graph,
                        spec: {
                          ...graph.spec,
                          instructions: { ...refs, skillIds: ids },
                          nodes: graph.spec.nodes.map((n) => ({
                            ...n,
                            skillIds:
                              n.skillIds?.filter((id) => ids.includes(id)) ??
                              n.skillIds,
                          })),
                        },
                      });
                    }}
                  />
                  {s.title} ({s.id})
                </label>
              ))}
              {graph.spec.nodes.map((n) => (
                <fieldset key={n.id}>
                  <legend>{n.name || n.id}</legend>
                  <label>
                    <input
                      type="checkbox"
                      checked={n.skillIds == null}
                      onChange={(e) =>
                        updateGraph({
                          ...graph,
                          spec: {
                            ...graph.spec,
                            nodes: graph.spec.nodes.map((v) =>
                              v.id === n.id
                                ? {
                                    ...v,
                                    skillIds: e.target.checked ? null : [],
                                  }
                                : v,
                            ),
                          },
                        })
                      }
                    />
                    Inherit graph skills
                  </label>
                  {n.skillIds != null &&
                    (refs.skillIds ?? []).map((id) => (
                      <label key={id}>
                        <input
                          type="checkbox"
                          checked={n.skillIds!.includes(id)}
                          onChange={(e) =>
                            updateGraph({
                              ...graph,
                              spec: {
                                ...graph.spec,
                                nodes: graph.spec.nodes.map((v) =>
                                  v.id === n.id
                                    ? {
                                        ...v,
                                        skillIds: e.target.checked
                                          ? [...(v.skillIds ?? []), id]
                                          : (v.skillIds ?? []).filter(
                                              (s) => s !== id,
                                            ),
                                      }
                                    : v,
                                ),
                              },
                            })
                          }
                        />
                        {id}
                      </label>
                    ))}
                </fieldset>
              ))}
              <Btn
                type="button"
                onClick={() =>
                  void run(async () => {
                    const saved = await api.updateGraph(
                      graph.metadata.id,
                      graph,
                      graph.metadata.version,
                    );
                    setGraph(saved.graph);
                    await graphs.reload();
                    onChanged();
                  })
                }
              >
                Save associations
              </Btn>
            </fieldset>
            <h3>Inspect saved configuration and executions</h3>
            <label>
              Agent
              <select
                disabled={busy}
                aria-label="Agent"
                value={node}
                onChange={(e) => {
                  setNode(e.target.value);
                  setSnapshot(undefined);
                }}
              >
                {graph.spec.nodes.map((n) => (
                  <option key={n.id}>{n.id}</option>
                ))}
              </select>
            </label>
            <Btn
              disabled={busy}
              type="button"
              onClick={() =>
                void run(async () =>
                  setSnapshot(await api.instructions(graph.metadata.id, node)),
                )
              }
            >
              Inspect saved instructions
            </Btn>
            <Btn
              disabled={busy}
              type="button"
              onClick={() =>
                void run(async () =>
                  setExecutions(await api.executions(graph.metadata.id)),
                )
              }
            >
              Load executions
            </Btn>
            {executions.map((x) => (
              <Btn
                disabled={busy}
                key={x.id}
                type="button"
                onClick={() =>
                  void run(async () =>
                    setSnapshot(await api.executionInstructions(x.id)),
                  )
                }
              >
                Inspect {x.nodeId} · {x.id}
              </Btn>
            ))}
          </>
        )}
        {snapshot && <InstructionManifest snapshot={snapshot} />}
      </section>
    </div>
  );
}
export function InstructionManifest({
  snapshot,
}: {
  snapshot: InstructionSnapshot;
}) {
  return (
    <section aria-label="Instruction manifest">
      <h3>
        {snapshot.executionId
          ? `Execution ${snapshot.executionId}`
          : "Current saved configuration (not an execution)"}
      </h3>
      <p>Remaining conservative token budget: {snapshot.budget}</p>
      {snapshot.entries.map((e, i) => (
        <article key={i}>
          <h4>
            {e.scope} · {e.source}
          </h4>
          <p>
            {e.included ? "Included" : `Omitted: ${e.reason}`}{" "}
            {e.revision ? `· v${e.revision}` : ""}
          </p>
          {e.sha256 && <code>{e.sha256}</code>}
          {e.body && <pre style={{ whiteSpace: "pre-wrap" }}>{e.body}</pre>}
        </article>
      ))}
    </section>
  );
}
