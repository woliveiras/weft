import { useEffect, useState } from 'react';
import { api, ApiError, errorMessage } from '../api/client';
import type { ChatModelSettings } from '../api/types';
import { useAsync } from '../hooks/useData';
import { Btn, ErrorBox } from './common';
import './chatModel.css';

export function ChatModelSection({ canEdit, onSaved }: { canEdit: boolean; onSaved?: (settings: ChatModelSettings) => void }) {
  const settings = useAsync(api.chatModelSettings, []);
  const [saved, setSaved] = useState(false);
  return <section className="card chat-model-settings" aria-labelledby="chat-model-heading" aria-busy={settings.loading}>
    <h2 id="chat-model-heading">Chat model</h2>
    <p className="muted">Choose the model that handles conversation requests and generates titles.</p>
    {settings.loading && <p role="status">Loading Chat model selection…</p>}
    {settings.error && <><ErrorBox error={settings.error} /><Btn onClick={() => void settings.reload()}>Retry loading Chat model</Btn></>}
    {settings.data && <>
      {settings.data.model
        ? <p>Selected model: <strong>{settings.data.model.provider} · {settings.data.model.name}</strong></p>
        : !canEdit && <p>No Chat model selected. Contact an administrator to configure Chat.</p>}
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
  const [attempt, setAttempt] = useState(0);
  const [catalog, setCatalog] = useState<{provider:string; attempt:number; ids:string[]; error?:string}>();
  const selectedName = selected?.name;
  useEffect(() => {
    if (!selectedName) return;
    let current = true;
    api.models(selectedName).then(items => {
      if (current) setCatalog({provider:selectedName,attempt,ids:[...new Set(items.map(item => item.id).filter(Boolean))].sort()});
    }).catch(error => {
      if (current) setCatalog({provider:selectedName,attempt,ids:[],error:errorMessage(error)});
    });
    return () => { current = false; };
  }, [selectedName, attempt]);
  const listing = catalog?.provider === selectedName && catalog?.attempt === attempt ? catalog : undefined;
  const choices = listing?.ids ?? [];
  const configuredDefault = selected?.defaultModel;
  const defaultOnly = configuredDefault && !choices.includes(configuredDefault);
  const canChoose = !!listing && (choices.length > 0 || !!configuredDefault);
  const listed = choices.includes(model) || (!!configuredDefault && model === configuredDefault);
  const changed = provider !== settings.model?.provider || model.trim() !== settings.model?.name;
  const canSave = !busy && !refreshing && !stale && !!selected && canChoose && listed && changed;

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

  return <form className="chat-model-form" aria-label="Chat model selection" onSubmit={event => { event.preventDefault(); void save(); }}>
    {providers.loading && <p role="status">Loading configured providers…</p>}
    {providers.error && <><ErrorBox error={providers.error} /><Btn type="button" onClick={() => void providers.reload()}>Retry loading providers</Btn></>}
    {providers.data && available.length === 0 && <p>No available providers. <a href="#/models">Open Providers</a> to configure one.</p>}
    <div className="chat-model-fields"><label>Provider
      <select value={provider} onChange={event => { setProvider(event.target.value); setModel(''); setError(undefined); }} required disabled={busy || refreshing || providers.loading}>
        <option value="">Select a provider</option>
        {available.map(item => <option value={item.name} key={item.name}>{item.name}</option>)}
        {provider && !selected && <option value={provider} disabled>{provider} (unavailable)</option>}
      </select>
    </label>
    <label>Model ID
      <select required value={model} disabled={busy || refreshing || !canChoose} onChange={event => setModel(event.target.value)}>
        <option value="">{!selected ? 'Choose a provider first' : !listing ? 'Loading models…' : canChoose ? 'Select a model' : 'No models available'}</option>
        {model && !listed && <option value={model} disabled>{model} (saved)</option>}
        {defaultOnly && <option value={configuredDefault}>{configuredDefault} (provider default)</option>}
        {choices.map(id => <option value={id} key={id}>{id}</option>)}
      </select>
    </label></div>
    {selected && !listing && <p className="muted" role="status">Loading models from {selected.name}…</p>}
    {listing?.error && <ErrorBox error={listing.error}/>}
    {listing && !listing.error && !choices.length && <p className="muted" role="status">The provider returned an empty model list.</p>}
    {listing && (listing.error || !choices.length) && <Btn type="button" onClick={() => setAttempt(n => n+1)}>Reload models</Btn>}
    {error && <ErrorBox error={error}/>}
    <div className="btn-row">
      {stale && <Btn type="button" disabled={refreshing} onClick={onReload}>Reload saved selection</Btn>}
      <Btn tone="primary" type="submit" disabled={!canSave}>{busy ? 'Saving…' : 'Save Chat model'}</Btn>
    </div>
  </form>;
}
