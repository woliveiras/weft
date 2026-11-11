import { expect, it } from 'vitest';
import type { MCPConnection } from '../api/types';
import { connectionLink, matchingConnections } from './mcpNavigation';
const connections = [{id:'personal',name:'Personal GitHub',service:'github',status:'ready'}, {id:'team',name:'Work',service:'generic',status:'unavailable'}] as MCPConnection[];
it('combines normalized search across names, IDs and services with exact status', () => {
 expect(matchingConnections(connections,' GITHUB ','ready')).toEqual([connections[0]]);
 expect(matchingConnections(connections,'team','unavailable')).toEqual([connections[1]]);
 expect(matchingConnections(connections,'github','disabled')).toEqual([]);
 expect(matchingConnections([], '', '')).toEqual([]);
 expect(matchingConnections(connections, '', '')).toEqual(connections);
});
it('builds explicit connection section links and encodes identifiers', () => {
 expect(connectionLink('team')).toBe('#/connections/c/team/overview');
 expect(connectionLink('a/b','history')).toBe('#/connections/c/a%2Fb/history');
});
