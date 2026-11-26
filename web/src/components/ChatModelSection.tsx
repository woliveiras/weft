import { useState } from 'react';
import { api, ApiError, errorMessage } from '../api/client';
import type { ChatModelSettings } from '../api/types';
import { useAsync } from '../hooks/useData';
import { Btn, ErrorBox } from './common';

export function ChatModelSection({ canEdit, onSaved }: { canEdit: boolean; onSaved?: (settings: ChatModelSettings) => void }) {
  const settings = useAsync(api.chatModelSettings, []);
  const [saved, setSaved] = useState(false);
  return <section className="card" aria-labelledby="chat-model-heading" aria-busy={settings.loading}>
    <h2 id="chat-model-heading">Chat model</h2>
    <p className="muted">Select which configured provider and model Chat may use for conversation requests.</p>
    {settings.loading && <p role="status">Loading Chat model selection…</p>}
    {settings.error && <><ErrorBox error={settings.error} /><Btn onClick={() => void settings.reload()}>Retry loading Chat model</Btn></>}
    {settings.data && <>
      {settings.data.model
        ? <p>Selected model: <strong>{settings.data.model.provider} · {settings.data.model.name}</strong></p>
        : <p>No Chat model selected.</p>}
      {settings.data.status === 'unavailable' && <p role="status">The selected provider is unavailable. {canEdit ? 'Select an available provider below.' : 'Contact an administrator.'}</p>}
      {canEdit && <ChatModelForm key={settings.data.revision} settings={settings.data}
        onSaved={next => { settings.setData(next); setSaved(true); onSaved?.(next); }}
        onReload={() => { setSaved(false); void settings.reload(); }}
        refreshing={settings.loading} />}
    </>}
    {saved && <p role="status">Chat model saved.</p>}
  </section>;
}

function ChatModelForm({settings, onSaved, onReload, refreshing}: {
  settings: ChatModelSettings;
  onSaved: (next: ChatModelSettings) => void;
  onReload: () => void;
  refreshing: boolean;
}) {
  const providers = useAsync(api.providers, []);
  const [provider, setProvider] = useState(settings.model?.provider ?? '');
  const [model, setModel] = useState(settings.model?.name ?? '');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [stale, setStale] = useState(false);
  const available = providers.data?.filter(item => item.status !== 'unavailable') ?? [];
  const selected = available.find(item => item.name === provider);
  const changed = provider !== settings.model?.provider || model.trim() !== settings.model?.name;
  const canSave = !busy && !refreshing && !stale && !!selected && !!model.trim() && changed;

  async function save() {
    if (!canSave) return;
    setBusy(true);
    setError(undefined);
    try {
      onSaved(await api.saveChatModel({ provider, name: model.trim() }, settings.revision));
    } catch (error) {
      const conflict = error instanceof ApiError && error.status === 409;
      setStale(conflict);
      setError(conflict ? 'The Chat model changed. Reload the saved selection before reviewing it again.' : errorMessage(error));
    } finally {
      setBusy(false);
    }
  }

  return <form className="form-stack" aria-label="Chat model selection" onSubmit={event => { event.preventDefault(); void save(); }}>
    {providers.loading && <p role="status">Loading configured providers…</p>}
    {providers.error && <><ErrorBox error={providers.error} /><Btn type="button" onClick={() => void providers.reload()}>Retry loading providers</Btn></>}
    {providers.data && available.length === 0 && <p>No available providers. <a href="#/models">Open Providers</a> to configure one.</p>}
    <label>Provider
      <select value={provider} onChange={event => { setProvider(event.target.value); setModel(''); }} required disabled={busy || refreshing || providers.loading}>
        <option value="">Select a provider</option>
        {available.map(item => <option value={item.name} key={item.name}>{item.name}</option>)}
        {provider && !selected && <option value={provider} disabled>{provider} (unavailable)</option>}
      </select>
    </label>
    <label>Model ID
      <input required maxLength={200} value={model} onChange={event => setModel(event.target.value)}
        placeholder={selected?.defaultModel} disabled={busy || refreshing}/>
    </label>
    {error && <ErrorBox error={error}/>}
    <div className="btn-row">
      {stale && <Btn type="button" disabled={refreshing} onClick={onReload}>Reload saved selection</Btn>}
      <Btn type="submit" disabled={!canSave}>{busy ? 'Saving…' : 'Save Chat model'}</Btn>
    </div>
  </form>;
}
