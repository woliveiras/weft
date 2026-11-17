import { EnvironmentProxies } from './EnvironmentProxies';
import { useEffect, useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { EnvironmentSpec, Graph } from '../api/types';
import { useAsync, useThrottled } from '../hooks/useData';
import { useEventStream } from '../hooks/useEventStream';
import type { ElementIssue } from './canvas/edit';
import { Badge, Btn, ErrorBox, fmtDateTime } from './common';
import { FieldIssues } from './GraphIssues';
import { environmentPhase } from './environmentPresentation';
import { EnvironmentProgress } from './EnvironmentProgress';
import { EnvironmentDiscovery } from './EnvironmentDiscovery';

type Source = 'none' | 'image' | 'dockerfile';

const sourceOf = (env?: EnvironmentSpec): Source => (!env ? 'none' : env.dockerfile !== undefined ? 'dockerfile' : 'image');

/** Agents that run their commands in the graph environment (docker isolation, no image of their own). */
export function environmentUsers(graph: Graph): string[] {
  const defaults = graph.spec.defaults?.isolation ?? {};
  return graph.spec.nodes
    .filter((node) => {
      const isolation = { ...defaults, ...node.isolation };
      return !node.disabled && isolation.mode === 'docker' && !isolation.image;
    })
    .map((node) => node.name || node.id);
}

/**
 * The graph's project environment: its definition (part of the graph draft,
 * saved with Save graph) and the state of its build on the sandbox daemon
 * (built from the saved version).
 */
export function EnvironmentSection({
  graph,
  savedGraph,
  dirty,
  running,
  canBuild,
  canReset,
  canAccept = false,
  issues,
  onChange,
}: {
  /** The draft. */
  graph: Graph;
  /** The saved version: builds always use it. */
  savedGraph: Graph;
  dirty: boolean;
  running: boolean;
  canBuild: boolean;
  canReset: boolean;
  canAccept?: boolean;
  issues?: ElementIssue[];
  onChange: (update: (graph: Graph) => Graph) => void;
}) {
  const env = graph.spec.environment;
  const source = sourceOf(env);
  const setEnv = (next: EnvironmentSpec | undefined) =>
    onChange((g) => ({ ...g, spec: { ...g.spec, environment: next } }));
  const patch = (p: Partial<EnvironmentSpec>) => setEnv({ ...env, ...p });

  const changeSource = (next: Source) => {
    if (next === source) return;
    if (next === 'none') {
      if (env && !confirm('Remove the environment definition from this graph? Agents with docker isolation will use the default sandbox image.')) return;
      setEnv(undefined);
    } else if (next === 'image') {
      setEnv({ ...env, origin: undefined, image: env?.image ?? '', dockerfile: undefined });
    } else {
      setEnv({ ...env, origin: undefined, image: undefined, dockerfile: env?.dockerfile ?? 'FROM debian:bookworm-slim\nRUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates && rm -rf /var/lib/apt/lists/*\n' });
    }
  };

  return (
    <fieldset aria-label="Environment">
      <legend>Environment</legend>
      <EnvironmentProxies key={savedGraph.metadata.id} graphId={savedGraph.metadata.id} />
      <p className="muted tiny">
        The container image agents run their commands in when their Command sandbox uses docker isolation without an
        image of their own. Weft builds it on its own sandbox daemon, never on your machine.
      </p>
      {canAccept && (
        <EnvironmentDiscovery
          key={savedGraph.metadata.id + ':' + savedGraph.metadata.version}
          graphId={savedGraph.metadata.id}
          disabled={!!env || !!savedGraph.spec.environment || JSON.stringify(graph.spec.workspace ?? {}) !== JSON.stringify(savedGraph.spec.workspace ?? {})}
          onAccept={setEnv}
        />
      )}
      {env?.origin && (
        <p className="muted small">
          Accepted from {env.origin.path}{env.origin.service ? ` · ${env.origin.service}` : ''}.
          The repository files and settings will be checked again before building.
          To customize the definition manually, remove it and choose a new source.
        </p>
      )}
      <label>
        Source
        <select value={source} onChange={(event) => changeSource(event.target.value as Source)}>
          <option value="none">none — default sandbox image</option>
          <option value="image">base image — pulled from a registry</option>
          <option value="dockerfile">Dockerfile — written here</option>
        </select>
      </label>
      <FieldIssues issues={issues} field="spec.environment" />
      {source === 'image' && (
        <label>
          Base image
          <input value={env?.image ?? ''} placeholder="golang:1.26" onChange={(event) => patch({ image: event.target.value })} />
        </label>
      )}
      {source === 'dockerfile' && (
        <label>
          Dockerfile
          <textarea
            className="mono"
            rows={6}
            value={env?.dockerfile ?? ''}
            spellCheck={false}
            onChange={(event) => patch({ dockerfile: event.target.value })}
          />
        </label>
      )}
      {source !== 'none' && (
        <>
          <span className="muted tiny">
            {source === 'dockerfile'
              ? 'Runs as root while building, so install system packages here. Manual Dockerfiles have no build context. Accept a discovered repository definition to use COPY or ADD.'
              : 'Use an image with the toolchain you need. Installing system packages needs a Dockerfile; setup commands run without root.'}
          </span>
          <LinesField
            label="Setup commands (one per line)"
            values={env?.setup}
            placeholder={'go mod download\npnpm -C web install --frozen-lockfile'}
            onChange={(setup) => patch({ setup })}
          />
          <span className="muted tiny">
            Run once per build, in order, as the sandbox user in /workspace with the graph workspace mounted. What they
            install outside the workspace becomes part of the image.
          </span>
          <FieldIssues issues={issues} field="spec.environment.setup" />
          <label>
            Network during build and setup
            <select
              value={env?.network?.setup ?? 'bridge'}
              onChange={(event) => patch({ network: { ...env?.network, setup: event.target.value as 'none' | 'bridge' | 'egress', setupAllowedHosts: event.target.value === 'egress' ? env?.network?.setupAllowedHosts : undefined } })}
            >
              <option value="bridge">bridge — internet, to download tools and dependencies</option>
              <option value="egress">egress — approved HTTP/HTTPS hosts through a proxy</option>
              <option value="none">none</option>
            </select>
          </label>
          {env?.network?.setup === 'egress' && <LinesField label="Build and setup hosts (one exact DNS name per line)" values={env.network.setupAllowedHosts} placeholder="registry.npmjs.org" onChange={(setupAllowedHosts) => patch({ network: { ...env.network, setupAllowedHosts } })} />}
          <label>
            Network for agents
            <select
              value={env?.network?.run ?? 'none'}
              onChange={(event) => patch({ network: { ...env?.network, run: event.target.value as 'none' | 'bridge' | 'egress', allowedHosts: event.target.value === 'egress' ? env?.network?.allowedHosts : undefined } })}
            >
              <option value="none">none — default</option>
              <option value="bridge">bridge — unrestricted internet</option>
              <option value="egress">egress — approved HTTP/HTTPS hosts through a proxy</option>
            </select>
          </label>
          {env?.network?.run === 'egress' && <LinesField label="Allowed hosts (one exact DNS name per line)" values={env.network.allowedHosts} placeholder="registry.npmjs.org" onChange={(allowedHosts) => patch({ network: { ...env.network, allowedHosts } })} />}
          <span className="muted tiny">Installation policy must permit the network and every host. An egress environment cannot switch to unrestricted bridge.</span>
          <LinesField
            label="Cache directories (one per line)"
            values={env?.caches}
            placeholder={'/home/sandbox/go/pkg/mod\n/home/sandbox/.local/share/pnpm'}
            onChange={(caches) => patch({ caches })}
          />
          <span className="muted tiny">Kept in volumes across agent restarts and rebuilds; Reset clears them.</span>
          <FieldIssues issues={issues} field="spec.environment.caches" />
          <label>
            Build timeout
            <input value={env?.timeout ?? ''} placeholder="30m" onChange={(event) => patch({ timeout: event.target.value || undefined })} />
          </label>
          <FieldIssues issues={issues} field="spec.environment.timeout" />
          <EnvironmentStatus
            graph={graph}
            savedGraph={savedGraph}
            dirty={dirty}
            running={running}
            canBuild={canBuild}
            canReset={canReset}
          />
        </>
      )}
    </fieldset>
  );
}

/** One value per line; the text is kept as typed so a trailing newline can start the next value. */
function LinesField({
  label,
  values,
  placeholder,
  onChange,
}: {
  label: string;
  values?: string[];
  placeholder?: string;
  onChange: (values: string[] | undefined) => void;
}) {
  const joined = (values ?? []).join('\n');
  const [text, setText] = useState(joined);
  const parsed = (value: string) => value.split('\n').map((line) => line.trim()).filter(Boolean);
  useEffect(() => {
    setText((current) => (parsed(current).join('\n') === joined ? current : joined));
  }, [joined]);
  return (
    <label>
      {label}
      <textarea
        className="mono"
        rows={3}
        value={text}
        placeholder={placeholder}
        spellCheck={false}
        onChange={(event) => {
          setText(event.target.value);
          const next = parsed(event.target.value);
          onChange(next.length ? next : undefined);
        }}
      />
    </label>
  );
}

function EnvironmentStatus({
  graph,
  savedGraph,
  dirty,
  running,
  canBuild,
  canReset,
}: {
  graph: Graph;
  savedGraph: Graph;
  dirty: boolean;
  running: boolean;
  canBuild: boolean;
  canReset: boolean;
}) {
  const gid = savedGraph.metadata.id;
  const status = useAsync(() => api.environment(gid), [gid, savedGraph.metadata.version]);
  const reload = useThrottled(() => void status.reload(), 300);
  useEventStream({ graph: gid, prefixes: ['environment.', 'sandbox.capacity'] }, reload);
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const view = status.data;
  const building = view?.state === 'building';

  // Follow a running build: the log tail is stored every few seconds.
  useEffect(() => {
    if (!building) return;
    const timer = setInterval(() => void status.reload(), 2000);
    return () => clearInterval(timer);
  }, [building]); // eslint-disable-line react-hooks/exhaustive-deps

  const unsavedEnv = dirty && JSON.stringify(graph.spec.environment ?? null) !== JSON.stringify(savedGraph.spec.environment ?? null);
  const savedDefined = !!savedGraph.spec.environment;
  const users = environmentUsers(graph);

  const act = async (kind: 'build' | 'rebuild' | 'reset') => {
    if (kind === 'reset' && !confirm('Reset the environment? Graph-owned images and writable caches are deleted. Shared prepared images and cache snapshots are retained.')) return;
    setBusy(true);
    try {
      const next =
        kind === 'reset' ? await api.resetEnvironment(gid) : await api.buildEnvironment(gid, kind === 'rebuild');
      status.setData(next);
      setError(undefined);
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  };

  const tone = !view ? undefined : view.state === 'failed' ? 'bad' : view.state === 'ready' ? (view.upToDate ? 'ok' : 'warn') : view.state === 'building' ? 'info' : 'warn';
  return (
    <section className="environment-status" aria-label="Environment status">
      <div className="section-head">
        <h4>Build</h4>
        {view && (
          <Badge tone={tone}>{environmentPhase(view)}</Badge>
        )}
      </div>
      <ErrorBox error={status.error ?? error} />
      {unsavedEnv && <p className="field-warning">Unsaved environment changes: save the graph, then build.</p>}
      {!savedDefined && !unsavedEnv && <p className="muted small">Save the graph to build this environment.</p>}
      {view?.state === 'ready' && !view.upToDate && (
        <p className="field-warning">The saved definition or repository inputs changed since this image was built. Build it again before starting agents.</p>
      )}
      {view && <EnvironmentProgress view={view} showPhase={false} />}
      {view?.reuseKey && <p className="muted small">Workspaces and writable caches remain isolated. Reset preserves shared snapshots.</p>}
      {view?.image && (
        <p className="muted small">
          Image <code>{view.image}</code>
          {view.finishedAt ? ` · built ${fmtDateTime(view.finishedAt)}` : ''}
          {view.actor ? ` by ${view.actor}` : ''}
        </p>
      )}
      {view?.state === 'failed' && view.error && <ErrorBox error={view.error} />}
      <p className="muted small">
        {users.length
          ? `Used by: ${users.join(', ')}.`
          : 'No agent uses it yet: set Isolation to docker in an agent\'s Command sandbox.'}
      </p>
      <div className="btn-row">
        {canBuild && (
          <Btn
            small
            tone="primary"
            disabled={busy || building || !savedDefined || unsavedEnv}
            onClick={() => void act(view?.state === 'ready' && view.upToDate ? 'rebuild' : 'build')}
          >
            {view?.state === 'ready' && view.upToDate ? 'Rebuild' : view?.state === 'failed' ? 'Build again' : 'Build'}
          </Btn>
        )}
        {canReset && (
          <Btn
            small
            tone="danger"
            disabled={busy || building || running || !view || view.state === 'absent'}
            title={running ? 'Stop the graph before resetting its environment' : undefined}
            onClick={() => void act('reset')}
          >
            Reset
          </Btn>
        )}
      </div>
      {running && canReset && view && view.state !== 'absent' && (
        <p className="muted tiny">Stop the graph to reset its environment.</p>
      )}
      {view?.log && (
        <details open={view.state === 'failed' || building}>
          <summary>Build log</summary>
          <pre className="log build-log" aria-label="Environment build log">
            {view.log}
          </pre>
        </details>
      )}
    </section>
  );
}
