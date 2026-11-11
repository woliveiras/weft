import type { GraphNode, GraphSummary } from '../api/types';
export function matchingGraphs(graphs: GraphSummary[], query: string) {
 const search=query.trim().toLowerCase();
 return graphs.filter(g=>[g.graph.metadata.id,g.graph.metadata.name??''].some(s=>s.toLowerCase().includes(search)));
}
export function matchingAgents(nodes: GraphNode[], query: string) {
 const search=query.trim().toLowerCase();
 return nodes.filter(n=>[n.id,n.name??''].some(s=>s.toLowerCase().includes(search)));
}
