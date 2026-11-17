import { useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { Info } from '../api/types';
import { useAsync } from '../hooks/useData';
import { Btn, ErrorBox } from './common';
import { exampleGraph, freshProgress, guideSteps, parseProgress, progressKey, reviewStep, type GuideProgress } from './onboardingModel';

export function Onboarding({ identity, onCreated }: { identity: NonNullable<Info['identity']>; onCreated: (id: string) => void }) {
  const key = progressKey(identity.name, identity.role);
  const [initial] = useState(() => {
    try { return { progress: parseProgress(localStorage.getItem(key)), error: undefined as string | undefined }; }
    catch (e) { return { progress: freshProgress(), error: errorMessage(e) }; }
  });
  const [progress, setProgress] = useState(initial.progress);
  const [error, setError] = useState(initial.error);
  const [open, setOpen] = useState(false);
  const [restart, setRestart] = useState(false);
  const [creating, setCreating] = useState(false);
  const [exampleId] = useState(() => 'first-steps-' + crypto.randomUUID());
  const [createError, setCreateError] = useState<string>();
  const save = (next: GuideProgress) => {
    try { localStorage.setItem(key, JSON.stringify(next)); setError(undefined); }
    catch { setError('Progress could not be saved in this browser. You can continue, but reloading may lose your place.'); }
    setProgress(next);
  };
  const create = async () => {
    setCreating(true); setCreateError(undefined);
    try {
      const graph = exampleGraph(exampleId);
      const validation = await api.validateGraph(graph);
      if (!validation.valid) throw new Error('Example validation failed. No graph was created. ' + (validation.errors ?? []).map(issue => issue.path + ': ' + issue.message).join('; '));
      const result = await api.createGraph(graph);
      onCreated(result.graph.metadata.id);
      setOpen(false);
    } catch (e) { setCreateError(errorMessage(e)); }
    finally { setCreating(false); }
  };
  const step = guideSteps[progress.current];
  return <section className="onboarding" aria-label="Getting started">
    <div className="btn-row">
      <Btn aria-expanded={open} onClick={() => setOpen(!open)}>{open ? 'Pause guide' : 'Getting started'}</Btn>
      <span className="muted small">{progress.reviewed.length}/{guideSteps.length} topics reviewed{progress.reviewed.length === guideSteps.length ? ' · Guide complete' : ''}</span>
    </div>
    {open && <div className="card form">
      <h2>Getting started</h2>
      <p>Optional guidance. Pause at any time and reopen Getting started to resume. Reviewing a topic records your acknowledgement, not a successful configuration check.</p>
      <p className="muted small">Progress is stored in this browser for your sign-in identity. Shared identities share progress. It does not sync across devices. No tokens or configuration values are stored by this guide.</p>
      <ErrorBox error={error}/>
      <nav className="btn-row" aria-label="Guide topics">{guideSteps.map((item, index) =>
        <Btn key={item.id} aria-current={progress.current === index ? 'step' : undefined} onClick={() => save({...progress, current: index})}>{index + 1}. {item.title}{progress.reviewed.includes(item.id) ? ' ✓' : ''}</Btn>)}</nav>
      <h3>{step.title}</h3><p>{step.text}</p>
      {step.id === 'health' && <a href="#/administration/health">Open System health</a>}
      {step.id === 'models' && <a href="#/models">Open Models</a>}
      {step.id === 'connections' && <a href="#/connections">Open Connections</a>}
      {(step.id === 'graph' || step.id === 'run') && <a href="#/graphs">Open Graphs</a>}
      {step.id === 'graph' && <>
        <p>The example creates one echo agent with no tool grants. It does not start automatically. Add agents and channels only if you want to explore further.</p>
        <Btn disabled={creating || identity.role === 'viewer'} onClick={() => void create()}>{creating ? 'Creating example…' : 'Create echo example'}</Btn>
        {identity.role === 'viewer' && <p>Viewers can explore the guide. Ask an operator or administrator to create and run graphs.</p>}
        <ErrorBox error={createError}/>
      </>}
      {['health', 'models', 'connections'].includes(step.id) && <Prerequisites key={step.id} topic={step.id}/>}
      <div className="btn-row">
        <Btn tone="primary" onClick={() => save(reviewStep(progress))}>Mark reviewed and continue</Btn>
        <Btn disabled={progress.current === guideSteps.length - 1} onClick={() => save({...progress, current: progress.current + 1})}>Skip for now</Btn>
        <Btn onClick={() => setOpen(false)}>Pause guide</Btn>
        <Btn onClick={() => setRestart(true)}>Restart guide</Btn>
      </div>
      {restart && <div role="group" aria-label="Restart guide confirmation"><p>Reset only your guide progress? Graphs, drafts, connections and permissions will remain unchanged.</p>
        <Btn onClick={() => { save(freshProgress()); setRestart(false); }}>Confirm restart</Btn><Btn onClick={() => setRestart(false)}>Keep progress</Btn></div>}
    </div>}
  </section>;
}

function Prerequisites({ topic }: { topic: string }) {
  const result = useAsync(async () => {
    if (topic === 'health') { const health = await api.health(); return 'API health: ' + health.status + '. Inspect individual dependencies in System health.'; }
    if (topic === 'models') { const providers = await api.providers(); return providers.length
      ? providers.length + ' provider(s) configured. Configuration alone does not prove inference works. Inspect provider tests in Models and a real execution in Runs.'
      : 'No model providers configured. Configure one in Models, or use the echo example without inference.'; }
    const connections = await api.mcpConnections();
    return connections.length ? connections.length + ' connection(s) saved. Inspect authorization, discovery and grants in Connections; a saved connection is not proof of tool access.'
      : 'No MCP connections saved. This is optional for the echo example.';
  }, [topic]);
  return <section aria-label="Current prerequisites"><h4>Current prerequisites</h4>
    {result.loading ? <p>Checking…</p> : result.error ? <ErrorBox error={'Could not check prerequisites: ' + result.error}/> : <p role="status">{result.data}</p>}
    <Btn disabled={result.loading} onClick={() => void result.reload()}>Refresh prerequisites</Btn>
  </section>;
}
