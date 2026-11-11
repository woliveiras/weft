import { useEffect, useMemo, useRef, useState } from 'react';
import { api } from '../api/client';
import type { Graph, Info, WeftEvent } from '../api/types';
import { useEventStream } from '../hooks/useEventStream';
import { Btn, JsonView, fmtTime } from './common';
import { describeEvent, eventTone } from './describe';
import { tempoLink } from './traces';

const MAX = 3000;

/** Live timeline of the event log for one graph. */
export function EventsView({ graph, info }: { graph: Graph; info?: Info }) {
  const gid = graph.metadata.id;
  const [events, setEvents] = useState<WeftEvent[]>([]);
  const [typeFilter, setTypeFilter] = useState('');
  const [nodeFilter, setNodeFilter] = useState('');
  const [follow, setFollow] = useState(true);
  const [paused, setPaused] = useState(false);
  const queued = useRef<WeftEvent[]>([]);
  const box = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let cancelled = false;
    setEvents([]);
    api
      .events(gid, { limit: 500 })
      .then((evs) => !cancelled && setEvents((cur) => merge(evs, cur)))
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [gid]);

  const { connected } = useEventStream({ graph: gid }, (ev) => {
    if (paused) {
      queued.current.push(ev);
      return;
    }
    setEvents((cur) => merge(cur, [ev]));
  });

  useEffect(() => {
    if (!paused && queued.current.length) {
      const q = queued.current;
      queued.current = [];
      setEvents((cur) => merge(cur, q));
    }
  }, [paused]);

  const shown = useMemo(
    () =>
      events.filter(
        (e) => (!typeFilter || e.type.startsWith(typeFilter)) && (!nodeFilter || e.nodeId === nodeFilter),
      ),
    [events, typeFilter, nodeFilter],
  );

  useEffect(() => {
    if (follow && box.current) box.current.scrollTop = box.current.scrollHeight;
  }, [shown, follow]);

  const types = useMemo(() => Array.from(new Set(events.map((e) => e.type.split('.')[0]))).sort(), [events]);

  return (
    <div className="page fill">
      <div className="page-head">
        <h2>Events</h2>
        <span className={`conn ${connected ? 'on' : ''}`}>{connected ? 'live' : 'reconnecting…'}</span>
        <label>
          type{' '}
          <select value={typeFilter} onChange={(e) => setTypeFilter(e.target.value)}>
            <option value="">all</option>
            {types.map((t) => (
              <option key={t} value={t}>
                {t}.*
              </option>
            ))}
          </select>
        </label>
        <label>
          node{' '}
          <select value={nodeFilter} onChange={(e) => setNodeFilter(e.target.value)}>
            <option value="">all</option>
            {graph.spec.nodes.map((n) => (
              <option key={n.id} value={n.id}>
                {n.name || n.id}
              </option>
            ))}
          </select>
        </label>
        <label className="small">
          <input type="checkbox" checked={follow} onChange={(e) => setFollow(e.target.checked)} /> autoscroll
        </label>
        <Btn small onClick={() => setPaused(!paused)}>
          {paused ? `Resume (${queued.current.length})` : 'Pause'}
        </Btn>
      </div>
      <div className="timeline" ref={box}>
        {shown.map((e) => (
          <div key={e.id} className={`tl-row ${eventTone(e)}`}>
            <span className="log-time">{fmtTime(e.time)}</span>
            <span className="tl-type">{e.type}</span>
            <span className="tl-who">
              {e.nodeId ? `${e.nodeId}${e.replica !== undefined ? ` n${e.replica}` : ''}` : e.actor ?? ''}
            </span>
            <span className="tl-text">
              {describeEvent(e)}
              {e.traceId && info?.grafanaUrl && (
                <>
                  {' '}
                  <a href={tempoLink(info.grafanaUrl, e.traceId)} target="_blank" rel="noreferrer" className="small">
                    trace
                  </a>
                </>
              )}
              {e.data !== undefined && <JsonView value={e.data} label="data" />}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

function merge(a: WeftEvent[], b: WeftEvent[]): WeftEvent[] {
  const seen = new Set(a.map((e) => e.id));
  const out = [...a, ...b.filter((e) => !seen.has(e.id))];
  out.sort((x, y) => x.seq - y.seq);
  return out.length > MAX ? out.slice(-MAX) : out;
}
