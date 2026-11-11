import { ApiError } from '../api/client';
import type { CredentialState, ProviderInfo, ProviderInput, ProviderTestResult, ProviderTypeInfo } from '../api/types';

/** Form state of the provider editor. The API key lives here only while typed. */
export interface ProviderDraft {
  name: string;
  type: string;
  baseUrl: string;
  defaultModel: string;
  timeoutSec: string;
  maxRetries: string;
  concurrency: string;
  contextWindow: string;
  modelGuidance: '' | 'small';
  project: string;
  region: string;
  apiKey: string;
  clearKey: boolean;
}

export function draftFrom(provider: ProviderInfo | undefined, types: ProviderTypeInfo[]): ProviderDraft {
  const num = (n?: number) => (n ? String(n) : '');
  return {
    name: provider?.name ?? '',
    type: provider?.type ?? types.find((t) => t.type === 'openai')?.type ?? types[0]?.type ?? '',
    baseUrl: provider?.baseUrl ?? '',
    defaultModel: provider?.defaultModel ?? '',
    timeoutSec: num(provider?.timeoutSec),
    maxRetries: num(provider?.maxRetries),
    concurrency: num(provider?.concurrency),
    contextWindow: num(provider?.contextWindow),
    modelGuidance: provider?.modelGuidance ?? '',
    project: provider?.project ?? '',
    region: provider?.region ?? '',
    apiKey: '',
    clearKey: false,
  };
}

function optionalInt(v: string): number | undefined {
  const t = v.trim();
  if (!t) return undefined;
  const n = Number(t);
  return Number.isFinite(n) ? Math.trunc(n) : NaN;
}

/**
 * Builds the API body. The credential is only sent when the operator typed
 * one (replace) or asked to remove it; otherwise the stored one is kept.
 * Project and region are sent only for types whose settings include them,
 * so switching the type does not carry them over.
 */
export function inputFromDraft(d: ProviderDraft, mode: 'create' | 'edit', settings: string[] = []): ProviderInput {
  const input: ProviderInput = {
    type: d.type,
    baseUrl: d.baseUrl.trim() || undefined,
    defaultModel: d.defaultModel.trim() || undefined,
    timeoutSec: optionalInt(d.timeoutSec),
    maxRetries: optionalInt(d.maxRetries),
    concurrency: optionalInt(d.concurrency),
    contextWindow: optionalInt(d.contextWindow),
    modelGuidance: d.modelGuidance || undefined,
    project: (settings.includes('project') && d.project.trim()) || undefined,
    region: (settings.includes('region') && d.region.trim()) || undefined,
  };
  input.name = d.name.trim();
  if (mode === 'create' && !input.name) delete input.name;
  if (d.clearKey) input.apiKey = { clear: true };
  else if (d.apiKey.trim()) input.apiKey = { value: d.apiKey.trim() };
  return input;
}

/** Client-side checks that mirror the server's, so obvious mistakes are caught before a round-trip. */
export function localErrors(input: ProviderInput, mode: 'create' | 'edit'): Record<string, string> {
  const out: Record<string, string> = {};
  if (mode === 'create' && !input.name) out.name = 'Enter a name.';
  if (!input.type) out.type = 'Choose a provider type.';
  for (const k of ['timeoutSec', 'maxRetries', 'concurrency', 'contextWindow'] as const) {
    if (Number.isNaN(input[k])) out[k] = 'Enter a whole number.';
  }
  if (input.baseUrl && !/^https?:\/\/[^/\s]+/i.test(input.baseUrl)) out.baseUrl = 'Use an http:// or https:// URL.';
  for (const k of ['project', 'region'] as const) {
    const v = input[k];
    if (v && !/^[a-z0-9]+(-[a-z0-9]+)*$/.test(v)) out[k] = 'Use lowercase letters, digits and hyphens.';
  }
  return out;
}

/** Field errors from a 422 response ({details: {errors: [{path, message}]}}). */
export function fieldErrors(e: unknown): Record<string, string> {
  if (!(e instanceof ApiError) || e.status !== 422) return {};
  const details = e.details as { errors?: { path: string; message: string }[] } | undefined;
  const out: Record<string, string> = {};
  for (const issue of details?.errors ?? []) {
    out[issue.path] = out[issue.path] ? `${out[issue.path]} ${issue.message}` : issue.message;
  }
  return out;
}

export function credentialLabel(c?: CredentialState): string {
  if (!c || c.source === 'none') return 'No credential';
  if (c.source === 'stored') return c.configured ? 'Stored encrypted in Weft' : 'Not stored';
  if (c.source === 'env') return c.configured ? `From server variable ${c.env}` : `Server variable ${c.env} is empty`;
  return c.configured ? 'From a server key file' : 'Server key file is missing or empty';
}

export interface TestSummary {
  tone: 'ok' | 'warn' | 'bad';
  title: string;
  detail: string;
}

export function describeTest(r: ProviderTestResult): TestSummary {
  const secs = (ms?: number) => (ms === undefined ? '' : ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(1)} s`);
  const models = r.models.ok
    ? `${r.models.count} model${r.models.count === 1 ? '' : 's'} listed${
        r.models.defaultModelListed === false ? '; the default model is not in the list' : ''
      }`
    : `model list unavailable (${r.models.error ?? 'unknown error'})`;
  if (r.ok) {
    const toolsFailed = r.tools?.checked && !r.tools.ok;
    const warn = !r.models.ok || r.models.defaultModelListed === false || toolsFailed;
    const tools = !r.tools?.checked
      ? ''
      : r.tools.ok
        ? ' Tool calling works.'
        : ` Tool calling does not work: ${r.tools.error ?? 'no tool call'}${r.tools.reply ? ` (it answered “${r.tools.reply}”)` : ''}. Agents with tools need another model.`;
    return {
      tone: warn ? 'warn' : 'ok',
      title: toolsFailed ? `Inference works${r.model ? ` with ${r.model}` : ''}, but not tool calling` : `Inference works${r.model ? ` with ${r.model}` : ''}`,
      detail: `Answered in ${secs(r.inference.durationMs)}${r.inference.reply ? ` (“${r.inference.reply}”)` : ''}; ${models}.${tools}`,
    };
  }
  return {
    tone: 'bad',
    title: 'Inference failed',
    detail: `${r.inference.error ?? 'The provider did not answer.'} ${r.models.ok ? `(${models} — listing models alone does not prove inference.)` : ''}`.trim(),
  };
}
