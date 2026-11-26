import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { ModelsPage } from './ModelsPage';
import { apiError, fakeApi } from '../test/fakeApi';
import type { Info } from '../api/types';

const settingsPath = '/api/v1/chat/settings';
const providers = [{name:'local', type:'ollama', default:true, defaultModel:'qwen:7b'}];
const info = (role: 'viewer' | 'admin') => ({ identity: {name: role, role} }) as Info;

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it('saves an explicitly selected Chat model using the loaded revision', async () => {
  const backend = fakeApi({
    [`GET ${settingsPath}`]: () => ({status:'unconfigured', revision:0}),
    'GET /api/v1/providers': () => providers,
    [`PUT ${settingsPath}`]: call => ({status:'configured', revision:1, model:call.body}),
  });
  render(<ModelsPage info={info('admin')} modelSection="chat" />);
  await screen.findByRole('option', {name:'local'});
  const provider = await screen.findByLabelText('Provider');
  const model = screen.getByLabelText('Model ID');
  const save = screen.getByRole('button', {name:'Save Chat model'}) as HTMLButtonElement;
  expect(save.disabled).toBe(true);
  fireEvent.change(provider, {target:{value:'local'}});
  expect(save.disabled).toBe(true);
  fireEvent.change(model, {target:{value:'qwen:7b'}});
  fireEvent.click(save);
  expect(await screen.findByText('Chat model saved.')).toBeTruthy();
  const [write] = backend.find('PUT', settingsPath);
  expect(write.headers['If-Match']).toBe('0');
  expect(write.body).toEqual({provider:'local', name:'qwen:7b'});
  expect(backend.calls.filter(call => call.method === 'POST')).toHaveLength(0);
});

it('shows a removed provider to viewers without exposing change or test controls', async () => {
  const backend = fakeApi({
    [`GET ${settingsPath}`]: () => ({status:'unavailable', revision:2, model:{provider:'retired', name:'model-one'}}),
  });
  render(<ModelsPage info={info('viewer')} modelSection="chat" />);
  expect(await screen.findByText(/retired.*model-one/)).toBeTruthy();
  expect(screen.getByText(/provider is unavailable/i)).toBeTruthy();
  expect(screen.queryByRole('button', {name:'Save Chat model'})).toBeNull();
  expect(backend.calls.map(call => call.path)).toEqual([settingsPath]);
});

it('retains input and reveals a recovery action after a stale write', async () => {
  const backend = fakeApi({
    [`GET ${settingsPath}`]: () => ({status:'configured', revision:1, model:{provider:'local', name:'qwen:7b'}}),
    'GET /api/v1/providers': () => providers,
    [`PUT ${settingsPath}`]: () => apiError(409, 'setting revision changed', 'conflict'),
  });
  render(<ModelsPage info={info('admin')} modelSection="chat" />);
  const model = await screen.findByLabelText('Model ID') as HTMLInputElement;
  fireEvent.change(model, {target:{value:'review-draft'}});
  fireEvent.click(screen.getByRole('button', {name:'Save Chat model'}));
  expect(await screen.findByRole('button', {name:'Reload saved selection'})).toBeTruthy();
  expect(model.value).toBe('review-draft');
  expect(backend.find('PUT', settingsPath)).toHaveLength(1);
});
