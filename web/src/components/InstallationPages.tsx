import { DelegationSection } from './DelegationSection';
import type { Info } from '../api/types';
import { ConnectionsList } from './ConnectionsList';
import { MCPHistory } from './MCPHistory';
import type { ConnectionTab } from './mcpNavigation';
import { MCPSection } from './MCPSection';
import { SCMConnectionCreate, SCMConnectionsSection } from './SCMConnectionsSection';
import { IntegrationsView } from './IntegrationsView';
import { NetworkPolicySection } from './NetworkPolicySection';
import { HistoryRetentionSection } from './HistoryRetentionSection';
import { SharedEnvironmentsSection } from './SharedEnvironmentsSection';
import { ToolCatalog } from './ToolCatalog';

export const administrationSections = ['health', 'network', 'resources', 'retention', 'tools', 'delegation'] as const;
export type AdministrationSection = typeof administrationSections[number];
const titles: Record<AdministrationSection, string> = {
  health: 'System health', network: 'Network policies', resources: 'Shared images & caches',
  retention: 'History retention', tools: 'Tool catalog', delegation: 'Delegation',
};

export function ConnectionsPage({ info, connectionID, connectionTab = 'overview', create, sourceControlCreate, history, connectQuery, oauthQuery }: { info?: Info; connectionID?: string; connectionTab?: ConnectionTab; create?: boolean; sourceControlCreate?: boolean; history?: boolean; connectQuery?: string; oauthQuery?: string }) {
  // Legacy integration and OAuth/template links retain their reviewed setup flow.
  const legacy = location.hash.startsWith('#/integrations') || oauthQuery !== undefined;
  const creating = !legacy && (create || connectQuery !== undefined);
  const overview = !history && !legacy && !creating && !sourceControlCreate && connectionID === undefined;
  return <div className="page installation-page connections-page"><header className="page-head" hidden={connectionID !== undefined || creating || sourceControlCreate}><div><h1>Connections</h1>
    {overview && <p className="muted">Manage agent tool integrations and source control connections for this installation.</p>}</div></header>
    {sourceControlCreate ? <SCMConnectionCreate role={info?.identity?.role} /> : history ? <><a href="#/connections">Back to connections</a><MCPHistory /></> : legacy || create || connectQuery !== undefined || connectionID !== undefined
      ? <MCPSection key={connectionID ?? connectQuery ?? (create ? 'new' : 'manual')} role={info?.identity?.role} connectQuery={connectQuery} oauthQuery={oauthQuery} presentation={legacy ? undefined : creating ? 'create' : 'detail'} connectionID={connectionID} tab={connectionTab} />
      : <><ConnectionsList role={info?.identity?.role} /><SCMConnectionsSection role={info?.identity?.role} /></>}
  </div>;
}

export { ModelsPage } from './ModelsPage';

export function AdministrationPage({ info, infoError, section }: { info?: Info; infoError?: string; section: AdministrationSection }) {
  const canManage = info?.identity?.role === 'admin';
  return <div className="page administration-page"><header className="page-head"><div><h1>Administration</h1>
    <p className="muted">Installation-wide services and policies. Changes may affect multiple graphs.</p></div></header>
    <nav className="section-navigation" aria-label="Administration sections">{administrationSections.map(item =>
      <a key={item} href={`#/administration/${item}`} aria-current={section === item ? 'page' : undefined}>{titles[item]}</a>)}</nav>
    <div key={section} className="administration-content">
      {section === 'health' && <IntegrationsView info={info} infoError={infoError} />}
      {section === 'network' && <NetworkPolicySection canManage={canManage} />}
      {section === 'resources' && <SharedEnvironmentsSection canManage={canManage} />}
      {section === 'retention' && <HistoryRetentionSection canManage={canManage} />}
      {section === 'tools' && <ToolCatalog />}
      {section === 'delegation' && <DelegationSection canManage={canManage} />}
    </div>
  </div>;
}
