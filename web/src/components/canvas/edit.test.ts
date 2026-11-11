import { describe, expect, it } from 'vitest';
import type { Graph } from '../../api/types';
import { effectiveLoopMode } from './model';
import {
  addAgent,
  applyRemoval,
  connect,
  describeRemoval,
  fieldIssues,
  indexIssues,
  moveAgents,
  planRemoval,
  updateChannel,
  updateNode,
} from './edit';

const graph = (): Graph =>
  ({
    apiVersion: 'weft.io/v1',
    kind: 'Graph',
    'x-editor': { zoom: 2 },
    metadata: { id: 'g', name: 'G', version: 3 },
    spec: {
      nodes: [
        { id: 'dev', name: 'Developer', prompt: 'build', 'x-color': 'blue' },
        { id: 'rev', prompt: 'review' },
        { id: 'qa', prompt: 'test' },
      ],
      channels: [
        { id: 'in', to: ['dev'] },
        { id: 'code', from: 'dev', to: ['rev', 'qa'] },
        { id: 'back', from: 'rev', to: ['dev'] },
        { id: 'news', type: 'topic', from: 'rev', topic: 'news' },
      ],
    },
  }) as Graph;

describe('graph edits', () => {
  it('adds agents with unique ids, valid defaults and pinned positions', () => {
    const g = graph();
    g.spec.nodes.push({ id: 'agent-4' });
    const { graph: next, id } = addAgent(g, { x: 5, y: 6 }, { dev: { x: 1, y: 2 } });
    expect(id).toBe('agent-5');
    const added = next.spec.nodes.find((n) => n.id === id)!;
    expect(added).toMatchObject({ name: 'Agent 5', runtime: { kind: 'native' }, loop: { mode: 'event' }, position: { x: 5, y: 6 } });
    expect(added.prompt).toBeTruthy();
    expect(next.spec.nodes[0].position).toEqual({ x: 1, y: 2 });
    expect(g.spec.nodes).toHaveLength(4); // input untouched
  });

  it('connects agents with a queue channel and ignores ports', () => {
    const { graph: next, id } = connect(graph(), 'qa', 'dev');
    expect(id).toBe('channel-5');
    expect(next.spec.channels!.at(-1)).toEqual({ id: 'channel-5', type: 'queue', from: 'qa', to: ['dev'] });
    const port = connect(graph(), '__in:in', 'dev');
    expect(port.id).toBeUndefined();
    expect(port.graph.spec.channels).toHaveLength(4);
  });

  it('keeps fields it does not edit, including extensions', () => {
    let g = updateNode(graph(), 'dev', { name: 'Dev' });
    g = updateChannel(g, 'code', { approval: true });
    g = moveAgents(g, { rev: { x: 9, y: 9 } });
    expect(g.spec.nodes[0]).toMatchObject({ name: 'Dev', prompt: 'build', 'x-color': 'blue' });
    expect(g.spec.channels![1]).toMatchObject({ approval: true, from: 'dev', to: ['rev', 'qa'] });
    expect((g as unknown as Record<string, unknown>)['x-editor']).toEqual({ zoom: 2 });
  });

  it('removing an agent drops the channels it produces and narrows the ones it consumes', () => {
    const removal = planRemoval(graph(), ['rev']);
    expect(removal).toEqual({ nodes: ['rev'], channels: ['back', 'news'], narrowed: [{ channelId: 'code', targets: ['rev'] }] });
    const next = applyRemoval(graph(), removal);
    expect(next.spec.nodes.map((n) => n.id)).toEqual(['dev', 'qa']);
    expect(next.spec.channels!.map((c) => [c.id, c.to])).toEqual([
      ['in', ['dev']],
      ['code', ['qa']],
    ]);
  });

  it('treats a keyboard delete of an agent and its edges as one removal', () => {
    // React Flow reports the agent plus every connected edge; the multi-target
    // channel must only lose the removed agent, not disappear.
    const removal = planRemoval(
      graph(),
      ['rev'],
      [
        { channelId: 'code', target: 'rev' },
        { channelId: 'back', target: 'dev' },
        { channelId: 'news', target: '__nosubs:news' },
      ],
    );
    expect(removal.channels.sort()).toEqual(['back', 'news']);
    expect(removal.narrowed).toEqual([{ channelId: 'code', targets: ['rev'] }]);
  });

  it('removing edges narrows or removes channels; listed channels go entirely', () => {
    expect(planRemoval(graph(), [], [{ channelId: 'code', target: 'qa' }])).toEqual({
      nodes: [],
      channels: [],
      narrowed: [{ channelId: 'code', targets: ['qa'] }],
    });
    expect(planRemoval(graph(), [], [{ channelId: 'in', target: 'dev' }]).channels).toEqual(['in']);
    // A subscription edge has no explicit target to drop.
    expect(planRemoval(graph(), [], [{ channelId: 'news', target: 'auditor' }]).channels).toEqual(['news']);
    expect(planRemoval(graph(), [], [], ['code']).channels).toEqual(['code']);
  });

  it('describes removals for confirmation', () => {
    const g = graph();
    expect(describeRemoval(g, planRemoval(g, ['rev']))).toBe(
      'Remove agent "rev" and channels "back", "news"?\n"code" will stop delivering to "rev"',
    );
    expect(describeRemoval(g, planRemoval(g, [], [{ channelId: 'code', target: 'qa' }]))).toBe(
      'Change channel targets?\n"code" will stop delivering to "qa"',
    );
    expect(describeRemoval(g, planRemoval(g, ['dev', 'qa'], [], []))).toMatch(/^Remove agents "Developer", "qa" and channels/);
    expect(describeRemoval(g, planRemoval(g, ['ghost']))).toBeUndefined();
  });
});

describe('validation issues', () => {
  it('maps paths to the agents and channels of the validated draft', () => {
    const index = indexIssues(
      graph(),
      [
        { path: 'spec.nodes[1].prompt', message: 'native agents need a prompt' },
        { path: 'spec.nodes[1].loop.schedule', message: 'bad schedule' },
        { path: 'spec.channels[1].to[0]', message: 'unknown node "ghost"' },
        { path: 'spec.nodes[1]', message: 'whole node' },
        { path: 'metadata.id', message: 'bad id' },
        { path: 'spec.nodes[9].prompt', message: 'stale index' },
      ],
      [{ path: 'spec.channels[3]', message: 'no consumers' }],
    );
    expect(index.errorCount).toBe(6);
    expect(index.nodes.get('rev')!.map((i) => i.field)).toEqual(['prompt', 'loop.schedule', '']);
    expect(index.channels.get('code')).toEqual([
      { path: 'spec.channels[1].to[0]', message: 'unknown node "ghost"', severity: 'error', field: 'to[0]' },
    ]);
    expect(index.channels.get('news')![0].severity).toBe('warning');
    expect(index.graph.map((i) => i.path)).toEqual(['metadata.id', 'spec.nodes[9].prompt']);
  });

  it('selects field issues by prefix', () => {
    const issues = indexIssues(graph(), [
      { path: 'spec.nodes[0].loop.schedule', message: 'a' },
      { path: 'spec.nodes[0].loop', message: 'b' },
      { path: 'spec.nodes[0].loopy', message: 'c' },
      { path: 'spec.nodes[0].tools[2]', message: 'd' },
    ]).nodes.get('dev');
    expect(fieldIssues(issues, 'loop').map((i) => i.message)).toEqual(['a', 'b']);
    expect(fieldIssues(issues, 'tools').map((i) => i.message)).toEqual(['d']);
    expect(fieldIssues(undefined, 'loop')).toEqual([]);
  });
});

describe('effective loop mode', () => {
  it('applies the node, the graph default, then inputs like the server', () => {
    const g = graph();
    expect(effectiveLoopMode(g, g.spec.nodes[0])).toBe('event'); // fed by "in" and "back"
    expect(effectiveLoopMode(g, { id: 'lonely' })).toBe('once');
    expect(effectiveLoopMode(g, { id: 'x', loop: { mode: 'scheduled' } })).toBe('scheduled');
    const withDefault = { ...g, spec: { ...g.spec, defaults: { loop: { mode: 'continuous' as const } } } };
    expect(effectiveLoopMode(withDefault, { id: 'lonely' })).toBe('continuous');
  });
});
