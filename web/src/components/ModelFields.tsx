import { useEffect, useId, useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { ModelRef, ProviderInfo } from '../api/types';
import { useAsync } from '../hooks/useData';

/**
 * Provider and model selection for one agent. The provider list comes from
 * the server (configured and console-managed providers); models are
 * suggested from the selected provider but any name can be typed.
 */
export function ModelFields({
  model,
  inherited,
  onChange,
}: {
  /** The node's own model reference (what is saved). */
  model?: ModelRef;
  /** Values inherited from graph defaults, shown as placeholders. */
  inherited?: ModelRef;
  onChange: (next: ModelRef | undefined) => void;
}) {
  const providers = useAsync(api.providers, []);
  const listId = useId();
  const list = providers.data ?? [];
  const selected = model?.provider ?? '';
  const def = list.find((p) => p.default);
  const effectiveName = selected || inherited?.provider || def?.name || '';
  const effective = list.find((p) => p.name === effectiveName);
  const [models, setModels] = useState<{ provider: string; ids: string[]; error?: string }>();

  useEffect(() => {
    if (!effective || effective.status === 'unavailable') return;
    let live = true;
    api
      .models(effective.name)
      .then((ms) => live && setModels({ provider: effective.name, ids: ms.map((m) => m.id) }))
      .catch((e) => live && setModels({ provider: effective.name, ids: [], error: errorMessage(e) }));
    return () => {
      live = false;
    };
  }, [effective?.name, effective?.status]); // eslint-disable-line react-hooks/exhaustive-deps

  const update = (patch: Partial<ModelRef>) => {
    const next: ModelRef = { ...model, ...patch };
    for (const k of Object.keys(next) as (keyof ModelRef)[]) if (next[k] === undefined || next[k] === '') delete next[k];
    onChange(Object.keys(next).length ? next : undefined);
  };

  const missing = selected !== '' && providers.data !== undefined && !list.some((p) => p.name === selected);
  const problem = missing
    ? `Provider “${selected}” is not configured. The agent will not start until it exists (Integrations).`
    : effective?.status === 'unavailable'
      ? `Provider “${effective.name}” is unavailable: ${effective.error ?? 'see Integrations'}.`
      : !effectiveName && providers.data
        ? 'No default provider is configured. Add one under Integrations.'
        : undefined;
  const modelHint =
    models && models.provider === effectiveName
      ? models.error
        ? `Could not list models (${models.error}); type the model name.`
        : models.ids.length
          ? `${models.ids.length} models available from ${effectiveName}.`
          : undefined
      : undefined;

  return (
    <>
      <label>
        Provider
        <select value={selected} onChange={(e) => update({ provider: e.target.value || undefined })} aria-invalid={!!problem}>
          <option value="">
            {inherited?.provider ? `Graph default (${inherited.provider})` : `Default provider${def ? ` (${def.name})` : ''}`}
          </option>
          {list.map((p: ProviderInfo) => (
            <option key={p.name} value={p.name}>
              {p.name}
              {p.status === 'unavailable' ? ' — unavailable' : ''}
            </option>
          ))}
          {missing && <option value={selected}>{selected} — not configured</option>}
        </select>
        {providers.error && <span className="field-error">Could not load providers: {providers.error}</span>}
        {problem && (
          <span className="field-error" role="alert">
            {problem}
          </span>
        )}
      </label>
      <label>
        Model
        <input
          value={model?.name ?? ''}
          list={listId}
          onChange={(e) => update({ name: e.target.value || undefined })}
          placeholder={inherited?.name || effective?.defaultModel || 'provider default'}
        />
        <datalist id={listId}>
          {(models?.provider === effectiveName ? models.ids : []).map((id) => (
            <option key={id} value={id} />
          ))}
        </datalist>
        {modelHint && <span className="field-hint">{modelHint}</span>}
      </label>
    </>
  );
}
