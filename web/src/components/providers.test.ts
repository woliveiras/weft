import { describe, expect, it } from 'vitest';
import { ApiError } from '../api/client';
import type { ProviderTestResult } from '../api/types';
import { credentialLabel, describeTest, draftFrom, fieldErrors, inputFromDraft, localErrors } from './providers';

const types = [
  { type: 'mock', label: 'Mock', keyRequired: false },
  { type: 'openai', label: 'OpenAI', keyRequired: true, defaultBaseUrl: 'https://api.openai.com/v1' },
];

describe('provider form helpers', () => {
  it('starts a new draft on a hosted type with empty secrets', () => {
    const d = draftFrom(undefined, types);
    expect(d).toMatchObject({ type: 'openai', name: '', apiKey: '', clearKey: false });
  });

  it('sends the credential only when typed or cleared', () => {
    const base = { ...draftFrom({ name: 'p', type: 'openai', defaultModel: 'm' }, types) };
    expect(inputFromDraft(base, 'edit').apiKey).toBeUndefined();
    expect(inputFromDraft({ ...base, apiKey: '  sk-1  ' }, 'edit').apiKey).toEqual({ value: 'sk-1' });
    expect(inputFromDraft({ ...base, apiKey: 'sk-1', clearKey: true }, 'edit').apiKey).toEqual({ clear: true });
  });

  it('omits empty optional values and parses numbers', () => {
    const d = { ...draftFrom(undefined, types), name: ' x ', baseUrl: ' ', timeoutSec: '30', concurrency: '' };
    const input = inputFromDraft(d, 'create');
    expect(input).toMatchObject({ name: 'x', timeoutSec: 30 });
    expect(input.baseUrl).toBeUndefined();
    expect(input.concurrency).toBeUndefined();
    expect(inputFromDraft({ ...d, name: '' }, 'create').name).toBeUndefined();
  });

  it('round-trips the context window', () => {
    const d = draftFrom({ name: 'ol', type: 'ollama', contextWindow: 65536 }, types);
    expect(d.contextWindow).toBe('65536');
    expect(inputFromDraft({ ...d, contextWindow: '' }, 'edit').contextWindow).toBeUndefined();
    expect(localErrors(inputFromDraft({ ...d, contextWindow: 'lots' }, 'edit'), 'edit').contextWindow).toBe('Enter a whole number.');
  });

  it('round-trips the model guidance', () => {
    const d = draftFrom({ name: 'ol', type: 'ollama', modelGuidance: 'small' }, types);
    expect(d.modelGuidance).toBe('small');
    expect(inputFromDraft(d, 'edit').modelGuidance).toBe('small');
    expect(inputFromDraft({ ...d, modelGuidance: '' }, 'edit').modelGuidance).toBeUndefined();
    expect(draftFrom(undefined, types).modelGuidance).toBe('');
  });

  it('catches obvious mistakes before a request', () => {
    const errs = localErrors(inputFromDraft({ ...draftFrom(undefined, types), timeoutSec: 'soon', baseUrl: 'ftp://x' }, 'create'), 'create');
    expect(Object.keys(errs).sort()).toEqual(['baseUrl', 'name', 'timeoutSec']);
    expect(localErrors({ type: 'mock', name: 'ok' }, 'create')).toEqual({});
  });

  it('maps 422 details to fields and ignores other errors', () => {
    const e = new ApiError(422, 'invalid', 'invalid', { errors: [{ path: 'apiKey', message: 'required' }, { path: 'apiKey', message: 'again' }] });
    expect(fieldErrors(e)).toEqual({ apiKey: 'required again' });
    expect(fieldErrors(new ApiError(409, 'conflict'))).toEqual({});
    expect(fieldErrors(new Error('x'))).toEqual({});
  });

  it('labels credentials without revealing them', () => {
    expect(credentialLabel({ configured: true, source: 'stored' })).toMatch(/encrypted/);
    expect(credentialLabel({ configured: false, source: 'env', env: 'OPENAI_API_KEY' })).toMatch(/OPENAI_API_KEY is empty/);
    expect(credentialLabel({ configured: false, source: 'none' })).toBe('No credential');
    expect(credentialLabel(undefined)).toBe('No credential');
  });
});

describe('describeTest', () => {
  const base: ProviderTestResult = {
    provider: 'p', ok: true, checkedAt: '2026-01-01T00:00:00Z', durationMs: 10, model: 'm',
    models: { ok: true, count: 3, defaultModelListed: true },
    inference: { ok: true, reply: 'OK', durationMs: 1500 },
  };

  it('reports working inference', () => {
    expect(describeTest(base)).toMatchObject({ tone: 'ok', title: 'Inference works with m' });
    expect(describeTest(base).detail).toContain('1.5 s');
  });

  it('warns when the default model is not listed', () => {
    expect(describeTest({ ...base, models: { ok: true, count: 3, defaultModelListed: false } }).tone).toBe('warn');
  });

  it('reports tool calling when it was checked', () => {
    const works = describeTest({ ...base, tools: { checked: true, ok: true } });
    expect(works).toMatchObject({ tone: 'ok', title: 'Inference works with m' });
    expect(works.detail).toContain('Tool calling works.');
    const textOnly = describeTest({
      ...base,
      tools: { checked: true, ok: false, reply: '{"name": "connection_check"}', error: 'the model answered in text instead of calling the tool' },
    });
    expect(textOnly.tone).toBe('warn');
    expect(textOnly.title).toBe('Inference works with m, but not tool calling');
    expect(textOnly.detail).toContain('Agents with tools need another model');
  });

  it('never treats a model list as a passing test', () => {
    const r = describeTest({ ...base, ok: false, inference: { ok: false, error: 'HTTP 401: [redacted]' } });
    expect(r.tone).toBe('bad');
    expect(r.detail).toContain('HTTP 401');
    expect(r.detail).toContain('does not prove inference');
  });
});
