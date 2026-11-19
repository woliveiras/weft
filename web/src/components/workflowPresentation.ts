import type { GraphState, GraphSummary } from '../api/types';

export const workflowStates: Record<GraphState, string> = {
  running: 'Running', paused: 'Paused', stopped: 'Stopped', stopping: 'Stopping', failed: 'Failed',
};

export function listedWorkflows(graphs: GraphSummary[], search = '', status = '') {
  const query = search.trim().toLowerCase();
  return graphs.filter(({ graph, record }) =>
    !graph.metadata.id.startsWith('task-run-') && !graph.metadata.labels?.['weft.task'] &&
    (!status || record.state === status) &&
    [graph.metadata.name, graph.metadata.id, graph.metadata.description ?? ''].some(value => value?.toLowerCase().includes(query)),
  ).sort((a, b) =>
    (Date.parse(b.record.updatedAt) || 0) - (Date.parse(a.record.updatedAt) || 0) ||
    a.graph.metadata.id.localeCompare(b.graph.metadata.id),
  );
}
