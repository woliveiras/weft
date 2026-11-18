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
  const compact = connectionRoutes && query === undefined;
  const body = <>
    <p className="muted">Choose a template to review its settings. Saving, discovery and agent permissions are separate steps.</p>
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
    <ul className={connectionRoutes ? 'mcp-template-list' : undefined}>{templates.data?.map(t => <li key={`${t.id}-${t.version}`}><a href={connectionRoutes?mcpTemplateLink(t).replace('#/integrations/', '#/connections/'):mcpTemplateLink(t)}>{t.name} · v{t.version}</a></li>)}</ul>
  </>;
  if (compact) return <details className="card mcp-setup-disclosure"><summary>Browse MCP templates</summary>{body}</details>;
  return <section aria-label="MCP templates" className={connectionRoutes ? 'mcp-template-review' : undefined}>
    <h2>MCP templates</h2>{body}
  </section>;
}
