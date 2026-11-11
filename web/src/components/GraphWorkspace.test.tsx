import type { AgentSection } from './agentSettingsModel';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Activity, useState } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Graph, GraphSummary, Info, ValidationResult } from '../api/types';
import { EventStreamProvider } from '../hooks/useEventStream';
import { apiError, fakeApi, type Call } from '../test/fakeApi';
import { GraphWorkspace, type GraphTab } from './GraphWorkspace';

class FakeEventSource {
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  constructor(public url: string) {}
  addEventListener() {}
  close() {}
}

class FakeResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const initial = (): GraphSummary => ({
  graph: {
    apiVersion: 'weft.io/v1',
    kind: 'Graph',
    'x-editor': { zoom: 2 },
    metadata: { id: 'g', name: 'G', version: 1 },
    spec: {
      nodes: [
        { id: 'dev', name: 'Developer', prompt: 'build', runtime: { kind: 'native' }, loop: { mode: 'event' }, 'x-color': 'blue' },
        { id: 'rev', name: 'Reviewer', prompt: 'review', runtime: { kind: 'native' }, loop: { mode: 'event' } },
      ],
      channels: [
        { id: 'in', type: 'queue', to: ['dev'] },
        { id: 'code', type: 'queue', from: 'dev', to: ['rev'] },
      ],
    },
  } as Graph,
  record: { id: 'g', version: 1, state: 'stopped', desiredState: 'stopped', updatedAt: '2026-10-14T10:00:00Z' },
});

const valid: ValidationResult = { valid: true, errors: [], warnings: [] };

/** Validation that mirrors the server's prompt rule, with real field paths. */
function promptRule(graph: Graph): ValidationResult {
  const errors = graph.spec.nodes.flatMap((node, i) =>
    node.prompt?.trim() ? [] : [{ path: `spec.nodes[${i}].prompt`, message: 'native agents need a prompt' }],
  );
  return { valid: errors.length === 0, errors, warnings: [] };
}

/** A stateful fake control plane: versioned graph with If-Match checks. */
function backend(validate: (graph: Graph) => ValidationResult = promptRule) {
  const state = { current: initial(), environment: 'absent' as 'absent' | 'building' | 'ready' };
  const perNode = Object.fromEntries(
    ['dev', 'rev', 'agent-3'].flatMap((id) => [
      [`GET /api/v1/graphs/g/nodes/${id}/chat`, () => []],
      [`POST /api/v1/graphs/g/nodes/${id}/start`, () => status()],
    ]),
  );
  const status = () => ({ graphId: 'g', state: 'stopped', desiredState: 'stopped', nodes: [], channels: [] });
  const api = fakeApi({
    ...perNode,
    'GET /api/v1/graphs/g': () => state.current,
    'GET /api/v1/graphs/g/status': status,
    'GET /api/v1/graphs/g/events': () => [],
    'GET /api/v1/graphs/g/executions': () => [],
    'GET /api/v1/graphs/g/messages': () => [],
    'GET /api/v1/graphs/g/versions': () => [{ version: state.current.record.version, createdAt: '2026-10-14T10:00:00Z' }],
    'GET /api/v1/tools': () => [
      { name: 'filesystem.read', capability: 'filesystem.read', description: 'Read files' },
      { name: 'shell.exec', capability: 'shell.exec', description: 'Run commands' },
    ],
    'GET /api/v1/providers': () => [],
    'GET /api/v1/graphs/g/environment': () => ({
      graph: 'g',
      state: state.environment,
      defined: !!state.current.graph.spec.environment,
      upToDate: state.environment === 'ready',
      currentHash: 'h1',
    }),
    'POST /api/v1/graphs/g/environment/build': () => {
      state.environment = 'ready';
      return { graph: 'g', state: 'building', defined: true, upToDate: false, currentHash: 'h1' };
    },
    'POST /api/v1/graphs/g/start': status,
    'POST /api/v1/graphs/validate': (call: Call) => validate(call.body as Graph),
    'PUT /api/v1/graphs/g': (call: Call) => {
      const version = state.current.record.version;
      if (call.headers['If-Match'] !== String(version)) {
        return apiError(409, `conflict: graph was modified (expected version ${call.headers['If-Match']})`, 'conflict');
      }
      const graph = call.body as Graph;
      state.current = {
        graph: { ...graph, metadata: { ...graph.metadata, version: version + 1 } },
        record: { ...state.current.record, version: version + 1 },
      };
      return state.current;
    },
  });
  return { api, state, puts: () => api.find('PUT', '/api/v1/graphs/g') };
}

function Harness({ start = 'canvas' as GraphTab, role = 'admin' as 'viewer' | 'operator' | 'admin' }) {
  const [summary, setSummary] = useState(initial);
  const [tab, setTab] = useState<GraphTab>(start);
  const [agentId, setAgentId] = useState('dev');
  const [section, setSection] = useState<AgentSection>('general');
  return (
    <EventStreamProvider token="t">
      <GraphWorkspace
        summary={summary}
        info={{ identity: { name: role, role } } as Info}
        tab={tab}
        agentId={tab === 'agent' ? agentId : undefined}
        agentSection={section}
        onAgent={(id, section) => {setAgentId(id); setSection(section); setTab('agent');}}
        onTab={setTab}
        onSaved={setSummary}
        onCorrelation={() => undefined}
      />
    </EventStreamProvider>
  );
}

function renderWorkspace(selected?: string) {
  location.hash = selected ? `#/g/g/canvas?sel=node:${selected}` : '#/g/g/canvas';
  return render(<Harness />);
}

const agentEditor = () => screen.getByRole('region', { name: 'Agent configuration' });
const toolbarButton = (name: string) => screen.getByRole('button', { name });

beforeEach(() => {
  vi.stubGlobal('EventSource', FakeEventSource);
  vi.stubGlobal('ResizeObserver', FakeResizeObserver);
  // jsdom has no transform matrix API; canvas measurements use unit zoom here.
  vi.stubGlobal('DOMMatrixReadOnly', class { m22 = 1; });
  vi.stubGlobal('confirm', vi.fn(() => true));
});
afterEach(() => {
  cleanup();
  location.hash = '';
});

describe('GraphWorkspace editor', () => {
  it('adds an agent, edits it and saves the draft with the version it was based on', async () => {
    const user = userEvent.setup();
    const { puts } = backend();
    renderWorkspace();

    await user.click(await screen.findByRole('button', { name: 'Add agent' }));
    // The new agent is selected so it can be configured right away.
    const name = within(agentEditor()).getByLabelText('Name') as HTMLInputElement;
    expect(name.value).toBe('Agent 3');
    await user.clear(name);
    await user.type(name, 'Tester');
    await user.selectOptions(within(agentEditor()).getByLabelText('Stop policy'), 'iterations');
    await user.type(within(agentEditor()).getByLabelText('Max turns'), '3');
    await user.click(within(agentEditor()).getByLabelText('Approve each turn before it starts'));
    expect(screen.getByText('unsaved graph changes')).toBeTruthy();

    await user.click(toolbarButton('Save graph'));
    await waitFor(() => expect(puts()).toHaveLength(1));
    const put = puts()[0];
    expect(put.headers['If-Match']).toBe('1');
    const body = put.body as Graph & Record<string, unknown>;
    expect(body['x-editor']).toEqual({ zoom: 2 });
    expect(body.spec.nodes[0]).toMatchObject({ id: 'dev', 'x-color': 'blue' });
    expect(body.spec.nodes[2]).toMatchObject({
      id: 'agent-3',
      name: 'Tester',
      runtime: { kind: 'native' },
      loop: { mode: 'event', stop: { policy: 'iterations', maxIterations: 3 } },
      approval: { beforeTurn: true },
    });
    await waitFor(() => expect(screen.queryByText('unsaved graph changes')).toBeNull());
    expect(screen.getByText('v2')).toBeTruthy();
  });

  it('shows validation errors next to the agent and field, and clears them once fixed', async () => {
    const user = userEvent.setup();
    const { puts } = backend();
    renderWorkspace('dev');

    const prompt = await screen.findByRole('heading', { name: 'Prompt' });
    const section = prompt.closest('section')!;
    await user.click(within(section).getByRole('button', { name: 'Edit' }));
    await user.clear(within(section).getByRole('textbox'));
    await user.click(within(section).getByRole('button', { name: 'Apply to draft' }));
    await user.click(toolbarButton('Save graph'));

    const summary = await screen.findByRole('list', { name: 'Graph validation' });
    expect(within(summary).getByText(/1 error must be fixed/)).toBeTruthy();
    expect(within(summary).getByText('prompt: native agents need a prompt')).toBeTruthy();
    expect(within(section).getByText('native agents need a prompt')).toBeTruthy();
    expect(screen.getByLabelText('1 validation errors')).toBeTruthy(); // marker on the canvas agent
    expect(puts()).toHaveLength(0);

    // Fixing the field revalidates without another save attempt.
    await user.click(within(section).getByRole('button', { name: 'Edit' }));
    await user.type(within(section).getByRole('textbox'), 'build it');
    await user.click(within(section).getByRole('button', { name: 'Apply to draft' }));
    await waitFor(() => expect(screen.queryByRole('list', { name: 'Graph validation' })).toBeNull(), { timeout: 2000 });

    await user.click(toolbarButton('Save graph'));
    await waitFor(() => expect(puts()).toHaveLength(1));
    expect((puts()[0].body as Graph).spec.nodes[0].prompt).toBe('build it');
  });

  it('shows errors the server reports on save against their element', async () => {
    const user = userEvent.setup();
    const b = backend(() => valid);
    renderWorkspace('rev');
    await user.type(within(await screen.findByRole('region', { name: 'Agent configuration' })).getByLabelText('Name'), '!');
    const route = b.api.fetch.getMockImplementation()!;
    b.api.fetch.mockImplementation(async (input, init) =>
      init?.method === 'PUT'
        ? apiError(422, 'invalid graph', 'invalid', {
            valid: false,
            errors: [{ path: 'spec.nodes[1].colour', message: 'unknown field "colour"' }],
            warnings: [],
          })
        : route(input, init),
    );
    await user.click(toolbarButton('Save graph'));
    expect(await screen.findByText('colour: unknown field "colour"')).toBeTruthy();
    expect(within(screen.getByRole('list', { name: 'Agent validation' })).getByText('unknown field "colour"')).toBeTruthy();
    expect(screen.getByText('unsaved graph changes')).toBeTruthy();
  });

  it('keeps local edits on a version conflict and lets the user overwrite or reload', async () => {
    const user = userEvent.setup();
    const { state, puts } = backend(() => valid);
    renderWorkspace('dev');
    const name = within(await screen.findByRole('region', { name: 'Agent configuration' })).getByLabelText('Name');
    await user.clear(name);
    await user.type(name, 'Mine');

    // Someone else saves version 5 meanwhile.
    state.current = { ...state.current, record: { ...state.current.record, version: 5 } };
    state.current.graph = { ...state.current.graph, spec: { ...state.current.graph.spec, nodes: [{ ...state.current.graph.spec.nodes[0], name: 'Theirs' }, state.current.graph.spec.nodes[1]] } };

    await user.click(toolbarButton('Save graph'));
    expect(await screen.findByText(/Someone saved a newer version/)).toBeTruthy();
    expect(screen.getByText('unsaved graph changes')).toBeTruthy();
    expect((within(agentEditor()).getByLabelText('Name') as HTMLInputElement).value).toBe('Mine');

    await user.click(toolbarButton('Keep mine and overwrite'));
    await waitFor(() => expect(puts()).toHaveLength(2));
    expect(puts()[1].headers['If-Match']).toBe('5');
    expect((puts()[1].body as Graph).spec.nodes[0].name).toBe('Mine');
    await waitFor(() => expect(screen.queryByText(/Someone saved a newer version/)).toBeNull());

    // Reload path: discard local edits in favour of the latest version.
    await user.clear(within(agentEditor()).getByLabelText('Name'));
    await user.type(within(agentEditor()).getByLabelText('Name'), 'Again');
    state.current = { ...state.current, record: { ...state.current.record, version: 9 } };
    await user.click(toolbarButton('Save graph'));
    await user.click(await screen.findByRole('button', { name: 'Reload latest' }));
    await waitFor(() => expect((within(agentEditor()).getByLabelText('Name') as HTMLInputElement).value).toBe('Mine'));
    expect(screen.queryByText('unsaved graph changes')).toBeNull();
  });

  it('confirms what removing an agent does before changing the draft', async () => {
    const user = userEvent.setup();
    const confirmSpy = vi.fn(() => false);
    vi.stubGlobal('confirm', confirmSpy);
    const { puts } = backend(() => valid);
    renderWorkspace('dev');
    await screen.findByRole('region', { name: 'Agent configuration' });

    await user.click(toolbarButton('Remove agent'));
    expect(confirmSpy).toHaveBeenCalledWith('Remove agent "Developer" and channels "in", "code"?');
    expect(screen.queryByText('unsaved graph changes')).toBeNull();

    confirmSpy.mockReturnValue(true);
    await user.click(toolbarButton('Remove agent'));
    expect(screen.getByText('unsaved graph changes')).toBeTruthy();
    await user.click(toolbarButton('Save graph'));
    await waitFor(() => expect(puts()).toHaveLength(1));
    const body = puts()[0].body as Graph;
    expect(body.spec.nodes.map((n) => n.id)).toEqual(['rev']);
    expect(body.spec.channels).toEqual([]);
  });

  it('discards the draft back to the saved version', async () => {
    const user = userEvent.setup();
    backend(() => valid);
    renderWorkspace('dev');
    const name = within(await screen.findByRole('region', { name: 'Agent configuration' })).getByLabelText('Name');
    await user.type(name, ' changed');
    await user.click(toolbarButton('Discard'));
    expect((within(agentEditor()).getByLabelText('Name') as HTMLInputElement).value).toBe('Developer');
    expect(screen.queryByText('unsaved graph changes')).toBeNull();
  });

  it('configures the command sandbox and warns about shell access under process isolation', async () => {
    const user = userEvent.setup();
    const { puts } = backend(() => valid);
    renderWorkspace('dev');
    const editor = await screen.findByRole('region', { name: 'Agent configuration' });

    // Granting shell under process isolation explains the risk.
    expect(within(editor).queryByText(/can run shell commands/)).toBeNull();
    await user.click(await screen.findByRole('checkbox', { name: /shell\.exec/ }));
    expect(within(editor).getByText(/can run shell commands/)).toBeTruthy();

    await user.selectOptions(within(editor).getByLabelText('Isolation'), 'docker');
    expect(within(editor).queryByText(/can run shell commands/)).toBeNull();
    await user.selectOptions(within(editor).getByLabelText('Network'), 'bridge');
    await user.type(within(editor).getByLabelText('Memory limit'), '2g');

    await user.click(toolbarButton('Save graph'));
    await waitFor(() => expect(puts()).toHaveLength(1));
    const dev = (puts()[0].body as Graph).spec.nodes[0];
    expect(dev.isolation).toEqual({ mode: 'docker', network: 'bridge', memory: '2g' });
    expect(dev.tools).toEqual([{ name: 'shell.exec' }]);
  });

  it('edits per-turn limits for long tasks', async () => {
    const user = userEvent.setup();
    const { puts } = backend(() => valid);
    renderWorkspace('dev');
    const editor = await screen.findByRole('region', { name: 'Agent configuration' });
    await user.type(within(editor).getByLabelText('Turn timeout'), '2h');
    await user.type(within(editor).getByLabelText('Max tool calls'), '300');
    await user.click(toolbarButton('Save graph'));
    await waitFor(() => expect(puts()).toHaveLength(1));
    expect((puts()[0].body as Graph).spec.nodes[0].limits).toEqual({ turnTimeout: '2h', maxToolCalls: 300 });
  });

  it('edits graph settings: repository and parameters, with graph-level errors linked to the panel', async () => {
    const user = userEvent.setup();
    const { puts } = backend((graph) =>
      graph.spec.workspace?.repo?.startsWith('/')
        ? {
            valid: false,
            errors: [{ path: 'spec.workspace.repo', message: 'is not a remote repository URL' }],
            warnings: [],
          }
        : valid,
    );
    renderWorkspace();
    await user.click(await screen.findByRole('button', { name: 'Graph settings' }));
    const settings = screen.getByRole('region', { name: 'Graph configuration' });

    const repo = within(settings).getByLabelText('Repository URL');
    await user.type(repo, '/var/lib/weft/workspaces/other');
    await user.click(toolbarButton('Save graph'));
    const summary = await screen.findByRole('list', { name: 'Graph validation' });
    expect(within(settings).getByText('is not a remote repository URL')).toBeTruthy();
    expect(puts()).toHaveLength(0);

    // The summary links graph-level problems back to the settings panel.
    await user.click(within(summary).getByRole('button', { name: 'Graph settings' }));
    await user.clear(within(screen.getByRole('region', { name: 'Graph configuration' })).getByLabelText('Repository URL'));
    await user.type(
      within(screen.getByRole('region', { name: 'Graph configuration' })).getByLabelText('Repository URL'),
      'https://github.com/woliveiras/bookaneer.git',
    );
    await user.click(screen.getByRole('button', { name: 'Add parameter' }));
    const parameter = screen.getByRole('group', { name: 'Parameter 1' });
    await user.type(within(parameter).getByLabelText('Name'), 'branch');
    await user.type(within(parameter).getByLabelText('Value'), 'deps/update');

    await user.click(toolbarButton('Save graph'));
    await waitFor(() => expect(puts()).toHaveLength(1));
    const body = puts()[0].body as Graph;
    expect(body.spec.workspace).toEqual({ repo: 'https://github.com/woliveiras/bookaneer.git' });
    expect(body.spec.parameters).toEqual([{ name: 'branch', value: 'deps/update' }]);
  });

  it('defines a project environment in graph settings, saves it and builds it', async () => {
    const user = userEvent.setup();
    const { puts, api } = backend(() => valid);
    renderWorkspace();
    await user.click(await screen.findByRole('button', { name: 'Graph settings' }));
    const env = screen.getByRole('group', { name: 'Environment' });
    await user.selectOptions(within(env).getByLabelText('Source'), 'image');
    await user.type(within(env).getByLabelText('Base image'), 'golang:1.26');
    await user.type(within(env).getByLabelText('Setup commands (one per line)'), 'go version{Enter}go mod download');
    await user.type(within(env).getByLabelText('Cache directories (one per line)'), '/home/sandbox/go/pkg/mod');
    await user.selectOptions(within(env).getByLabelText('Network for agents'), 'bridge');
    const status = within(env).getByRole('region', { name: 'Environment status' });
    expect(within(status).getByText(/Unsaved environment changes/)).toBeTruthy();

    await user.click(toolbarButton('Save graph'));
    await waitFor(() => expect(puts()).toHaveLength(1));
    expect((puts()[0].body as Graph).spec.environment).toEqual({
      image: 'golang:1.26',
      setup: ['go version', 'go mod download'],
      caches: ['/home/sandbox/go/pkg/mod'],
      network: { run: 'bridge' },
    });

    const build = await within(status).findByRole('button', { name: 'Build' });
    await waitFor(() => expect((build as HTMLButtonElement).disabled).toBe(false));
    await user.click(build);
    await waitFor(() => expect(api.find('POST', '/api/v1/graphs/g/environment/build')).toHaveLength(1));
    // The status follows the build until it is ready.
    expect(await within(status).findByText('Ready', {}, { timeout: 4000 })).toBeTruthy();
  });

  it('offers to build the environment when a start is refused for it', async () => {
    const user = userEvent.setup();
    const { api } = backend(() => valid);
    api.fetch.mockImplementation(
      ((route) => async (input: RequestInfo | URL, init?: RequestInit) =>
        String(input).endsWith('/api/v1/graphs/g/start')
          ? apiError(422, 'invalid graph', 'invalid', {
              valid: false,
              errors: [{ path: 'spec.environment', message: 'environment not ready: it has not been built yet (needed by dev)' }],
              warnings: [],
            })
          : route(input, init))(api.fetch.getMockImplementation()!),
    );
    renderWorkspace();
    await user.click(await screen.findByRole('button', { name: /Run all/ }));
    const box = await screen.findByText(/The graph cannot start yet/);
    expect(box.parentElement?.textContent).toContain('(needed by dev)');
    expect(screen.getByRole('button', { name: 'Build and start when ready' })).toBeTruthy();
    await user.click(screen.getByRole('button', { name: 'Dismiss' }));
    expect(screen.queryByText(/The graph cannot start yet/)).toBeNull();
  });

  it('asks before starting while graph edits are unsaved', async () => {
    const user = userEvent.setup();
    const confirmSpy = vi.fn(() => false);
    vi.stubGlobal('confirm', confirmSpy);
    const { api } = backend(() => valid);
    renderWorkspace('dev');
    await user.type(within(await screen.findByRole('region', { name: 'Agent configuration' })).getByLabelText('Name'), 'x');
    await user.click(screen.getByRole('button', { name: /Run all/ }));
    expect(confirmSpy).toHaveBeenCalledWith(expect.stringContaining('unsaved graph changes are not included'));
    expect(api.find('POST', '/api/v1/graphs/g/start')).toHaveLength(0);
  });
});

describe('GraphWorkspace YAML view', () => {
  it('edits the same draft as the canvas, in both directions', async () => {
    const user = userEvent.setup();
    const { puts } = backend(() => valid);
    renderWorkspace('dev');
    const name = within(await screen.findByRole('region', { name: 'Agent configuration' })).getByLabelText('Name');
    await user.clear(name);
    await user.type(name, 'From canvas');

    await user.click(screen.getByRole('tab', { name: 'Definition' }));
    const yaml = (await screen.findByRole('textbox', { name: 'Graph YAML' })) as HTMLTextAreaElement;
    expect(yaml.value).toContain('name: From canvas');
    expect(yaml.value).toContain('x-color: blue');

    fireEvent.change(yaml, { target: { value: yaml.value.replace('name: From canvas', 'name: From YAML') } });
    // Unparseable text keeps the last valid draft.
    fireEvent.change(yaml, { target: { value: `${yaml.value}\n  bad: [` } });
    expect(screen.getByText(/The graph view and Save use the last valid YAML/)).toBeTruthy();

    await user.click(screen.getByRole('tab', { name: 'Graph' }));
    expect((within(agentEditor()).getByLabelText('Name') as HTMLInputElement).value).toBe('From YAML');

    await user.click(toolbarButton('Save graph'));
    await waitFor(() => expect(puts()).toHaveLength(1));
    const body = puts()[0].body as Graph & Record<string, unknown>;
    expect(body.spec.nodes[0].name).toBe('From YAML');
    expect(body['x-editor']).toEqual({ zoom: 2 });
  });
});

describe('dedicated agent configuration',()=>{
 it('preserves edits across sections and agents and reviews the entire graph before a versioned save',async()=>{
  const {puts}=backend();const user=userEvent.setup();render(<Harness start="agent"/>);
  await user.type(within(await screen.findByRole('region',{name:'Agent configuration'})).getByLabelText('Name'),' updated');
  const sections=screen.getByRole('navigation',{name:'Agent configuration sections'});
  await user.click(within(sections).getByRole('button',{name:'Instructions'}));
  await user.clear(screen.getByLabelText('Agent instructions'));await user.type(screen.getByLabelText('Agent instructions'),'New instructions');
  await user.selectOptions(screen.getByLabelText('Agent',{exact:true}),'rev');
  await user.type(within(screen.getByRole('region',{name:'Agent configuration'})).getByLabelText('Name'),' updated');
  await user.click(toolbarButton('Review & save'));
  const review=screen.getByRole('dialog',{name:'Review graph changes'});
  expect(within(review).getByText('spec.nodes[0].name')).toBeTruthy();expect(within(review).getByText('spec.nodes[0].prompt')).toBeTruthy();expect(within(review).getByText('spec.nodes[1].name')).toBeTruthy();expect(puts()).toHaveLength(0);
  await user.click(within(review).getByRole('button',{name:'Continue editing'}));expect(puts()).toHaveLength(0);
  await user.click(toolbarButton('Review & save'));await user.click(screen.getByRole('button',{name:'Save graph draft'}));
  await waitFor(()=>expect(puts()).toHaveLength(1));
  expect(puts()[0].headers['If-Match']).toBe('1');
  const body=puts()[0].body as Graph;expect(body.spec.nodes[0]).toMatchObject({name:'Developer updated',prompt:'New instructions','x-color':'blue'});expect(body.spec.nodes[1].name).toBe('Reviewer updated');expect(body.spec.channels).toEqual(initial().graph.spec.channels);
 });
 it('keeps invalid instructions editable and prevents persistence after validation failure',async()=>{
  const {puts}=backend();const user=userEvent.setup();render(<Harness start="agent"/>);
  await user.click(within(screen.getByRole('navigation',{name:'Agent configuration sections'})).getByRole('button',{name:'Instructions'}));
  await user.clear(screen.getByLabelText('Agent instructions'));await user.click(toolbarButton('Review & save'));await user.click(screen.getByRole('button',{name:'Save graph draft'}));
  await waitFor(()=>expect(screen.getAllByText('native agents need a prompt').length).toBeGreaterThan(0));expect(puts()).toHaveLength(0);expect(screen.getByLabelText('Agent instructions')).toHaveProperty('value','');
 });
 it('keeps the draft after a revision conflict',async()=>{
  const {state,puts}=backend();const user=userEvent.setup();render(<Harness start="agent"/>);
  await user.type(within(await screen.findByRole('region',{name:'Agent configuration'})).getByLabelText('Name'),' local');
  state.current={...state.current,record:{...state.current.record,version:2}};
  await user.click(toolbarButton('Review & save'));await user.click(screen.getByRole('button',{name:'Save graph draft'}));
  expect(await screen.findByText(/Someone saved a newer version/)).toBeTruthy();expect(puts()).toHaveLength(1);expect(within(screen.getByRole('region',{name:'Agent configuration'})).getByLabelText('Name')).toHaveProperty('value','Developer local');
 });
 it('shows viewers a read-only configuration without operational controls',async()=>{
  const {puts}=backend();const user=userEvent.setup();render(<Harness start="agent" role="viewer"/>);
  const name=within(await screen.findByRole('region',{name:'Agent configuration'})).getByLabelText('Name');
  await user.type(name,' forbidden');expect(name).toHaveProperty('value','Developer');expect(screen.queryByRole('button',{name:'Run all'})).toBeNull();
  expect(toolbarButton('Review & save').hasAttribute('disabled')).toBe(true);expect(puts()).toHaveLength(0);
 });
});
it('preserves agent edits when validation is unavailable',async()=>{
 const {puts}=backend(()=>{throw new Error('Validation service unavailable');});const user=userEvent.setup();render(<Harness start="agent"/>);
 await user.type(within(await screen.findByRole('region',{name:'Agent configuration'})).getByLabelText('Name'),' retained');
 await user.click(toolbarButton('Review & save'));await user.click(screen.getByRole('button',{name:'Save graph draft'}));
 expect(await screen.findByText('network error: Validation service unavailable')).toBeTruthy();expect(puts()).toHaveLength(0);
 expect(within(screen.getByRole('region',{name:'Agent configuration'})).getByLabelText('Name')).toHaveProperty('value','Developer retained');
});
it('keeps graph-level changes in the review when opening an agent',async()=>{
 backend();const user=userEvent.setup();renderWorkspace();
 await user.click(toolbarButton('Graph settings'));
 const settings=screen.getByRole('complementary',{name:'Graph settings'});
 await user.type(within(settings).getByLabelText('Name'),' edited');
 await user.click(screen.getByRole('tab',{name:'Agent configuration'}));
 await user.selectOptions(screen.getByLabelText('Agent',{exact:true}),'dev');
 await user.click(toolbarButton('Review & save'));
 expect(within(screen.getByRole('dialog',{name:'Review graph changes'})).getByText('metadata.name')).toBeTruthy();
});
it('returns to the canvas without inventing a selection when no agent is configured',async()=>{
 backend();const user=userEvent.setup();renderWorkspace();
 await user.click(screen.getByRole('tab',{name:'Agent configuration'}));
 expect(screen.getByText('Select an agent from this graph. The requested agent may have been removed.')).toBeTruthy();
 await user.click(screen.getByRole('tab',{name:'Graph'}));
 expect(screen.queryByRole('complementary',{name:'Details'})).toBeNull();
});

it('retains the draft base revision when refreshed metadata arrives before save', async () => {
 const {state,puts}=backend();
 const props={info:{identity:{name:'admin',role:'admin'}} as Info,tab:'agent' as const,agentId:'dev',onTab:vi.fn(),onSaved:vi.fn(),onCorrelation:vi.fn()};
 const view=render(<EventStreamProvider token="t"><GraphWorkspace {...props} summary={initial()}/></EventStreamProvider>);
 fireEvent.change(within(agentEditor()).getByLabelText('Name'),{target:{value:'Local draft'}});
 state.current={...initial(),record:{...initial().record,version:2}};
 view.rerender(<EventStreamProvider token="t"><GraphWorkspace {...props} summary={state.current}/></EventStreamProvider>);
 fireEvent.click(screen.getByRole('button',{name:'Review & save'}));
 fireEvent.click(screen.getByRole('button',{name:'Save graph draft'}));
 await waitFor(()=>expect(puts()).toHaveLength(1));
 expect(puts()[0].headers['If-Match']).toBe('1');
 expect(await screen.findByText(/conflict: graph was modified/)).toBeTruthy();
 expect(within(agentEditor()).getByLabelText('Name')).toHaveProperty('value','Local draft');
});


it('preserves locally selected agents when a cached workspace becomes visible again', async () => {
 backend();
 const props={summary:initial(),tab:'canvas' as const,selectionKey:'node:dev',onTab:vi.fn(),onSaved:vi.fn(),onCorrelation:vi.fn()};
 const content=(visible:boolean)=><EventStreamProvider token="t"><Activity mode={visible?'visible':'hidden'}><GraphWorkspace {...props}/></Activity></EventStreamProvider>;
 const view=render(content(true));
 const agents=()=>within(screen.getByRole('complementary',{name:'Agents'}));
 fireEvent.click(agents().getByRole('button',{name:/^Reviewer.*rev/}));
 expect(agents().getByRole('button',{name:/^Reviewer.*rev/}).getAttribute('aria-current')).toBe('true');
 view.rerender(content(false));view.rerender(content(true));
 await waitFor(()=>expect(agents().getByRole('button',{name:/^Reviewer.*rev/}).getAttribute('aria-current')).toBe('true'));
});
