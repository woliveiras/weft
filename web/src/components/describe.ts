import type { WeftEvent } from '../api/types';

function d(ev: WeftEvent): Record<string, unknown> {
  return ev.data && typeof ev.data === 'object' ? (ev.data as Record<string, unknown>) : {};
}

function short(v: unknown, n = 160): string {
  const s = typeof v === 'string' ? v : JSON.stringify(v);
  if (s === undefined) return '';
  return s.length > n ? `${s.slice(0, n)}…` : s;
}

/** One-line human description of an event, for logs and the timeline. */
export function describeEvent(ev: WeftEvent): string {
  const x = d(ev);
  switch (ev.type) {
    case 'agent.output':
      // A live piece of a streamed answer (NAT-09) is still being written.
      return x.partial === true ? `${String(x.text ?? '')}…` : String(x.text ?? short(ev.data));
    case 'agent.broadcast':
      return `broadcast: ${String(x.text ?? short(ev.data))}`;
    case 'agent.plan': {
      const items = Array.isArray(x.items) ? (x.items as { status?: unknown }[]) : [];
      return `plan updated: ${items.filter(i => i?.status === 'done').length}/${items.length} done`;
    }
    case 'agent.context':
      return `context ${String(x.operation ?? '')} ${String(x.outcome ?? '')}: ~${String(x.tokensBefore ?? '?')} → ~${String(x.tokensAfter ?? '?')} tokens`;
    case 'operator.message':
      return `operator${ev.actor ? ` (${ev.actor})` : ''}: ${String(x.text ?? '')}`;
    case 'tool.invoked':
      return `→ ${String(x.tool ?? x.name ?? 'tool')} ${short(x.args ?? '', 120)}`;
    case 'tool.completed':
      return `✓ ${String(x.tool ?? x.name ?? 'tool')}${x.durationMs ? ` (${x.durationMs}ms)` : ''}${x.isError ? ' – error' : ''}`;
    case 'tool.denied':
      return `✗ denied ${String(x.tool ?? x.name ?? 'tool')}: ${String(x.reason ?? '')}`;
    case 'execution.started':
      return `turn #${String(x.iteration ?? '?')} started`;
    case 'execution.completed':
      return `turn #${String(x.iteration ?? '?')} ${String(x.status ?? 'completed')}${x.summary ? `: ${String(x.summary)}` : ''}`;
    case 'execution.failed':
      return `turn #${String(x.iteration ?? '?')} failed: ${String(x.error ?? '')}`;
    case 'replica.state_changed':
      return `state → ${String(x.state ?? '')}${x.detail ? ` (${String(x.detail)})` : ''}`;
    case 'message.published':
    case 'message.injected':
    case 'message.held':
    case 'message.dropped':
    case 'message.dead_lettered':
      return `${ev.type.split('.')[1]} ${String(x.type ?? '')} ${x.source ? `from ${String(x.source)}` : ''}${
        x.metadata && typeof x.metadata === 'object' && (x.metadata as Record<string, unknown>).channel
          ? ` on ${String((x.metadata as Record<string, unknown>).channel)}`
          : ''
      }`.trim();
    case 'approval.requested':
      return `approval requested: ${String(x.title ?? '')}`;
    case 'approval.decided':
      return `approval ${String(x.status ?? 'decided')}: ${String(x.title ?? '')}`;
    case 'log':
      return String(x.msg ?? x.message ?? short(ev.data));
    default:
      return ev.data === undefined ? '' : short(ev.data);
  }
}

export function eventTone(ev: WeftEvent): string {
  if (ev.type.endsWith('failed') || ev.type === 'tool.denied' || ev.type.includes('dead') || ev.level === 'error')
    return 'bad';
  if (ev.type.startsWith('approval') || ev.type === 'message.held' || ev.level === 'warn') return 'warn';
  if (ev.type === 'operator.message') return 'op';
  if (ev.type === 'agent.output') return 'out';
  if (ev.type.startsWith('tool')) return 'tool';
  return '';
}

/**
 * Folds the live pieces of streamed model answers (agent.output with
 * `partial` and a `stream` id, NAT-09) into one line per answer, at the
 * position of its first piece. Once the final output of that stream is in
 * the list, it replaces the live line. Other events are kept as they are.
 */
export function foldLiveOutput(events: WeftEvent[]): WeftEvent[] {
  const finals = new Set<string>();
  const texts = new Map<string, string>();
  for (const ev of events) {
    const stream = liveStream(ev);
    if (!stream) continue;
    if (d(ev).partial === true) texts.set(stream, (texts.get(stream) ?? '') + String(d(ev).text ?? ''));
    else finals.add(stream);
  }
  const out: WeftEvent[] = [];
  const placed = new Set<string>();
  for (const ev of events) {
    const stream = liveStream(ev);
    if (!stream || d(ev).partial !== true) {
      out.push(ev);
      continue;
    }
    if (finals.has(stream) || placed.has(stream)) continue;
    placed.add(stream);
    out.push({ ...ev, data: { ...d(ev), text: texts.get(stream) ?? '' } });
  }
  return out;
}

function liveStream(ev: WeftEvent): string {
  if (ev.type !== 'agent.output') return '';
  const s = d(ev).stream;
  return typeof s === 'string' ? s : '';
}
