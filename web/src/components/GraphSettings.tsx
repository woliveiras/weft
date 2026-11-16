import { useEffect, useRef, useState } from 'react';
import type { EnvironmentView, Graph, Parameter, Trigger } from '../api/types';
import type { ElementIssue } from './canvas/edit';
import { Badge, Btn } from './common';
import { EnvironmentNetwork, EnvironmentSection } from './EnvironmentSection';
import { TriggersSection } from './TriggersSection';
import { ElementIssues, FieldIssues } from './GraphIssues';
import {
  settingsPath,
  settingsSectionForIssue,
  settingsSectionIds,
  settingsSections,
  type SettingsSection,
} from './graphSettingsSections';

/**
 * Graph settings share the toolbar's graph draft. Environment builds and
 * trigger changes act on the saved graph through their own controls.
 */
export function GraphSettings({
  section,
  graph,
  savedGraph,
  dirty,
  running,
  role,
  issues,
  onChange,
}: {
  section: SettingsSection;
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
  const [environment, setEnvironment] = useState<EnvironmentView>();
  const [triggers, setTriggers] = useState<Trigger[]>();
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => { heading.current?.focus(); }, [section]);
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

  const issuesBySection = Object.fromEntries(settingsSectionIds.map((id) => [
    id,
    (issues ?? []).filter((issue) => settingsSectionForIssue(issue.path) === id),
  ])) as Record<SettingsSection, ElementIssue[]>;
  const statuses: Partial<Record<SettingsSection, string>> = {
    workspace: workspace.repo ? 'repository' : workspace.git ? 'git' : 'local',
    environment: !graph.spec.environment || environment?.state === 'absent' ? 'not built'
      : environment?.state === 'ready' && !environment.upToDate ? 'outdated' : environment?.state,
    network: graph.spec.environment ? graph.spec.environment.network?.run ?? 'none' : undefined,
    triggers: triggers?.length && triggers.every((trigger) => trigger.state === 'paused') ? 'paused' : triggers?.length.toString(),
    parameters: String(parameters.length),
  };
  const contents = {
    general: (
      <fieldset aria-label="Graph configuration">
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
    ),
    workspace: (
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
    ),
    environment: (
      <EnvironmentSection
        graph={graph}
        savedGraph={savedGraph}
        dirty={dirty}
        running={running}
        canBuild={role === 'operator' || role === 'admin'}
        canReset={role === 'admin'}
        canAccept={role === 'admin'}
        issues={issuesBySection.environment}
        onBuildStatus={setEnvironment}
        onChange={onChange}
      />
    ),
    network: section === 'network' ? <EnvironmentNetwork graph={graph} onChange={onChange} /> : null,
    triggers: savedGraph.metadata.version
      ? <TriggersSection graph={savedGraph} role={role} onTriggers={setTriggers} />
      : <p className="muted">Save the graph before configuring triggers.</p>,
    parameters: (
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
    ),
    safety: <p>Set the message chain limit under <a href={settingsPath(graph.metadata.id, 'general')}>General</a>.
      Configure command isolation and tool approvals in <a href={`#/g/${encodeURIComponent(graph.metadata.id)}/agent`}>Agent configuration</a>.</p>,
  };

  return (
    <div className="graph-settings">
      <nav aria-label="Graph settings sections">
        {settingsSectionIds.map((id) => (
          <a key={id} href={settingsPath(graph.metadata.id, id)} aria-current={id === section ? 'page' : undefined}>
            <span>{statuses[id] === undefined ? settingsSections[id] : `${settingsSections[id]} · ${statuses[id]}`}</span>
            {issuesBySection[id].some((issue) => issue.severity === 'error') && <Badge tone="bad">Error</Badge>}
          </a>
        ))}
      </nav>
      <div className="graph-settings-content">
        {settingsSectionIds.map((id) => (
          <section key={id} hidden={id !== section} aria-label={settingsSections[id]}>
            <h2 ref={id === section ? heading : undefined} tabIndex={-1}>{settingsSections[id]}</h2>
            <ElementIssues issues={issuesBySection[id]} label={`${settingsSections[id]} validation`} />
            <p className="muted small">Changes join the graph draft; use Save graph in the toolbar to keep them.</p>
            <div className="form node-editor">{contents[id]}</div>
          </section>
        ))}
      </div>
    </div>
  );
}
