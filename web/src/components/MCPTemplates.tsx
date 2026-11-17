import { api } from '../api/client';
import type { MCPTemplate } from '../api/types';
import { useAsync } from '../hooks/useData';
import { Btn, ErrorBox } from './common';
import { mcpTemplateLink, parseMCPLink } from './mcpLinks';

export function MCPTemplates({ query, canManage, onSelect, connectionRoutes }: { connectionRoutes?: boolean; query?: string; canManage: boolean; onSelect: (template: MCPTemplate) => void }) {
  const templates = useAsync(api.mcpTemplates, []);
  let selected: MCPTemplate | undefined;
  let error: string | undefined;
  if (query !== undefined) {
    try {
      const ref = parseMCPLink(query);
      selected = templates.data?.find(t => t.id === ref.id && t.version === ref.version);
      if (templates.data && !selected) error = 'Unknown MCP template version. Choose a template from the catalogue.';
    } catch (e) { error = (e as Error).message; }
  }
  return <section aria-label="MCP templates">
    <h3>MCP templates</h3>
    <p>Opening a link only prepares a review. Saving, discovery and agent permissions are separate steps. OAuth client setup and consent are reviewed separately.</p>
    <ErrorBox error={error ?? templates.error} />
    {templates.loading && <p role="status">Loading templates…</p>}
    {templates.error && <Btn onClick={() => void templates.reload()}>Reload templates</Btn>}
    {selected && <article aria-label="Review MCP template" className="card">
      <h4>{selected.name} · version {selected.version}</h4>
      <p>{selected.endpoint}</p><p>Authentication: {selected.authentication}</p>
      <p>{selected.guidance}</p><p>{selected.validation}</p>
      <a href={selected.source} target="_blank" rel="noreferrer">Official documentation</a><p>Reviewed {selected.reviewedAt}</p>
      {canManage ? <Btn onClick={() => onSelect(selected!)}>Use template defaults</Btn> : <p>An administrator must create this connection.</p>}
    </article>}
    <ul>{templates.data?.map(t => <li key={`${t.id}-${t.version}`}><a href={connectionRoutes?mcpTemplateLink(t).replace('#/integrations/', '#/connections/'):mcpTemplateLink(t)}>{t.name} · v{t.version}</a></li>)}</ul>
  </section>;
}
