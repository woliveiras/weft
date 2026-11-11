import { api } from '../api/client';
import type { ToolInfo } from '../api/types';
import { useAsync } from '../hooks/useData';
import { ErrorBox } from './common';

export function ToolCatalog() {
  const tools = useAsync(api.tools, []);
  const toolCount = tools.data?.length ?? 0;
  return (
      <section className="integration-section">
        <div className="section-head">
          <div>
            <h2>Tool catalog</h2>
            <p className="muted small">Tools currently registered in Weft and available for agent permissions.</p>
          </div>
          {tools.loading && <p role="status">Loading tools…</p>}
        </div>
        {tools.error && <ErrorBox error={`Could not load tool catalog: ${tools.error}`} />}
        {tools.data && (
          <>
            <p className="integration-result">{toolCount} tools registered</p>
            <div className="integration-tools">
              {tools.data.map((tool: ToolInfo) => (
                <article className="integration-tool" key={tool.name} title={tool.description}>
                  <strong>{tool.name}</strong>
                  {tool.capability !== tool.name && <span className="muted small">{tool.capability}</span>}
                </article>
              ))}
            </div>
            <p className="muted small integration-footnote">
              MCP connections are managed in Connections. Discovered MCP tools run through the gateway when explicitly granted to an agent.
            </p>
          </>
        )}
      </section>
  );
}
