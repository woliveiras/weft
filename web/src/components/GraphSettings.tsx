import type { Graph, Parameter } from '../api/types';
import type { ElementIssue } from './canvas/edit';
import { Btn } from './common';
import { EnvironmentSection } from './EnvironmentSection';
import { TriggersSection } from './TriggersSection';
import { FieldIssues } from './GraphIssues';

/**
 * Graph-wide settings edited in the draft: identity, the workspace
 * repository, prompt parameters and the hop limit. Like the agent and
 * channel panels, changes are saved with the toolbar's Save graph.
 */
export function GraphSettings({
  graph,
  savedGraph,
  dirty,
  running,
  role,
  issues,
  onChange,
}: {
  graph: Graph;
  /** Saved version; the environment is always built from it. */
  savedGraph: Graph;
  dirty: boolean;
  running: boolean;
  role?: 'viewer' | 'operator' | 'admin';
  /** Validation findings that are not tied to an agent or channel (full paths). */
  issues?: ElementIssue[];
  onChange: (update: (graph: Graph) => Graph) => void;
}) {
  const workspace = graph.spec.workspace ?? {};
  const parameters = graph.spec.parameters ?? [];
  const setMetadata = (patch: Partial<Graph['metadata']>) =>
    onChange((g) => ({ ...g, metadata: { ...g.metadata, ...patch } }));
  const setWorkspace = (patch: NonNullable<Graph['spec']['workspace']>) =>
    onChange((g) => {
      const next = { ...g.spec.workspace, ...patch };
      const empty = Object.values(next).every((value) => value === undefined || value === false || value === '');
      return { ...g, spec: { ...g.spec, workspace: empty ? undefined : next } };
    });
  const setParameters = (next: Parameter[]) =>
    onChange((g) => ({ ...g, spec: { ...g.spec, parameters: next.length ? next : undefined } }));
  const updateParameter = (index: number, patch: Partial<Parameter>) =>
    setParameters(parameters.map((p, i) => (i === index ? { ...p, ...patch } : p)));

  return (
    <div className="panel-body">
      <div className="panel-title">
        <h2>Graph settings</h2>
      </div>
      <p className="muted small">Changes join the graph draft; use Save graph in the toolbar to keep them.</p>

      <section className="form node-editor" aria-label="Graph configuration">
        <fieldset>
          <legend>Graph</legend>
          <label>
            ID
            <input value={graph.metadata.id} readOnly aria-describedby="graph-id-hint" />
          </label>
          <span id="graph-id-hint" className="muted tiny">
            Fixed once created: URLs, workspaces and history refer to it.
          </span>
          <label>
            Name
            <input value={graph.metadata.name ?? ''} onChange={(event) => setMetadata({ name: event.target.value })} />
            <FieldIssues issues={issues} field="metadata.name" />
          </label>
          <label>
            Description
            <textarea
              rows={2}
              value={graph.metadata.description ?? ''}
              onChange={(event) => setMetadata({ description: event.target.value || undefined })}
            />
          </label>
        </fieldset>

        <fieldset>
          <legend>Workspace</legend>
          <label>
            Repository URL
            <input
              value={workspace.repo ?? ''}
              placeholder="https://github.com/owner/project.git"
              onChange={(event) => setWorkspace({ repo: event.target.value.trim() || undefined })}
              aria-describedby="graph-repo-hint"
            />
          </label>
          <span id="graph-repo-hint" className="muted tiny">
            Cloned into the graph workspace the first time the graph starts, while the workspace is empty. Changing it
            later does not replace an existing workspace. Use https://, ssh:// or user@host:path; public repositories only
            for now.
          </span>
          <FieldIssues issues={issues} field="spec.workspace.repo" />
          <label className="check">
            <input
              type="checkbox"
              checked={!!workspace.git || !!workspace.repo}
              disabled={!!workspace.repo}
              onChange={(event) => setWorkspace({ git: event.target.checked || undefined })}
            />
            Track the workspace with git {workspace.repo ? '(always on for a repository)' : ''}
          </label>
          <FieldIssues issues={issues} field="spec.workspace" />
        </fieldset>

        <EnvironmentSection
          graph={graph}
          savedGraph={savedGraph}
          dirty={dirty}
          running={running}
          canBuild={role === 'operator' || role === 'admin'}
          canReset={role === 'admin'}
          canAccept={role === 'admin'}
          issues={issues}
          onChange={onChange}
        />

        {savedGraph.metadata.version ? <TriggersSection graph={savedGraph} role={role} /> : null}

        <fieldset>
          <legend>Parameters</legend>
          <p className="muted tiny">
            Values substituted into prompts as <code>@name</code>, for example <code>@topic</code>.
          </p>
          {parameters.map((parameter, index) => (
            <div key={index} className="parameter-row" role="group" aria-label={`Parameter ${index + 1}`}>
              <label>
                Name
                <input value={parameter.name} onChange={(event) => updateParameter(index, { name: event.target.value })} />
              </label>
              <label>
                Value
                <input value={parameter.value} onChange={(event) => updateParameter(index, { value: event.target.value })} />
              </label>
              <Btn small tone="ghost" onClick={() => setParameters(parameters.filter((_, i) => i !== index))}>
                Remove
              </Btn>
              <FieldIssues issues={issues} field={`spec.parameters[${index}]`} />
            </div>
          ))}
          <Btn small onClick={() => setParameters([...parameters, { name: '', value: '' }])}>
            Add parameter
          </Btn>
        </fieldset>

        <fieldset>
          <legend>Safety</legend>
          <label>
            Max hops
            <input
              type="number"
              min={1}
              value={graph.spec.maxHops ?? ''}
              placeholder="64"
              aria-describedby="graph-hops-hint"
              onChange={(event) =>
                onChange((g) => ({
                  ...g,
                  spec: { ...g.spec, maxHops: event.target.value ? Number(event.target.value) : undefined },
                }))
              }
            />
          </label>
          <span id="graph-hops-hint" className="muted tiny">
            How many agent-to-agent hops one message chain may take; stops runaway loops.
          </span>
          <FieldIssues issues={issues} field="spec.maxHops" />
        </fieldset>
      </section>
    </div>
  );
}
