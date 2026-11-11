import { EnvironmentProxies } from './EnvironmentProxies';
import { useEffect } from 'react';
import { api } from '../api/client';
import type { Graph, GraphNode } from '../api/types';
import { useAsync, useThrottled } from '../hooks/useData';
import { useEventStream } from '../hooks/useEventStream';
import { AgentCapacity } from './AgentCapacity';
import { EnvironmentProgress } from './EnvironmentProgress';
import { agentEnvironment } from './environmentPresentation';
import { Btn, ErrorBox } from './common';

export function AgentEnvironment({ graph, node }: { graph: Graph; node: GraphNode }) {
  const source = agentEnvironment(graph, node);
  const isolation = { ...graph.spec.defaults?.isolation, ...node.isolation };
  return <section aria-label="Agent environment">
    <h3>Command environment</h3>
    {source === 'graph' ? <GraphEnvironment key={graph.metadata.id} graph={graph} /> : <p>{source === 'image'
      ? `Commands use the agent sandbox image: ${isolation.image || 'installation default'}. The graph environment does not block this agent.`
      : 'Commands use process isolation inside the Weft server. The graph environment does not apply.'}</p>}
    {source !== 'process' && <EnvironmentProxies key={`proxies-${graph.metadata.id}`} graphId={graph.metadata.id} />}
    {source !== 'process' && <AgentCapacity key={graph.metadata.id} graphId={graph.metadata.id} nodeId={node.id} />}
  </section>;
}

function GraphEnvironment({ graph }: { graph: Graph }) {
  const id = graph.metadata.id;
  const status = useAsync(() => api.environment(id), [id, graph.metadata.version]);
  const reload = useThrottled(() => void status.reload(), 300);
  useEventStream({ graph: id, prefixes: ['environment.', 'sandbox.capacity'] }, reload);
  useEffect(() => {
    const timer = setInterval(() => void status.reload(), 2000);
    return () => clearInterval(timer);
  }, [status.reload]);
  return <>
    <p>Graph environment: {graph.metadata.name || id}. Commands run in its isolated sandbox.</p>
    {!!graph.spec.environment?.companions?.length && <p>Reviewed runtime services: {graph.spec.environment.companions.map(service => service.name).join(", ")}. Each replica has its own internal network and service data; startup requires capacity for the entire group.</p>}
    <ErrorBox error={status.error} />
    {status.error ? <p>Environment readiness could not be verified.</p> : status.data ? <>
      <EnvironmentProgress view={status.data} />
      <p>{status.data.upToDate ? 'Environment is ready for commands. Execution still requires sandbox capacity.' : 'Agent start is blocked until the saved environment is built and ready. Review Graph settings → Environment.'}</p>
      {status.data.image && <p>Image: <code>{status.data.image}</code></p>}
      <ErrorBox error={status.data.error} />
    </> : <p>Checking environment…</p>}
    <Btn small disabled={status.loading} onClick={() => void status.reload()}>Refresh environment</Btn>
  </>;
}
