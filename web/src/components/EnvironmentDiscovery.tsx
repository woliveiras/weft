import { useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { EnvironmentDiscovery as Discovery, EnvironmentSpec } from '../api/types';
import { Btn, ErrorBox } from './common';
import { EnvironmentProposal } from './EnvironmentProposal';

/** Discovery and review do not save the graph or start a build. */
export function EnvironmentDiscovery({ graphId, disabled, onAccept }: {
  graphId: string;
  disabled: boolean;
  onAccept: (environment: EnvironmentSpec) => void;
}) {
  const [result, setResult] = useState<Discovery>();
  const [selected, setSelected] = useState('');
  const [service, setService] = useState('');
  const [target, setTarget] = useState('');
  const [preview, setPreview] = useState<EnvironmentSpec>();
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [companionsReviewed, setCompanionsReviewed] = useState(false);
  const candidate = result?.candidates.find((item) => item.path === selected);

  const discover = async () => {
    setBusy(true);
    setError(undefined);
    setPreview(undefined); setCompanionsReviewed(false);
    setResult(undefined);
    setSelected('');
    setService(''); setTarget('');
    try {
      const next = await api.discoverEnvironment(graphId);
      setResult(next);
    } catch (cause) { setError(errorMessage(cause)); }
    finally { setBusy(false); }
  };
  const review = async () => {
    if (!candidate || !result) return;
    setBusy(true);
    setError(undefined);
    setPreview(undefined); setCompanionsReviewed(false);
    try {
      setPreview(await api.resolveEnvironment(graphId, {
        source: result.source, path: candidate.path, sha256: candidate.sha256,
        ...(service ? { service } : {}),
        ...(target ? { target } : {}),
      }));
    } catch (cause) { setError(errorMessage(cause)); }
    finally { setBusy(false); }
  };

  return <section aria-label="Repository environment">
    <h4>Discover from repository</h4>
    <p className="muted small">Inspect the existing checkout, select a definition and review it before accepting. Accepting updates the draft; save the graph before building.</p>
    {disabled && <p className="muted small">Save workspace changes and remove any saved or draft environment before discovering a repository definition.</p>}
    <Btn small disabled={disabled || busy} onClick={() => void discover()}>{busy ? 'Checking…' : 'Discover environment'}</Btn>
    <ErrorBox error={error} />
    {result?.status === 'configured' && <p>An environment is already saved. Remove it and save the graph before discovering another.</p>}
    {result?.status === 'proposal_required' && <EnvironmentProposal graphId={graphId} disabled={disabled || busy} />}
    {!!result?.candidates.length && <>
      <p className="muted small">Source: {result.source}</p>
      <label>Repository definition
        <select disabled={busy || disabled} value={selected} onChange={(event) => {
          setSelected(event.target.value); setService(''); setTarget(''); setPreview(undefined); setCompanionsReviewed(false); setError(undefined);
        }}>
          <option value="">Select a definition</option>
          {result.candidates.map((item) => <option key={item.path} value={item.path}>{item.path}</option>)}
        </select>
      </label>
      <ErrorBox error={candidate?.error} />
      {!!candidate?.services?.length && <label>Development service
        <select disabled={busy || disabled} value={service} onChange={(event) => {
          setService(event.target.value); setPreview(undefined); setCompanionsReviewed(false); setError(undefined);
        }}>
          <option value="">Select a service</option>
          {candidate.services.map((name) => <option key={name} value={name}>{name}</option>)}
        </select>
      </label>}
      {!!candidate?.targets?.length && <label>Build stage
        <select disabled={busy || disabled} value={target} onChange={(event) => {
          setTarget(event.target.value); setPreview(undefined); setCompanionsReviewed(false); setError(undefined);
        }}>
          <option value="">Final stage (default)</option>
          {candidate.targets.map((name) => <option key={name} value={name}>{name}</option>)}
        </select>
      </label>}
      <Btn small disabled={disabled || busy || !candidate || !!candidate.error || (result.source === 'compose' && !service)} onClick={() => void review()}>Review environment</Btn>
    </>}
    {preview && <>
      <p>Review the image, setup commands and network settings below. Nothing has been saved or built.</p>
      <pre aria-label="Environment proposal">{JSON.stringify({ ...preview, origin: undefined }, null, 2)}</pre>
      {!!preview.companions?.length && <section aria-label="Review auxiliary services">
        <h4>Auxiliary services</h4>
        <p>These dependencies start only when the agent runs, in private containers for each replica. They share an internal network with the agent, without published ports or internet access. Build and setup do not start them.</p>
        <ul>{preview.companions.map(item => <li key={item.name}><strong>{item.name}</strong>: <code>{item.image}</code>{item.command && <pre>{JSON.stringify(item.command)}</pre>}</li>)}</ul>
        <p>Each helper uses the installation CPU/memory defaults. The entire group waits for capacity together. Services run as uid 10001; startup checks container running state, not application health. Stop/reset removes ephemeral helper data; idle suspension preserves it.</p>
        <label><input type="checkbox" checked={companionsReviewed} disabled={disabled || busy} onChange={e => setCompanionsReviewed(e.target.checked)} /> I reviewed these auxiliary services and their internal network</label>
      </section>}
      <Btn small disabled={disabled || busy || (!!preview.companions?.length && !companionsReviewed)} onClick={() => { onAccept(preview); setPreview(undefined); setCompanionsReviewed(false); setResult(undefined); }}>Accept into draft</Btn>
    </>}
  </section>;
}
