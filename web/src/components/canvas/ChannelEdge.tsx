import { BaseEdge, EdgeLabelRenderer, getBezierPath, type Edge, type EdgeProps } from '@xyflow/react';
import { memo } from 'react';
import type { ChannelEdgeData } from './model';
import { Icon } from '../common';

/**
 * A channel edge. Styles encode delivery semantics:
 *   queue     solid single rail (one shared backlog)
 *   topic     double rail (copy per subscriber)
 *   fanout    dashed (static copy per target)
 *   broadcast dotted (copy per replica)
 * Back edges (cycles) curve underneath the graph; parallel edges between the
 * same pair are fanned out so they don't overlap.
 */
function ChannelEdgeImpl(props: EdgeProps<Edge<ChannelEdgeData>>) {
  const { id, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, data, selected, source, target } =
    props;
  if (!data) return null;
  const off = data.pairIndex * 28;
  let path: string;
  let lx: number;
  let ly: number;
  if (source === target) {
    // Self loop: arc below the node.
    const dy = 90 + off;
    path = `M ${sourceX} ${sourceY} C ${sourceX + 90} ${sourceY + dy}, ${targetX - 90} ${targetY + dy}, ${targetX} ${targetY}`;
    lx = (sourceX + targetX) / 2;
    ly = sourceY + dy * 0.75;
  } else if (sourceX > targetX - 30) {
    // Back edge in a cycle: route below.
    const dy = 110 + off + Math.abs(sourceY - targetY) * 0.2;
    const low = Math.max(sourceY, targetY) + dy;
    path = `M ${sourceX} ${sourceY} C ${sourceX + 120} ${low}, ${targetX - 120} ${low}, ${targetX} ${targetY}`;
    lx = (sourceX + targetX) / 2;
    ly = low - dy * 0.25;
  } else {
    const [p, x, y] = getBezierPath({
      sourceX,
      sourceY: sourceY + off * 0.3,
      targetX,
      targetY: targetY + off * 0.3,
      sourcePosition,
      targetPosition,
      curvature: 0.3 + data.pairIndex * 0.15,
    });
    path = p;
    lx = x;
    ly = y + off;
  }

  const ch = data.channel;
  const active = data.pending > 0 || data.inFlight > 0;
  const errors = (data.issues ?? []).filter((issue) => issue.severity === 'error');
  const cls = `ch-edge ch-${data.type} ${selected || data.selected ? 'ch-selected' : ''} ${active ? 'ch-active' : ''} ${errors.length ? 'ch-invalid' : ''}`;
  const tip = [
    `${ch.id} (${data.type})`,
    ch.types?.length ? `types: ${ch.types.join(', ')}` : '',
    ch.when ? `when: ${ch.when}` : '',
    ch.approval ? 'approval gate: messages are held until approved' : '',
    data.shared ? 'shared queue: consumers compete for one backlog' : '',
    ...errors.map((issue) => `error: ${issue.field || 'channel'}: ${issue.message}`),
  ]
    .filter(Boolean)
    .join('\n');

  return (
    <g className={cls}>
      {data.type === 'topic' ? (
        <>
          <BaseEdge id={id} path={path} className="rail-outer" interactionWidth={18} />
          <path d={path} className="rail-inner" fill="none" />
        </>
      ) : (
        <BaseEdge id={id} path={path} className="rail" interactionWidth={18} markerEnd="url(#lf-arrow)" />
      )}
      {active && <path d={path} className="flow" fill="none" />}
      <title>{tip}</title>
      <EdgeLabelRenderer>
        <div
          className={`edge-label nodrag nopan ${selected || data.selected ? 'selected' : ''}`}
          onClick={() => data.onSelect?.(ch.id)}
          style={{ transform: `translate(-50%, -50%) translate(${lx}px, ${ly}px)` }}
          title={tip}
        >
          <Icon name={data.type} size={12} />
          <span>{ch.label || ch.id}</span>
          {ch.approval && (
            <span className="edge-lock" aria-label="approval gate">
              <Icon name="lock" size={11} />
            </span>
          )}
          {ch.when && <span className="edge-when">if</span>}
          {errors.length > 0 && (
            <span className="edge-invalid" aria-label={`${errors.length} validation errors`}>
              !
            </span>
          )}
          {data.showCount && (
            <span className={`edge-count ${data.pending > 0 ? 'nonzero' : ''}`} title="pending / in flight">
              {data.pending}
              {data.inFlight > 0 ? `+${data.inFlight}` : ''}
            </span>
          )}
          {data.dead > 0 && (
            <span className="edge-dead" title="dead-lettered">
              ☠ {data.dead}
            </span>
          )}
        </div>
      </EdgeLabelRenderer>
    </g>
  );
}

export const ChannelEdge = memo(ChannelEdgeImpl);
