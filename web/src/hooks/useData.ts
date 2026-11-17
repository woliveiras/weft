import { useCallback, useEffect, useRef, useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { GraphStatus } from '../api/types';
import { useEventStream } from './useEventStream';

/** Load data with an async function; reload() re-runs it. */
export function useAsync<T>(fn: () => Promise<T>, deps: unknown[]) {
  const [data, setData] = useState<T | undefined>(undefined);
  const [error, setError] = useState<string | undefined>(undefined);
  const [loading, setLoading] = useState(true);
  const fnRef = useRef(fn);
  fnRef.current = fn;
  const gen = useRef(0);

  const reload = useCallback(async () => {
    const my = ++gen.current;
    setLoading(true);
    try {
      const d = await fnRef.current();
      if (my === gen.current) {
        setData(d);
        setError(undefined);
      }
    } catch (e) {
      if (my === gen.current) setError(errorMessage(e));
    } finally {
      if (my === gen.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    void reload();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  return { data, error, loading, reload, setData };
}

/** Calls fn at most once per `ms`, trailing. */
export function useThrottled(fn: () => void, ms: number) {
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const fnRef = useRef(fn);
  fnRef.current = fn;
  useEffect(() => () => clearTimeout(timer.current), []);
  return useCallback(() => {
    if (timer.current) return;
    timer.current = setTimeout(() => {
      timer.current = undefined;
      fnRef.current();
    }, ms);
  }, [ms]);
}

const STATUS_EVENT_PREFIXES = ['replica.', 'graph.', 'execution.', 'message.', 'node.', 'approval.'];

/**
 * Live graph status: polled every 2s and refreshed (throttled) on relevant
 * SSE events so state changes show up without waiting for the next poll.
 */
export function useGraphStatus(graphId: string | undefined) {
  const [status, setStatus] = useState<GraphStatus | undefined>(undefined);
  const [error, setError] = useState<string | undefined>(undefined);

  const load = useCallback(async () => {
    if (!graphId) return;
    try {
      const s = await api.status(graphId);
      setStatus(s);
      setError(undefined);
    } catch (e) {
      setError(errorMessage(e));
    }
  }, [graphId]);

  useEffect(() => {
    setStatus(undefined);
    if (!graphId) return;
    void load();
    const t = setInterval(() => void load(), 2000);
    return () => clearInterval(t);
  }, [graphId, load]);

  const refresh = useThrottled(() => void load(), 400);
  useEventStream({ graph: graphId, prefixes: STATUS_EVENT_PREFIXES }, refresh);

  return { status, error, refresh: load, setStatus };
}
