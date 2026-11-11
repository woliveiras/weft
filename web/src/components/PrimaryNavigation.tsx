import { useState } from 'react';
import { Icon, type IconName } from './common';

export type NavigationDestination = 'tasks' | 'projects' | 'graphs' | 'approvals' | 'connections' | 'models' | 'skills' | 'administration';

const configuration: { destination: NavigationDestination; label: string; icon: IconName }[] = [
  { destination: 'graphs', label: 'Workflows', icon: 'workflows' },
  { destination: 'connections', label: 'Connections', icon: 'connections' },
  { destination: 'models', label: 'Models', icon: 'models' },
  { destination: 'skills', label: 'Skills & instructions', icon: 'skills' },
  { destination: 'administration', label: 'Administration', icon: 'administration' },
];

export function PrimaryNavigation({ active, pending, onNavigate }: {
  active: NavigationDestination;
  pending: number;
  onNavigate: (destination: NavigationDestination) => void;
}) {
  // Keep the active destination visible on direct links and route changes.
  // An explicit collapse applies only to the destination where it was requested.
  const [collapsedAt, setCollapsedAt] = useState<NavigationDestination>();
  const expanded = collapsedAt !== active;
  return <nav className="primary-navigation" aria-label="Main navigation">
    <section className="navigation-group" aria-labelledby="work-navigation-label">
      <h2 id="work-navigation-label" className="navigation-label">Work</h2>
      <button type="button" className={`nav-item approvals-btn ${active === 'tasks' ? 'on' : ''}`} aria-current={active === 'tasks' ? 'page' : undefined} onClick={() => onNavigate('tasks')}><Icon name="tasks" size={18} /><span>Tasks</span></button>
      <button type="button" className={`nav-item approvals-btn ${active === 'projects' ? 'on' : ''}`} aria-current={active === 'projects' ? 'page' : undefined} onClick={() => onNavigate('projects')}><Icon name="projects" size={18} /><span>Projects</span></button>
      <button type="button" className={`nav-item approvals-btn ${active === 'approvals' ? 'on' : ''}`} aria-current={active === 'approvals' ? 'page' : undefined} onClick={() => onNavigate('approvals')}>
        <Icon name="approvals" size={18} /><span>Approvals</span> {pending > 0 && <span className="count">{pending}</span>}
      </button>
    </section>
    <section className="navigation-group" aria-label="Configuration">
      <button type="button" className="navigation-disclosure" aria-expanded={expanded} aria-controls="configuration-navigation" onClick={() => setCollapsedAt(expanded ? active : undefined)}>
        <span>Configuration</span><Icon name={expanded ? 'chevron-down' : 'chevron-right'} size={16} />
      </button>
      <div id="configuration-navigation" className="navigation-items" hidden={!expanded}>
        {configuration.map(({destination, label, icon}) => <button key={destination} type="button" className={`nav-item approvals-btn ${active === destination ? 'on' : ''}`} aria-current={active === destination ? 'page' : undefined} onClick={() => onNavigate(destination)}><Icon name={icon} size={18} /><span>{label}</span></button>)}
      </div>
    </section>
  </nav>;
}
