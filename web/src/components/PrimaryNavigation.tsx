import { useState } from 'react';

export type NavigationDestination = 'tasks' | 'projects' | 'graphs' | 'approvals' | 'connections' | 'models' | 'skills' | 'administration';

const configuration: { destination: NavigationDestination; label: string }[] = [
  { destination: 'graphs', label: 'Workflows' },
  { destination: 'connections', label: 'Connections' },
  { destination: 'models', label: 'Models' },
  { destination: 'skills', label: 'Skills & instructions' },
  { destination: 'administration', label: 'Administration' },
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
      <button type="button" className={`approvals-btn ${active === 'tasks' ? 'on' : ''}`} aria-current={active === 'tasks' ? 'page' : undefined} onClick={() => onNavigate('tasks')}>Tasks</button>
      <button type="button" className={`approvals-btn ${active === 'projects' ? 'on' : ''}`} aria-current={active === 'projects' ? 'page' : undefined} onClick={() => onNavigate('projects')}>Projects</button>
      <button type="button" className={`approvals-btn ${active === 'approvals' ? 'on' : ''}`} aria-current={active === 'approvals' ? 'page' : undefined} onClick={() => onNavigate('approvals')}>
        Approvals {pending > 0 && <span className="count">{pending}</span>}
      </button>
    </section>
    <section className="navigation-group" aria-label="Configuration">
      <button type="button" className="navigation-disclosure" aria-expanded={expanded} aria-controls="configuration-navigation" onClick={() => setCollapsedAt(expanded ? active : undefined)}>
        <span>Configuration</span><span aria-hidden="true">{expanded ? '▾' : '▸'}</span>
      </button>
      <div id="configuration-navigation" className="navigation-items" hidden={!expanded}>
        {configuration.map(({destination, label}) => <button key={destination} type="button" className={`approvals-btn ${active === destination ? 'on' : ''}`} aria-current={active === destination ? 'page' : undefined} onClick={() => onNavigate(destination)}>{label}</button>)}
      </div>
    </section>
  </nav>;
}
