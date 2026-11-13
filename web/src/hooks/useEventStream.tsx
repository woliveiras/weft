import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import { eventStreamUrl, MOCK } from '../api/client';
import type { WeftEvent } from '../api/types';
import { MockEventSource } from '../mock/events';

/**
 * One SSE connection for the whole app. The server names each SSE event by
 * its type (`event: replica.state_changed`), so EventSource only delivers it
 * to listeners registered for that name; we register for every type we know
 * plus the default "message" channel.
 */
const EVENT_TYPES = [
  'graph.created',
  'graph.updated',
  'graph.deleted',
  'graph.broker_cleanup_completed',
  'budget.raised',
  'budget.graph_transition',
  'graph.started',
  'graph.stopped',
  'graph.paused',
  'graph.resumed',
  'graph.failed',
  'environment.build_started',
  'environment.ready',
  'environment.proposal',
  'environment.failed',
  'environment.reset',
  'sandbox.capacity',
  'node.scaled',
  'replica.state_changed',
  'execution.started',
  'execution.completed',
  'execution.failed',
  'message.published',
  'message.delivered',
  'message.acked',
  'message.nacked',
  'message.dead_lettered',
  'message.dropped',
  'message.held',
  'message.injected',
  'tool.invoked',
  'tool.completed',
  'workspace.observed',
  'tool.denied',
  'approval.requested',
  'approval.decided',
  'operator.message',
  'agent.output',
  'agent.broadcast',
  'agent.context',
  'agent.plan',
  'log',
  'message',
];

const BUFFER = 2000;

type Listener = (ev: WeftEvent) => void;

interface StreamState {
  connected: boolean;
  /** Ring buffer of recent events (oldest first). */
  recent: () => WeftEvent[];
  subscribe: (fn: Listener) => () => void;
  /** Bumps whenever a new event arrives; cheap re-render trigger. */
  tick: number;
}

const Ctx = createContext<StreamState | null>(null);

export function EventStreamProvider({ children, token }: { children: ReactNode; token: string }) {
  const listeners = useRef(new Set<Listener>());
  const buffer = useRef<WeftEvent[]>([]);
  const lastSeq = useRef<number | undefined>(undefined);
  const [connected, setConnected] = useState(false);
  const [tick, setTick] = useState(0);

  useEffect(() => {
    let es: EventSource | null = null;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let delay = 1000;
    let closed = false;
    let pendingTick = false;

    const handle = (raw: MessageEvent<string>) => {
      let ev: WeftEvent;
      try {
        ev = JSON.parse(raw.data) as WeftEvent;
      } catch {
        return;
      }
      if (typeof ev.seq === 'number') {
        // Resumed streams may replay; drop what we've already seen.
        if (lastSeq.current !== undefined && ev.seq <= lastSeq.current) return;
        lastSeq.current = ev.seq;
      }
      buffer.current.push(ev);
      if (buffer.current.length > BUFFER) buffer.current.splice(0, buffer.current.length - BUFFER);
      listeners.current.forEach((fn) => fn(ev));
      if (!pendingTick) {
        pendingTick = true;
        requestAnimationFrame(() => {
          pendingTick = false;
          setTick((t) => t + 1);
        });
      }
    };

    const connect = () => {
      if (closed) return;
      const url = eventStreamUrl({ after: lastSeq.current });
      es = MOCK ? (new MockEventSource(url) as unknown as EventSource) : new EventSource(url);
      es.onopen = () => {
        delay = 1000;
        setConnected(true);
      };
      for (const t of EVENT_TYPES) es.addEventListener(t, handle as EventListener);
      es.onerror = () => {
        setConnected(false);
        es?.close();
        es = null;
        // Reconnect ourselves (instead of the browser's fixed retry) so the
        // URL carries ?after=<last seq> and the token is re-read.
        timer = setTimeout(connect, delay);
        delay = Math.min(delay * 2, 15000);
      };
    };
    connect();
    return () => {
      closed = true;
      if (timer) clearTimeout(timer);
      es?.close();
    };
  }, [token]);

  const value: StreamState = {
    connected,
    tick,
    recent: () => buffer.current,
    subscribe: (fn) => {
      listeners.current.add(fn);
      return () => listeners.current.delete(fn);
    },
  };
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export interface EventFilter {
  graph?: string;
  node?: string;
  replica?: number;
  types?: string[];
  prefixes?: string[];
}

export function matches(ev: WeftEvent, f: EventFilter): boolean {
  if (f.graph && ev.graphId && ev.graphId !== f.graph) return false;
  if (f.node && ev.nodeId !== f.node) return false;
  if (f.replica !== undefined && f.replica >= 0 && (ev.replica ?? 0) !== f.replica) return false;
  if (f.types && !f.types.includes(ev.type)) return false;
  if (f.prefixes && !f.prefixes.some((p) => ev.type.startsWith(p))) return false;
  return true;
}

/**
 * Subscribe to the shared stream. `onEvent` is called for every matching
 * event; the hook also returns connection state and the recent buffer.
 */
export function useEventStream(filter?: EventFilter, onEvent?: Listener) {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error('useEventStream outside EventStreamProvider');
  const cb = useRef(onEvent);
  cb.current = onEvent;
  const fRef = useRef(filter);
  fRef.current = filter;
  const { subscribe } = ctx;
  useEffect(
    () =>
      subscribe((ev) => {
        if (cb.current && (!fRef.current || matches(ev, fRef.current))) cb.current(ev);
      }),
    // subscribe identity changes each render; the ref pattern keeps one listener.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  );
  return { connected: ctx.connected, recent: ctx.recent, tick: ctx.tick };
}

/**
 * Like useEventStream for live refinements of data loaded through the API:
 * without an EventStreamProvider it subscribes to nothing instead of
 * throwing, and the component keeps what it loaded.
 */
export function useOptionalEventStream(filter: EventFilter, onEvent: Listener) {
  const ctx = useContext(Ctx);
  const cb = useRef(onEvent);
  cb.current = onEvent;
  const fRef = useRef(filter);
  fRef.current = filter;
  const subscribe = ctx?.subscribe;
  const available = !!subscribe;
  useEffect(
    () => (subscribe ? subscribe((ev) => { if (matches(ev, fRef.current)) cb.current(ev); }) : undefined),
    // One listener per provider; the refs carry the latest filter and callback.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [available],
  );
}
