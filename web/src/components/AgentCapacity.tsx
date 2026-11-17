import { useEffect } from 'react';
import { api } from '../api/client';
import { useAsync, useThrottled } from '../hooks/useData';
import { useEventStream } from '../hooks/useEventStream';
import { ErrorBox, fmtTime } from './common';
import { capacityTransitions } from './environmentPresentation';

/** Events are an admission history, not a claim that a running turn finished. */
export function AgentCapacity({ graphId, nodeId }: { graphId: string; nodeId: string }) {
  const events = useAsync(() => api.events(graphId, { types: 'sandbox.capacity', limit: 200 }), [graphId]);
  const reload = useThrottled(() => void events.reload(), 300);
  useEventStream({ graph: graphId, types: ['sandbox.capacity'] }, reload);
  useEffect(() => {
    const timer = setInterval(() => void events.reload(), 5000);
    return () => clearInterval(timer);
  }, [events.reload]);
  const transitions = capacityTransitions(events.data ?? [], nodeId);
  return <section aria-label="Sandbox admission">
    <h3>Sandbox admission</h3>
    <p className="muted small">Latest recorded transition per replica in the recent history. Waiting means the installation CPU, memory or concurrency budget is full; admission does not mean the task has completed.</p>
    <ErrorBox error={events.error} />
    {!events.error && <ul>{transitions.map(event => <li key={event.replica}>Replica {event.replica}: {event.label} · {fmtTime(event.time)}</li>)}</ul>}
    {!events.error && !transitions.length && <p>{events.loading ? 'Checking admission history…' : 'No recent admission transitions recorded for this agent.'}</p>}
  </section>;
}
