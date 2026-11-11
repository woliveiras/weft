import { useRef, useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { EnvironmentNetworkMode, InstallationNetwork, NetworkPolicyView } from '../api/types';
import { useAsync } from '../hooks/useData';
import { Btn, ErrorBox } from './common';

const modes: EnvironmentNetworkMode[] = ['none', 'bridge', 'egress'];

export function NetworkPolicySection({ canManage }: { canManage: boolean }) {
  return <section className="integration-section" aria-label="Installation network policy">
    <h2>Network policies</h2>
    <p className="muted">Manage the networks and public hosts environments may request. Bridge allows unrestricted internet; egress allows only approved HTTP/HTTPS hosts.</p>
    <NetworkPolicyControls key={String(canManage)} canManage={canManage} />
  </section>;
}

function Usage({ view }: { view: NetworkPolicyView }) {
  return <ul>{view.usage?.map(item => <li key={item.graph}>
    <strong>{item.graph}</strong> ({item.state}): {item.blocked ? `Blocked for new admission: ${item.reason}` : 'Permitted by this policy'}
    {item.hosts.length > 0 && <span> · Requested hosts: {item.hosts.join(', ')}</span>}
  </li>)}</ul>;
}

function NetworkPolicyControls({ canManage }: { canManage: boolean }) {
  const current = useAsync(api.networkPolicy, []);
  const [draft, setDraft] = useState<InstallationNetwork>();
  const [confirmation, setConfirmation] = useState<NetworkPolicyView>();
  const [error, setError] = useState<string>();
  const [notice, setNotice] = useState<string>();
  const [busy, setBusy] = useState(false);
  const locked = useRef(false);
  const network = draft ?? current.data?.network;
  const disabled = !canManage || busy || !!confirmation || current.loading || !!current.error;

  async function review() {
    if (!network || !current.data || locked.current) return;
    locked.current = true; setBusy(true); setError(undefined); setNotice(undefined);
    try { setConfirmation(await api.previewNetworkPolicy({ ...network, egressHosts: (network.egressHosts ?? []).map(host => host.trim()).filter(Boolean) }, current.data.revision)); }
    catch (e) { setError(`Preview failed: ${errorMessage(e)}. Reload the current policy before trying again.`); }
    finally { locked.current = false; setBusy(false); }
  }
  async function save() {
    if (!confirmation || locked.current) return;
    locked.current = true; setBusy(true); setError(undefined);
    try {
      await api.saveNetworkPolicy(confirmation.network, confirmation.revision);
      setDraft(undefined);
      setNotice('Network policy saved. Stop existing tasks and builds explicitly if their access must be revoked.');
      await current.reload();
    } catch (e) {
      setError(`Save could not be confirmed: ${errorMessage(e)}. Reload and review the latest policy before retrying.`);
      await current.reload();
    } finally { setConfirmation(undefined); locked.current = false; setBusy(false); }
  }
  function toggle(field: 'setupNetworks' | 'runNetworks', mode: EnvironmentNetworkMode) {
    if (!network) return;
    setDraft({ ...network, [field]: network[field].includes(mode) ? network[field].filter(value => value !== mode) : [...network[field], mode] });
  }
  return <>
    <Btn disabled={busy || !!confirmation || current.loading} onClick={() => { setDraft(undefined); setNotice(undefined); setError(undefined); void current.reload(); }}>Reload network policy</Btn>
    <ErrorBox error={current.error ?? error} />
    {notice && <p role="status">{notice}</p>}
    {current.loading && <p>Loading network policy…</p>}
    {current.data && network && <>
      <p className="muted small">Source: {current.data.source === 'server' ? 'server configuration' : 'saved installation policy'} · Revision {current.data.revision}{current.data.updatedBy ? ` · Updated by ${current.data.updatedBy}` : ''}</p>
      {!canManage && <p>Administrator access is required to change this policy.</p>}
      <div className="integration-grid">
        {(['setupNetworks', 'runNetworks'] as const).map(field => <fieldset key={field} disabled={disabled}>
          <legend>{field === 'setupNetworks' ? 'Build and setup networks' : 'Agent networks'}</legend>
          {modes.map(mode => <label key={mode}><input type="checkbox" checked={network[field].includes(mode)} onChange={() => toggle(field, mode)} />{mode}</label>)}
        </fieldset>)}
      </div>
      <label>Installation allowed hosts (one exact DNS name per line)
        <textarea disabled={disabled} rows={5} value={(network.egressHosts ?? []).join('\n')} onChange={event => setDraft({ ...network, egressHosts: event.target.value.split('\n') })} />
      </label>
      <p className="muted small">Exact lowercase public DNS names only; no wildcards, URLs, IPs or ports. Empty lists deny all egress destinations. Environment lists remain separate and must fit this ceiling.</p>
      <h3>Environment host usage and admission status</h3>
      <Usage view={current.data} />
      <p>Saved changes apply to new builds, approvals and sandbox admissions across controllers. Existing running work keeps its current network until stopped. Suspended environments are checked when tasks resume.</p>
      {canManage && <Btn disabled={disabled || network.setupNetworks.length === 0 || network.runNetworks.length === 0} onClick={() => void review()}>Review network policy</Btn>}
    </>}
    {confirmation && <section className="integration-card" aria-label="Confirm network policy">
      <h3>Review installation network changes</h3>
      <p>Build/setup: {confirmation.network.setupNetworks.join(', ')} · Agents: {confirmation.network.runNetworks.join(', ')}</p>
      <p>Allowed hosts: {confirmation.network.egressHosts?.join(', ') || 'none'}</p>
      <Usage view={confirmation} />
      <p>Allowing bridge grants unrestricted internet. Adding hosts expands permitted destinations. Removing access does not stop already running tasks or builds.</p>
      <Btn disabled={busy} onClick={() => void save()}>Confirm network policy</Btn>
      <Btn disabled={busy} onClick={() => setConfirmation(undefined)}>Cancel network changes</Btn>
    </section>}
  </>;
}
