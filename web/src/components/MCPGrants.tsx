import { useId, useRef, useState } from 'react';
import { api } from '../api/client';
import { grantName, type ToolGrant } from '../api/types';
import { useAsync } from '../hooks/useData';
import { Btn, ErrorBox } from './common';

export function MCPGrants({ grants, onChange }: {
  grants: (ToolGrant | string)[];
  onChange: (grants: (ToolGrant | string)[]) => void;
}) {
  const tools = useAsync(api.mcpTools, []);
  const descriptionPrefix = useId();
  const [search, setSearch] = useState('');
  const results = useRef<HTMLDivElement>(null);
  const selected = new Set(grants.map(grantName));
  const catalog = new Map((tools.data ?? []).map(tool => [tool.name, tool.description]));
  const unavailable = tools.loading && !tools.data ? [] : [...selected].filter(
    name => name.startsWith('mcp.') && !catalog.has(name),
  );
  const query = search.trim().toLowerCase();
  const names = [...catalog.keys(), ...unavailable];
  const matches = names.filter(name => name.toLowerCase().includes(query));

  return (
    <section className="mcp-grants" aria-label="MCP permissions">
      <h4>MCP permissions</h4>
      <div className="mcp-grants-actions">
        <p>Only explicitly selected MCP tools can run. Permissions and connection status are checked on every call. Save the graph to persist changes.</p>
        <Btn small onClick={() => void tools.reload()} disabled={tools.loading}>Reload MCP tools</Btn>
      </div>
      <div className="tool-picker">
        <label className="tool-search">
          <span className="sr-only">Search MCP tools</span>
          <input
            type="search"
            placeholder="Search MCP tools"
            value={search}
            onChange={event => {
              setSearch(event.target.value);
              if (results.current) results.current.scrollTop = 0;
            }}
          />
        </label>
        <ErrorBox error={tools.error} />
        {tools.loading && !tools.data && <p role="status">Loading MCP tools…</p>}
        {!tools.loading && !tools.error && !catalog.size && (
          <p>No enabled, discovered MCP tools. Configure connections in Integrations.</p>
        )}
        {matches.length ? (
          <div className="tool-options" role="group" aria-label="MCP tools" tabIndex={0} ref={results}>
            {matches.map(name => {
              const description = catalog.has(name)
                ? catalog.get(name)?.trim()
                : 'Unavailable in the current catalogue; uncheck to remove this grant.';
              const descriptionId = description ? `${descriptionPrefix}-${name}` : undefined;
              return (
                <label className="tool-option" key={name}>
                  <input
                    type="checkbox"
                    aria-label={name}
                    aria-describedby={descriptionId}
                    checked={selected.has(name)}
                    onChange={event => onChange(event.target.checked
                      ? [...grants, { name }]
                      : grants.filter(grant => grantName(grant) !== name))}
                  />
                  <span className="tool-option-copy">
                    <code>{name}</code>
                    {description && <span id={descriptionId} className="muted small">{description}</span>}
                  </span>
                </label>
              );
            })}
          </div>
        ) : names.length > 0 && <p role="status">No MCP tools match your search.</p>}
      </div>
    </section>
  );
}
