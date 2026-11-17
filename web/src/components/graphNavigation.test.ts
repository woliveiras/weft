import { describe, expect, it } from 'vitest';
import type { GraphSummary } from '../api/types';
import { matchingAgents, matchingGraphs } from './graphNavigation';
describe('graph navigation search', () => {
 it('matches names and IDs without changing draft nodes', () => {
  const nodes = [{id:'worker', name:'Draft reviewer'}, {id:'helper'}];
  expect(matchingAgents(nodes,' REVIEW ')).toEqual([nodes[0]]);
  expect(matchingAgents(nodes,'HELP')).toEqual([nodes[1]]);
  expect(matchingAgents(nodes,'missing')).toEqual([]);
  expect(matchingAgents(nodes,'')).toEqual(nodes);
  expect(nodes[0].name).toBe('Draft reviewer');
 });
 it('matches graph IDs, missing names and empty lists', () => {
  const graphs = [{graph:{metadata:{id:'alpha',name:'Factory'}}},{graph:{metadata:{id:'beta'}}}] as GraphSummary[];
  expect(matchingGraphs(graphs,' factory ')).toEqual([graphs[0]]);
  expect(matchingGraphs(graphs,'BETA')).toEqual([graphs[1]]);
  expect(matchingGraphs([], '')).toEqual([]);
  expect(matchingGraphs(graphs, 'missing')).toEqual([]);
 });
});
