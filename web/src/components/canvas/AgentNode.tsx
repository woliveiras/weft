import { Handle, Position, type NodeProps, type Node } from '@xyflow/react';
import { memo } from 'react';
import type { AgentNodeData, PortNodeData } from './model';
import { nodeCount } from './model';
import { StateDot, stateLabel } from '../common';

function runtimeLabel(n: AgentNodeData['node']): string {
  const k = n.runtime?.kind ?? 'native';
  if (k === 'external') return n.runtime?.framework ? `${n.runtime.framework}` : 'external';
  return k;
}

function AgentNodeImpl({ data, selected }: NodeProps<Node<AgentNodeData>>) {
  const { node, status, issues } = data;
  const errors = (issues ?? []).filter((issue) => issue.severity === 'error');
  const warnings = (issues ?? []).filter((issue) => issue.severity === 'warning');
  const state = status?.state ?? 'stopped';
  const { desired, label } = nodeCount(node);
  const reps = status?.replicas ?? [];
  const live = reps.filter((r) => !['stopped', 'completed', 'failed'].includes(r.state)).length;
  const isCluster = desired > 1 || node.cluster?.mode === 'autoscale';
  const stack = Math.min(isCluster ? Math.max(desired, 2) : 1, 5) - 1;
  const awaiting = reps.some((r) => r.state === 'awaiting_approval');
  const completed = reps.length > 0 && reps.every((r) => r.state === 'completed');
  const iterations = reps.reduce((a, r) => a + (r.iteration ?? 0), 0);
  const mode = data.loopMode ?? node.loop?.mode ?? 'once';
  const model = node.model?.name ? `${node.model.provider ? `${node.model.provider}/` : ''}${node.model.name}` : '';

  return (
    <div
      className={[
        'agent-node',
        `st-${state}`,
        selected ? 'selected' : '',
        node.disabled ? 'disabled' : '',
        stack > 0 ? `stack-${stack}` : '',
        errors.length ? 'has-errors' : warnings.length ? 'has-warnings' : '',
      ].join(' ')}
      title={node.description}
    >
      <Handle type="target" position={Position.Left} />
      {node.cluster?.comms === 'ring' && <div className="ring-badge">ring</div>}
      <div className="agent-head">
        <StateDot state={state} />
        <span className="agent-name">{node.name || node.id}</span>
        {completed && (
          <span className="mark-completed" title="Completed: stopped by its own stop policy">
            done
          </span>
        )}
        {(errors.length > 0 || warnings.length > 0) && (
          <span
            className={errors.length ? 'mark-invalid' : 'mark-warning'}
            title={[...errors, ...warnings].map((issue) => `${issue.field || 'agent'}: ${issue.message}`).join('\n')}
            aria-label={errors.length ? `${errors.length} validation errors` : `${warnings.length} validation warnings`}
          >
            {errors.length ? `${errors.length} error${errors.length > 1 ? 's' : ''}` : 'warning'}
          </span>
        )}
        {awaiting && (
          <span className="mark-approval" title="A replica is waiting for human approval">
            approval
          </span>
        )}
      </div>
      <div className="agent-sub">
        <span className={`chip rt-${node.runtime?.kind ?? 'native'}`}>{runtimeLabel(node)}</span>
        <span className="chip" title="Loop mode">
          {mode}
          {node.loop?.stop?.policy && node.loop.stop.policy !== 'never' ? ` · ${node.loop.stop.policy}` : ''}
        </span>
        {isCluster && (
          <span className="chip chip-cluster" title={`${node.cluster?.mode ?? 'fixed'} cluster`}>
            {live} of {label}
          </span>
        )}
        {node.workspace?.mode === 'worktree' && (
          <span className="chip" title="git worktree per replica">
            worktree
          </span>
        )}
      </div>
      {model && <div className="agent-model">{model}</div>}
      <div className="agent-foot">
        <span>{stateLabel(state)}</span>
        <span title="Turns taken (all replicas)">#{iterations}</span>
        {(status?.pending ?? 0) > 0 && (
          <span className="pending-in" title="Messages waiting for this node">
            {status?.pending} waiting
          </span>
        )}
      </div>
      <Handle type="source" position={Position.Right} />
    </div>
  );
}

export const AgentNode = memo(AgentNodeImpl);

function PortNodeImpl({ data }: NodeProps<Node<PortNodeData>>) {
  return (
    <div className={`port-node port-${data.kind}`}>
      {data.kind === 'ingress' && <Handle type="source" position={Position.Right} />}
      {data.kind === 'nosubs' && <Handle type="target" position={Position.Left} />}
      <span>{data.kind === 'ingress' ? '⇢ ' : ''}{data.label}</span>
    </div>
  );
}

export const PortNode = memo(PortNodeImpl);
