import { describe, expect, it } from 'vitest';
import { listedWorkflows } from './workflowPresentation';
import type { GraphSummary } from '../api/types';
import { matchingAgents } from './graphNavigation';
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
  const graphs = [{graph:{metadata:{id:'alpha',name:'Factory'}},record:{}},{graph:{metadata:{id:'beta'}},record:{}}] as GraphSummary[];
  expect(listedWorkflows(graphs,' factory ')).toEqual([graphs[0]]);
  expect(listedWorkflows(graphs,'BETA')).toEqual([graphs[1]]);
  expect(listedWorkflows([], '')).toEqual([]);
  expect(listedWorkflows(graphs, 'missing')).toEqual([]);
 });
});
