import type { MCPConnection } from '../api/types';
export const connectionTabs = ['overview', 'tools', 'history', 'settings'] as const;
export type ConnectionTab = typeof connectionTabs[number];
export const connectionTabTitles: Record<ConnectionTab, string> = {
  overview: 'Overview', tools: 'Tools', history: 'Usage & history', settings: 'Settings',
};
export function connectionLink(id: string, tab: ConnectionTab = 'overview') {
  return `#/connections/c/${encodeURIComponent(id)}/${tab}`;
}
export function matchingConnections(connections: MCPConnection[], search: string, status: string) {
  const query = search.trim().toLowerCase();
  return connections.filter(c => (!status || c.status === status) &&
    [c.id, c.name, c.service].some(value => value.toLowerCase().includes(query)));
}
