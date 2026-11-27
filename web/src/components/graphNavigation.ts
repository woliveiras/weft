import type { GraphNode } from '../api/types';
export function matchingAgents(nodes: GraphNode[], query: string) {
 const search=query.trim().toLowerCase();
 return nodes.filter(n=>[n.id,n.name??''].some(s=>s.toLowerCase().includes(search)));
}
