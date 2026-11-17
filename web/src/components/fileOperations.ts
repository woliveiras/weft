import type { WeftEvent } from '../api/types';

export function fileOperations(events: WeftEvent[], path: string) {
  return events.flatMap(event => {
    if (!['tool.completed', 'workspace.observed'].includes(event.type) || !event.data || typeof event.data !== 'object') return [];
    const data = event.data as Record<string, unknown>;
    const monitored = event.type === 'workspace.observed' && data.source === 'monitor';
    const journalled = event.type === 'workspace.observed' && data.source === 'kernel-journal';
    if (!event.executionId && !monitored && !journalled) return [];
    const process = data.process && typeof data.process === 'object' ? data.process as Record<string, unknown> : {};
    const author = journalled && typeof process.pid === 'number' && process.pid > 0 ? ` · kernel PID ${process.pid}${process.start ? ` · start ${String(process.start)}` : ' · process lifetime unavailable'}` : journalled ? ' · author unknown' : '';
    if (event.type === 'workspace.observed') {
      const changes = Array.isArray(data.changes) ? data.changes.filter((value): value is { path: string; operation: string } =>
        !!value && typeof value === 'object' && typeof value.path === 'string' && ['created', 'modified', 'deleted', 'moved_from', 'moved_to'].includes(value.operation)) : [];
      const matching = changes.filter(change => !path || change.path === path || change.path.startsWith(`${path}/`));
      if (path && matching.length === 0 && data.status !== 'partial' && data.gap !== true) return [];
      return [{ id: event.id, executionId: event.executionId, node: event.nodeId, replica: event.replica, tool: journalled ? 'kernel journal' : monitored ? 'workspace monitor' : String(data.tool), outcome: `observed · ${data.status === 'complete' ? 'complete' : data.status === 'baseline' ? 'baseline' : 'partial'}${monitored ? ' · author unknown' : author}${data.gap === true ? ' · coverage gap' : ''}`, paths: matching.map(change => `${change.path} (${change.operation})`) }];
    }
    if (!['filesystem.write', 'filesystem.edit', 'filesystem.multi_edit', 'filesystem.apply_patch', 'filesystem.move', 'filesystem.delete', 'filesystem.mkdir'].includes(String(data.tool))) return [];
    if (!data.args || typeof data.args !== 'object') return [];
    const args = data.args as Record<string, unknown>;
    const paths = [args.path, args.from, args.to, ...patchPaths(args.patch)].filter((value): value is string => typeof value === 'string');
    if (path && !paths.some(value => value === path || value.startsWith(`${path}/`))) return [];
    return [{ id: event.id, executionId: event.executionId, node: event.nodeId, replica: event.replica, tool: String(data.tool), outcome: String(data.outcome ?? 'unknown'), paths }];
  });
}

/** Target paths named by the +++ headers of a unified diff (new files included, /dev/null skipped). */
function patchPaths(patch: unknown): string[] {
  if (typeof patch !== 'string') return [];
  return patch.split('\n').flatMap(line => {
    const match = /^\+\+\+ (?:b\/)?([^\t\r]+)/.exec(line);
    return match && match[1] !== '/dev/null' ? [match[1].trim()] : [];
  });
}
