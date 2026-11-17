import type { MCPTemplateRef } from '../api/types';

export function parseMCPLink(query: string): MCPTemplateRef {
  const params = new URLSearchParams(query);
  if (query.length > 256 || [...params.keys()].some(key => !['template', 'version'].includes(key)) || params.getAll('template').length !== 1 || params.getAll('version').length !== 1) {
    throw new Error('Invalid MCP connection link. Only template and version are accepted.');
  }
  const id = params.get('template')!;
  const version = params.get('version')!;
  if (!/^[a-z][a-z0-9-]{0,63}$/.test(id) || !/^[1-9][0-9]{0,5}$/.test(version)) throw new Error('Invalid MCP template or version.');
  return { id, version: Number(version) };
}
export function mcpTemplateLink(template: MCPTemplateRef): string {
  return `#/integrations/connect?template=${encodeURIComponent(template.id)}&version=${template.version}`;
}
