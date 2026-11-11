import { ContextHelp } from '../common';
import {
  Background,
  Controls,
  MiniMap,
  ReactFlow,
  ReactFlowProvider,
  useEdgesState,
  useNodesState,
  useReactFlow,
  useUpdateNodeInternals,
  type Connection,
  type Edge,
  type Node,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { useEffect, useMemo, useRef } from 'react';
import type { Graph, GraphStatus } from '../../api/types';
import { Btn } from '../common';
import { AgentNode, PortNode } from './AgentNode';
import { ChannelEdge } from './ChannelEdge';
import type { EdgeRef, IssueIndex } from './edit';
import { buildElements, layout, type AgentNodeData, type ChannelEdgeData } from './model';

export type Selection = { kind: 'node'; id: string } | { kind: 'channel'; id: string } | null;

const nodeTypes = { agent: AgentNode, port: PortNode };
const edgeTypes = { channel: ChannelEdge };

interface Props {
  graph: Graph;
  status?: GraphStatus;
  selection: Selection;
  onSelect: (s: Selection) => void;
  editable?: boolean;
  onConnect?: (connection: Connection) => void;
  /** Delete key on a selection. The graph owner confirms and applies it; the canvas never removes elements itself. */
  onRemove?: (nodeIds: string[], edges: EdgeRef[]) => void;
  /** Validation findings to mark on agents and channels. */
  issues?: IssueIndex;
  onNodePositionChange?: (positions: Record<string, { x: number; y: number }>) => void;
  onAddNode?: (position: { x: number; y: number }, positions: Record<string, { x: number; y: number }>) => void;
}

function Canvas({
  graph,
  status,
  selection,
  onSelect,
  editable = false,
  onConnect,
  onRemove,
  issues,
  onNodePositionChange,
  onAddNode,
}: Props) {
  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);
  const { fitView, screenToFlowPosition, getNodes } = useReactFlow();
  const updateNodeInternals = useUpdateNodeInternals();
  const onSelectRef = useRef(onSelect);
  const didFitView = useRef(false);
  const canvasRef = useRef<HTMLDivElement>(null);
  onSelectRef.current = onSelect;

  // Structure: rebuild + relayout only when the document changes, so the
  // user's drags survive status polling.
  const structureKey = useMemo(
    () => JSON.stringify({ n: graph.spec.nodes, c: graph.spec.channels, v: graph.metadata.version }),
    [graph],
  );
  useEffect(() => {
    const { nodes: n, edges: e } = buildElements(graph, status);
    setNodes(layout(n, e));
    setEdges(e);
    // Fit once the laid-out nodes have been measured.
    const t = didFitView.current ? undefined : setTimeout(() => {
      didFitView.current = true;
      void fitView({ padding: 0.15, duration: 200 });
    }, 60);
    return () => {
      if (t !== undefined) clearTimeout(t);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [structureKey]);

  // Rebuilding nodes clears React Flow's handle bounds, even when their DOM
  // dimensions have not changed. Activity also restarts effects on reveal.
  // Measure after the structure render instead of relying on a resize event.
  useEffect(() => {
    const frame = requestAnimationFrame(() => updateNodeInternals(getNodes().map(node => node.id)));
    return () => cancelAnimationFrame(frame);
  }, [structureKey, updateNodeInternals, getNodes]);

  // Live data: patch node/edge data in place.
  useEffect(() => {
    const fresh = buildElements(graph, status, issues);
    const nodeData = new Map(fresh.nodes.map((n) => [n.id, n.data]));
    const edgeData = new Map(fresh.edges.map((e) => [e.id, e.data]));
    setNodes((ns) =>
      ns.map((n) => {
        const d = nodeData.get(n.id);
        return d ? { ...n, data: d, selected: selection?.kind === 'node' && selection.id === n.id } : n;
      }),
    );
    setEdges((es) =>
      es.map((e) => {
        const d = edgeData.get(e.id) as ChannelEdgeData | undefined;
        if (!d) return e;
        return {
          ...e,
          data: {
            ...d,
            selected: selection?.kind === 'channel' && selection.id === d.channel.id,
            onSelect: (id: string) => onSelectRef.current({ kind: 'channel', id }),
          },
        };
      }),
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status, selection, structureKey, issues]);

  return (
    <div className="canvas-inner" ref={canvasRef}>
      {editable && onAddNode && (
        <div className="canvas-toolbar">
          <Btn
            small
            tone="primary"
            onClick={() => {
              const positions = Object.fromEntries(
                getNodes()
                  .filter((node) => node.type === 'agent')
                  .map((node) => [node.id, node.position]),
              );
              const bounds = canvasRef.current?.getBoundingClientRect();
              const point = bounds
                ? { x: bounds.left + bounds.width / 2, y: bounds.top + bounds.height / 2 }
                : { x: window.innerWidth / 2, y: window.innerHeight / 2 };
              onAddNode(screenToFlowPosition(point), positions);
            }}
          >
            Add agent
          </Btn>
          <ContextHelp title="Canvas controls"><p>Drag between agent handles to add a channel. Select an item, or Shift-click or drag to select several. Press Delete to review removal before confirming.</p></ContextHelp>
        </div>
      )}
      <ReactFlow
        nodes={nodes.map((node) => ({ ...node, deletable: editable && node.type === 'agent' }))}
        edges={editable ? edges.map((edge) => ({ ...edge, deletable: true })) : edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={editable ? onConnect : undefined}
        onBeforeDelete={async ({ nodes: removedNodes, edges: removedEdges }) => {
          if (editable && onRemove) {
            onRemove(
              removedNodes.filter((node) => node.type === 'agent').map((node) => node.id),
              removedEdges.flatMap((edge) => {
                const data = edge.data as ChannelEdgeData | undefined;
                return data ? [{ channelId: data.channel.id, target: data.target }] : [];
              }),
            );
          }
          // The document is the source of truth: the canvas redraws from it.
          return false;
        }}
        onNodeDragStop={
          editable
            ? () =>
                onNodePositionChange?.(
                  Object.fromEntries(
                    getNodes()
                      .filter((node) => node.type === 'agent')
                      .map((node) => [node.id, node.position]),
                  ),
                )
            : undefined
        }
        nodesDraggable={editable}
        nodesConnectable={editable}
        deleteKeyCode={editable ? ['Backspace', 'Delete'] : null}
        onNodeClick={(_, n) => n.type === 'agent' && onSelect({ kind: 'node', id: n.id })}
        onEdgeClick={(_, e) => onSelect({ kind: 'channel', id: (e.data as ChannelEdgeData).channel.id })}
        onPaneClick={() => onSelect(null)}
        fitView
        fitViewOptions={{ padding: 0.2 }}
        minZoom={0.15}
        maxZoom={2}
        proOptions={{ hideAttribution: true }}
        colorMode="light"
      >
        <svg style={{ position: 'absolute', width: 0, height: 0 }}>
          <defs>
            <marker id="lf-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto">
              <path d="M0,0 L10,5 L0,10 z" fill="currentColor" />
            </marker>
          </defs>
        </svg>
        <Background gap={24} size={1} color="var(--canvas-grid)" />
        <Controls showInteractive={false} />
        <MiniMap
          pannable
          zoomable
          nodeColor={(n) => {
            const st = (n.data as AgentNodeData).status?.state;
            return st === 'running'
              ? 'var(--green)'
              : st === 'idle'
                ? 'var(--blue)'
                : st === 'failed'
                  ? 'var(--red)'
                  : '#c6c3bc';
          }}
          maskColor="rgba(41,42,50,0.12)"
        />
      </ReactFlow>
    </div>
  );
}

export function GraphCanvas(props: Props) {
  return (
    <div className="canvas-wrap">
      <ReactFlowProvider>
        <Canvas {...props} />
      </ReactFlowProvider>
    </div>
  );
}
