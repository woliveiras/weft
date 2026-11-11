import { DelegationSection } from './DelegationSection';
import { useRef, useState } from 'react';
import { Btn } from './common';
import type { Info } from '../api/types';
import { ConnectionsList } from './ConnectionsList';
import { MCPHistory } from './MCPHistory';
import type { ConnectionTab } from './mcpNavigation';
import { MCPSection } from './MCPSection';
import { ProvidersSection, type ProvidersHandle } from './ProvidersSection';
import { CostsSection } from './CostsSection';
import { SCMConnectionsSection } from './SCMConnectionsSection';
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

export function ConnectionsPage({ info, connectionID, connectionTab = 'overview', create, history, connectQuery, oauthQuery }: { info?: Info; connectionID?: string; connectionTab?: ConnectionTab; create?: boolean; history?: boolean; connectQuery?: string; oauthQuery?: string }) {
  // Legacy integration and OAuth/template links retain their reviewed setup flow.
  const legacy = location.hash.startsWith('#/integrations') || oauthQuery !== undefined;
  return <div className="page installation-page"><header className="page-head" hidden={connectionID !== undefined}><div><h1>Connections</h1>
    <p className="muted">Manage installation MCP connections. Agent tool permissions are granted separately.</p></div></header>
    {history ? <><a href="#/connections">Back to connections</a><MCPHistory /></> : legacy || create || connectQuery !== undefined || connectionID !== undefined
      ? <MCPSection key={connectionID ?? connectQuery ?? (create ? 'new' : 'manual')} role={info?.identity?.role} connectQuery={connectQuery} oauthQuery={oauthQuery} presentation={legacy ? undefined : create || connectQuery !== undefined ? 'create' : 'detail'} connectionID={connectionID} tab={connectionTab} />
      : <><ConnectionsList role={info?.identity?.role} /><SCMConnectionsSection role={info?.identity?.role} /></>}
  </div>;
}

export function ModelsPage({ info }: { info?: Info }) {
  const providers = useRef<ProvidersHandle>(null);
  const [testing, setTesting] = useState(false);
  const testAll = async () => {
    setTesting(true);
    try { await providers.current?.testAll(); } finally { setTesting(false); }
  };
  const role = info?.identity?.role;
  return <div className="page installation-page"><header className="page-head"><div><h1>Models</h1>
    <p className="muted">Configure model providers for this installation, then choose a model in each agent.</p></div>
    {(role === 'admin' || role === 'operator') && <Btn onClick={() => void testAll()} disabled={testing}>{testing ? 'Testing providers…' : 'Test all providers'}</Btn>}</header>
    <ProvidersSection ref={providers} canEdit={role === 'admin'} canTest={role === 'admin' || role === 'operator'} />
    <CostsSection canEdit={role === 'admin'} />
  </div>;
}

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
