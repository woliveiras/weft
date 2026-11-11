import { ContextHelp } from './common';
import { useImperativeHandle, useState, type Ref } from 'react';
import { ApiError, api, errorMessage } from '../api/client';
import type { ProviderInfo, ProviderPlugin, ProviderSettings, ProviderTestResult } from '../api/types';
import { useAsync } from '../hooks/useData';
import { Badge, Btn, ErrorBox, Modal, fmtDateTime, fmtTime } from './common';
import {
  credentialLabel,
  describeTest,
  draftFrom,
  fieldErrors,
  inputFromDraft,
  localErrors,
  type ProviderDraft,
} from './providers';

export interface ProvidersHandle {
  /** Tests every provider (one tiny prompt each). */
  testAll: () => Promise<void>;
}

interface Props {
  /** Admins add, edit, remove providers and pick the default. */
  canEdit: boolean;
  /** Operators and admins run connection tests (they cost tokens). */
  canTest: boolean;
  ref?: Ref<ProvidersHandle>;
}

type Testing = Record<string, boolean>;

export function ProvidersSection({ canEdit, canTest, ref }: Props) {
  const providers = useAsync(api.providers, []);
  const settings = useAsync(api.providerSettings, []);
  const plugins = useAsync(api.providerPlugins, []);
  const [pluginErrors, setPluginErrors] = useState<Record<string, string>>({});
  const [switching, setSwitching] = useState<Record<string, boolean>>({});
  const [results, setResults] = useState<Record<string, ProviderTestResult>>({});
  const [testing, setTesting] = useState<Testing>({});
  const [cardErrors, setCardErrors] = useState<Record<string, string>>({});
  const [editing, setEditing] = useState<{ mode: 'create' } | { mode: 'edit'; provider: ProviderInfo }>();

  const reload = async () => {
    await Promise.all([providers.reload(), settings.reload(), plugins.reload()]);
  };

  const setPluginEnabled = async (p: ProviderPlugin, enabled: boolean) => {
    if (!enabled && p.instances.length > 0) {
      const ok = confirm(
        `Disable the ${p.label} plugin? Its providers stay configured but become unavailable, and agents using them fail until it is enabled again: ${p.instances.join(', ')}.`,
      );
      if (!ok) return;
    }
    setSwitching((cur) => ({ ...cur, [p.id]: true }));
    setPluginErrors((cur) => ({ ...cur, [p.id]: '' }));
    try {
      await api.setProviderPluginEnabled(p.id, enabled, settings.data?.revision);
      await reload();
    } catch (e) {
      setPluginErrors((cur) => ({ ...cur, [p.id]: conflictMessage(e) }));
    } finally {
      setSwitching((cur) => ({ ...cur, [p.id]: false }));
    }
  };

  const runTest = async (name: string) => {
    setTesting((t) => ({ ...t, [name]: true }));
    setCardErrors((e) => ({ ...e, [name]: '' }));
    try {
      const r = await api.testProvider(name);
      setResults((cur) => ({ ...cur, [name]: r }));
    } catch (e) {
      setCardErrors((cur) => ({ ...cur, [name]: `Test could not run: ${errorMessage(e)}` }));
    } finally {
      setTesting((t) => ({ ...t, [name]: false }));
    }
  };

  useImperativeHandle(ref, () => ({
    testAll: async () => {
      if (!canTest) return;
      await Promise.all((providers.data ?? []).map((p) => runTest(p.name)));
    },
  }));

  const remove = async (p: ProviderInfo) => {
    if (!confirm(`Remove provider “${p.name}”? Agents that select it will no longer start.`)) return;
    try {
      await api.deleteProvider(p.name, settings.data?.revision);
      await reload();
    } catch (e) {
      setCardErrors((cur) => ({ ...cur, [p.name]: conflictMessage(e) }));
    }
  };

  const makeDefault = async (p: ProviderInfo) => {
    try {
      await api.setDefaultProvider(p.name, settings.data?.revision);
      await reload();
    } catch (e) {
      setCardErrors((cur) => ({ ...cur, [p.name]: conflictMessage(e) }));
    }
  };

  const meta = settings.data;
  const list = providers.data ?? [];
  const enabledTypes = new Set((plugins.data ?? []).filter((p) => p.enabled).map((p) => p.id));

  return (
    <section className="integration-section" aria-labelledby="providers-heading">
      <div className="section-head">
        <div>
          <h2 id="providers-heading">Model providers</h2>
          <p className="muted small">
            Agents call models only through Weft; credentials stay on the server and are never shown again after saving.
          </p>
          <ContextHelp title="What a provider test does"><p>A test lists models and sends one short prompt. It uses the configured provider and may incur usage charges.</p></ContextHelp>
        </div>
        <div className="integration-card-status">
          <Badge>{list.length} configured</Badge>
          {canEdit && (
            <Btn
              tone="primary"
              small
              onClick={() => setEditing({ mode: 'create' })}
              disabled={!meta || !plugins.data || enabledTypes.size === 0}
              title={plugins.data && enabledTypes.size === 0 ? 'Enable an inference plugin first' : undefined}
            >
              Add provider
            </Btn>
          )}
        </div>
      </div>
      {meta && !meta.secrets.enabled && (
        <p className="integration-note tone-note" role="status">
          Credentials cannot be stored: the server has no encryption key. Set <code>WEFT_SECRET_KEY</code> (or{' '}
          <code>secrets.keyFile</code>) and restart. Providers that need no key can still be added.
        </p>
      )}
      <PluginList plugins={plugins.data} canEdit={canEdit} switching={switching} errors={pluginErrors} onToggle={(p, on) => void setPluginEnabled(p, on)} />
      <ErrorBox error={plugins.error ? `Could not load inference plugins: ${plugins.error}` : undefined} />
      <ErrorBox error={providers.error ? `Could not load providers: ${providers.error}` : undefined} />
      <ErrorBox error={settings.error ? `Could not load provider settings: ${settings.error}` : undefined} />
      {providers.loading && !providers.data ? (
        <p className="muted">Loading providers…</p>
      ) : list.length === 0 ? (
        <p className="empty">
          No model providers yet.{' '}
          {canEdit
            ? enabledTypes.size === 0
              ? 'Enable an inference plugin above, then add a provider to let agents use a model.'
              : 'Add one to let agents use a model.'
            : 'Ask an administrator to enable an inference plugin and add a provider.'}
        </p>
      ) : (
        <div className="integration-grid">
          {list.map((p) => (
            <ProviderCard
              key={p.name}
              provider={p}
              result={results[p.name] ?? p.lastTest}
              testing={!!testing[p.name]}
              error={cardErrors[p.name]}
              canEdit={canEdit}
              canTest={canTest}
              onTest={() => void runTest(p.name)}
              onEdit={() => setEditing({ mode: 'edit', provider: p })}
              onRemove={() => void remove(p)}
              onDefault={() => void makeDefault(p)}
            />
          ))}
        </div>
      )}
      {editing && meta && (
        <ProviderForm
          mode={editing.mode}
          provider={editing.mode === 'edit' ? editing.provider : undefined}
          settings={{ ...meta, types: meta.types.filter((t) => enabledTypes.has(t.type) || (editing.mode === 'edit' && t.type === editing.provider.type)) }}
          onClose={() => setEditing(undefined)}
          onSaved={async (name) => {
            setEditing(undefined);
            setResults((cur) => {
              const next = { ...cur };
              delete next[name];
              return next;
            });
            await reload();
          }}
        />
      )}
    </section>
  );
}

function PluginList({
  plugins,
  canEdit,
  switching,
  errors,
  onToggle,
}: {
  plugins?: ProviderPlugin[];
  canEdit: boolean;
  switching: Record<string, boolean>;
  errors: Record<string, string>;
  onToggle: (p: ProviderPlugin, enabled: boolean) => void;
}) {
  if (!plugins) return null;
  return (
    <div className="plugin-list" role="group" aria-labelledby="plugins-heading">
      <h3 id="plugins-heading">Inference plugins</h3>
      <p className="muted small">
        Plugins are the kinds of model server Weft can use. Every plugin starts disabled; enable the ones this installation may use, then add providers from them.
      </p>
      <ul className="integration-grid">
        {plugins.map((p) => {
          const caps = [
            p.capabilities.tools && 'Tool calls',
            `JSON (${p.capabilities.json})`,
            p.capabilities.usage && 'Usage',
            p.capabilities.modelListing && 'Model listing',
            p.capabilities.streaming && 'Streaming',
          ].filter(Boolean) as string[];
          return (
            <li key={p.id} className="integration-card" aria-label={`Inference plugin ${p.label}`}>
              <div className="integration-card-head">
                <strong>{p.label}</strong>
                <span className="integration-card-status">
                  <Badge>{p.local ? 'Local' : 'Hosted'}</Badge>
                  {p.devOnly && <Badge>Development</Badge>}
                  <Badge tone={p.enabled ? 'ok' : undefined}>{p.enabled ? 'Enabled' : 'Disabled'}</Badge>
                </span>
              </div>
              {p.description && <p className="muted small">{p.description}</p>}
              <p className="small">
                {caps.join(' · ')}
                {p.key === 'required' ? ' · API key required' : p.key === 'optional' ? ' · API key optional' : ''}
              </p>
              <p className="small muted">
                {p.instances.length ? `Providers: ${p.instances.join(', ')}` : 'No providers yet'}
                {p.enabledBy === 'config' && ' · Provided by server configuration'}
              </p>
              {canEdit && p.enabledBy !== 'config' && (
                <Btn small disabled={!!switching[p.id]} onClick={() => onToggle(p, !p.enabled)}>
                  {p.enabled ? 'Disable' : 'Enable'}
                </Btn>
              )}
              <ErrorBox error={errors[p.id] || undefined} />
            </li>
          );
        })}
      </ul>
    </div>
  );
}

function conflictMessage(e: unknown): string {
  if (e instanceof ApiError && e.status === 409 && e.message.includes('revision')) {
    return 'Provider settings were changed by someone else. Reload the page and try again.';
  }
  return errorMessage(e);
}

function ProviderCard({
  provider: p,
  result,
  testing,
  error,
  canEdit,
  canTest,
  onTest,
  onEdit,
  onRemove,
  onDefault,
}: {
  provider: ProviderInfo;
  result?: ProviderTestResult;
  testing: boolean;
  error?: string;
  canEdit: boolean;
  canTest: boolean;
  onTest: () => void;
  onEdit: () => void;
  onRemove: () => void;
  onDefault: () => void;
}) {
  const summary = result ? describeTest(result) : undefined;
  return (
    <article className="integration-card" aria-label={`Provider ${p.name}`}>
      <div className="integration-card-head">
        <div>
          <h3>{p.name}</h3>
          <p className="muted small">
            {p.type} · {p.source === 'managed' ? 'added in the console' : 'server configuration'}
          </p>
        </div>
        <div className="integration-card-status">
          {p.default && <Badge tone="info">Default</Badge>}
          {p.status === 'unavailable' ? (
            <Badge tone="bad">Unavailable</Badge>
          ) : summary ? (
            <Badge tone={summary.tone}>{summary.tone === 'bad' ? 'Failing' : summary.tone === 'warn' ? 'Works, check' : 'Working'}</Badge>
          ) : (
            <Badge>Not tested</Badge>
          )}
        </div>
      </div>
      <dl className="integration-details">
        <dt>Default model</dt>
        <dd>{p.defaultModel || 'Not set'}</dd>
        {(p.contextWindow || p.type === 'ollama') && (
          <>
            <dt>Context window</dt>
            <dd>{p.contextWindow ? `${p.contextWindow.toLocaleString()} tokens` : p.type === 'ollama' ? '32,768 tokens (default)' : ''}</dd>
          </>
        )}
        {p.baseUrl && (
          <>
            <dt>Endpoint</dt>
            <dd>{p.baseUrl}</dd>
          </>
        )}
        <dt>Credential</dt>
        <dd>
          {credentialLabel(p.credential)}
          {p.credential?.updatedAt ? ` · updated ${fmtDateTime(p.credential.updatedAt)}` : ''}
        </dd>
      </dl>
      {p.status === 'unavailable' && p.error && <p className="integration-note">{p.error}</p>}
      {p.credential?.error && p.credential.error !== p.error && <p className="integration-note">{p.credential.error}</p>}
      {summary && result && (
        <div className={`provider-test tone-${summary.tone}`} role="status">
          <strong>{summary.title}</strong>
          <span>{summary.detail}</span>
          <span className="muted small">
            {result.draft ? 'Unsaved settings · ' : ''}checked {fmtTime(result.checkedAt)}
          </span>
        </div>
      )}
      {error && <p className="integration-note">{error}</p>}
      <div className="integration-card-actions">
        {canTest && (
          <Btn small onClick={onTest} disabled={testing}>
            {testing ? 'Testing…' : 'Test connection'}
          </Btn>
        )}
        {canEdit && p.editable && (
          <Btn small onClick={onEdit}>
            Edit
          </Btn>
        )}
        {canEdit && !p.default && (
          <Btn small tone="ghost" onClick={onDefault}>
            Make default
          </Btn>
        )}
        {canEdit && p.editable && !p.default && (
          <Btn small tone="danger" onClick={onRemove}>
            Remove
          </Btn>
        )}
      </div>
    </article>
  );
}

function ProviderForm({
  mode,
  provider,
  settings,
  onClose,
  onSaved,
}: {
  mode: 'create' | 'edit';
  provider?: ProviderInfo;
  settings: ProviderSettings;
  onClose: () => void;
  onSaved: (name: string) => Promise<void>;
}) {
  const [draft, setDraft] = useState<ProviderDraft>(() => draftFrom(provider, settings.types));
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string>();
  const [busy, setBusy] = useState<'save' | 'test'>();
  const [test, setTest] = useState<ProviderTestResult>();
  const type = settings.types.find((t) => t.type === draft.type);
  const stored = provider?.credential?.source === 'stored' && provider.credential.configured;
  const set = (patch: Partial<ProviderDraft>) => {
    setDraft((d) => ({ ...d, ...patch }));
    setTest(undefined);
  };

  const prepare = () => {
    const input = inputFromDraft(draft, mode);
    const local = localErrors(input, mode);
    setErrors(local);
    setFormError(undefined);
    return Object.keys(local).length ? undefined : input;
  };

  const runTest = async () => {
    const input = prepare();
    if (!input) return;
    setBusy('test');
    try {
      setTest(await api.testProviderDraft(input));
    } catch (e) {
      setErrors(fieldErrors(e));
      setFormError(Object.keys(fieldErrors(e)).length ? undefined : errorMessage(e));
    } finally {
      setBusy(undefined);
    }
  };

  const save = async () => {
    const input = prepare();
    if (!input) return;
    setBusy('save');
    try {
      const saved =
        mode === 'create'
          ? await api.createProvider(input, settings.revision)
          : await api.updateProvider(provider!.name, input, settings.revision);
      setDraft((d) => ({ ...d, apiKey: '' }));
      await onSaved(saved.name);
    } catch (e) {
      const fe = fieldErrors(e);
      setErrors(fe);
      if (!Object.keys(fe).length) setFormError(conflictMessage(e));
    } finally {
      setBusy(undefined);
    }
  };

  const summary = test ? describeTest(test) : undefined;
  return (
    <Modal title={mode === 'create' ? 'Add model provider' : `Edit ${provider?.name}`} onClose={onClose}>
      <form
        className="form provider-form"
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
        noValidate
      >
        {mode === 'create' && (
          <Field label="Name" error={errors.name} hint="Agents select the provider by this name, e.g. team-openai.">
            <input value={draft.name} onChange={(e) => set({ name: e.target.value })} autoFocus required />
          </Field>
        )}
        <Field label="Type" error={errors.type}>
          <select value={draft.type} onChange={(e) => set({ type: e.target.value })}>
            {settings.types.map((t) => (
              <option key={t.type} value={t.type}>
                {t.label}
              </option>
            ))}
          </select>
        </Field>
        <Field
          label="Base URL"
          error={errors.baseUrl}
          hint={type?.defaultBaseUrl ? `Leave empty for ${type.defaultBaseUrl}. The URL must be reachable from the Weft server.` : undefined}
        >
          <input
            value={draft.baseUrl}
            onChange={(e) => set({ baseUrl: e.target.value })}
            placeholder={type?.defaultBaseUrl}
            inputMode="url"
          />
        </Field>
        <Field label="Default model" error={errors.defaultModel} hint="Used by agents that do not choose a model.">
          <input value={draft.defaultModel} onChange={(e) => set({ defaultModel: e.target.value })} />
        </Field>
        <Field
          label="API key"
          error={errors.apiKey}
          hint={
            !settings.secrets.enabled
              ? 'The server has no encryption key, so credentials cannot be stored.'
              : stored
                ? 'A credential is stored. Leave empty to keep it; type a new one to replace it.'
                : type?.keyRequired
                  ? 'Required for this provider type. It is encrypted on the server and never shown again.'
                  : 'Optional for this provider type.'
          }
        >
          <input
            type="password"
            value={draft.apiKey}
            onChange={(e) => set({ apiKey: e.target.value, clearKey: false })}
            placeholder={stored ? '•••••••• stored' : ''}
            autoComplete="new-password"
            spellCheck={false}
            disabled={!settings.secrets.enabled || draft.clearKey}
          />
        </Field>
        {stored && (
          <label className="check">
            <input type="checkbox" checked={draft.clearKey} onChange={(e) => set({ clearKey: e.target.checked, apiKey: '' })} />
            Remove the stored credential
          </label>
        )}
        <details className="provider-advanced">
          <summary>Limits</summary>
          <div className="provider-advanced-grid">
            <Field label="Timeout (seconds)" error={errors.timeoutSec}>
              <input value={draft.timeoutSec} onChange={(e) => set({ timeoutSec: e.target.value })} inputMode="numeric" placeholder="300" />
            </Field>
            <Field label="Retries" error={errors.maxRetries}>
              <input value={draft.maxRetries} onChange={(e) => set({ maxRetries: e.target.value })} inputMode="numeric" placeholder="3" />
            </Field>
            <Field label="Concurrent requests" error={errors.concurrency}>
              <input value={draft.concurrency} onChange={(e) => set({ concurrency: e.target.value })} inputMode="numeric" placeholder="unlimited" />
            </Field>
            <Field
              label="Context window (tokens)"
              error={errors.contextWindow}
              hint={draft.type === 'ollama' ? 'Sent as num_ctx. Default 32768; larger windows need more memory on the model server.' : 'Prompts that do not fit fail instead of being truncated.'}
            >
              <input
                value={draft.contextWindow}
                onChange={(e) => set({ contextWindow: e.target.value })}
                inputMode="numeric"
                placeholder={draft.type === 'ollama' ? '32768' : 'provider default'}
              />
            </Field>
            <Field
              label="Model guidance"
              error={errors.modelGuidance}
              hint="Small models: compact tool schemas, call shapes in the prompt and one tool call per reply. Measured on the evaluation suite; off by default."
            >
              <select value={draft.modelGuidance} onChange={(e) => set({ modelGuidance: e.target.value === 'small' ? 'small' : '' })}>
                <option value="">Standard</option>
                <option value="small">Small model</option>
              </select>
            </Field>
          </div>
        </details>
        {summary && (
          <div className={`provider-test tone-${summary.tone}`} role="status">
            <strong>{summary.title}</strong>
            <span>{summary.detail}</span>
          </div>
        )}
        <ErrorBox error={formError} />
        <div className="provider-form-actions">
          <Btn onClick={() => void runTest()} disabled={!!busy}>
            {busy === 'test' ? 'Testing…' : 'Test settings'}
          </Btn>
          <span className="spacer" />
          <Btn tone="ghost" onClick={onClose} disabled={busy === 'save'}>
            Cancel
          </Btn>
          <Btn tone="primary" type="submit" disabled={!!busy}>
            {busy === 'save' ? 'Saving…' : mode === 'create' ? 'Add provider' : 'Save changes'}
          </Btn>
        </div>
      </form>
    </Modal>
  );
}

function Field({ label, error, hint, children }: { label: string; error?: string; hint?: string; children: React.ReactNode }) {
  return (
    <label className={error ? 'has-error' : undefined}>
      {label}
      {children}
      {error ? (
        <span className="field-error" role="alert">
          {error}
        </span>
      ) : hint ? (
        <span className="field-hint">{hint}</span>
      ) : null}
    </label>
  );
}
