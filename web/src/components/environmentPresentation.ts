import type { EnvironmentView, Graph, GraphNode } from '../api/types';

export const reasonLabel = (reason: string): string => ({
  installation_policy: 'Definition blocked by installation policy; review the definition before building',
  explicit_rebuild: 'Explicit rebuild requested',
  definition_changed: 'Environment definition or accepted build context changed',
  repository_inputs_changed: 'Repository manifests or lockfiles changed',
  repository_scope_changed: 'Repository, workspace or sandbox daemon scope changed',
  retry_failed: 'Retry after a failed build',
  not_built: 'No completed environment build',
}[reason] ?? `Other reason: ${reason}`);

export function environmentPhase(view: EnvironmentView): string {
  if (view.changeReasons?.includes('installation_policy')) return 'Blocked by installation policy';
  if (view.state === 'failed') return 'Build failed';
  if (view.state === 'ready') return view.upToDate ? 'Ready' : 'Outdated';
  if (view.state === 'absent') return 'Not built';
  return ({
    waiting_capacity: 'Waiting for sandbox capacity',
    waiting_reuse: 'Waiting for a compatible build',
    preparing: 'Preparing environment',
    building_image: 'Building image',
    setup: 'Preparing private caches and running setup',
  }[view.phase ?? ''] ?? 'Building…');
}

export function agentEnvironment(graph: Graph, node: GraphNode): 'graph' | 'image' | 'process' {
  const isolation = { ...graph.spec.defaults?.isolation, ...node.isolation };
  if (isolation.mode !== 'docker') return 'process';
  return !isolation.image && graph.spec.environment ? 'graph' : 'image';
}

export function capacityTransitions(events: import('../api/types').WeftEvent[], nodeId: string) {
  const latest = new Map<number, { replica: number; label: string; time: string; seq: number }>();
  const labels: Record<string, string> = { waiting: 'Waiting for capacity', admitted: 'Capacity granted', released: 'Capacity released', failed: 'Admission failed', cancelled: 'Admission cancelled' };
  for (const event of events) {
    if (event.type !== 'sandbox.capacity' || !event.data || typeof event.data !== 'object') continue;
    const data = event.data as Record<string, unknown>;
    if (data.phase === 'environment' || (event.nodeId ?? data.node) !== nodeId) continue;
    const replica = event.replica ?? data.replica;
    if (typeof replica !== 'number' || !Number.isInteger(replica) || replica < 0 || typeof data.state !== 'string' || !labels[data.state]) continue;
    if ((latest.get(replica)?.seq ?? -1) >= event.seq) continue;
    latest.set(replica, { replica, label: labels[data.state], time: event.time, seq: event.seq });
  }
  return [...latest.values()].sort((a, b) => a.replica - b.replica);
}
