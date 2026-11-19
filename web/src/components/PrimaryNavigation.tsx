import { Icon, type IconName } from './common';

export type NavigationDestination = 'activity' | 'tasks' | 'projects' | 'graphs' | 'agents' | 'approvals' | 'connections' | 'models' | 'skills' | 'administration';

const configuration: { destination: NavigationDestination; label: string; icon: IconName }[] = [
  { destination: 'projects', label: 'Projects', icon: 'projects' },
  { destination: 'graphs', label: 'Workflows', icon: 'workflows' },
  { destination: 'agents', label: 'Agents', icon: 'agents' },
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
  return <nav className="primary-navigation" aria-label="Main navigation">
    <section className="navigation-group" aria-labelledby="work-navigation-label">
      <h2 id="work-navigation-label" className="navigation-label">Work</h2>
      <button type="button" className={`nav-item approvals-btn ${active === 'activity' ? 'on' : ''}`} aria-current={active === 'activity' ? 'page' : undefined} onClick={() => onNavigate('activity')}><Icon name="activity" size={18} /><span>Activity</span></button>
      <button type="button" className={`nav-item approvals-btn ${active === 'tasks' ? 'on' : ''}`} aria-current={active === 'tasks' ? 'page' : undefined} onClick={() => onNavigate('tasks')}><Icon name="tasks" size={18} /><span>Tasks</span></button>
      <button type="button" className={`nav-item approvals-btn ${active === 'approvals' ? 'on' : ''}`} aria-current={active === 'approvals' ? 'page' : undefined} onClick={() => onNavigate('approvals')}>
        <Icon name="approvals" size={18} /><span>Approvals</span> {pending > 0 && <span className="count">{pending}</span>}
      </button>
    </section>
    <section className="navigation-group" aria-label="Configuration">
      <h2 className="navigation-label">Configuration</h2>
      <div className="navigation-items">
        {configuration.map(({destination, label, icon}) => <button key={destination} type="button" className={`nav-item approvals-btn ${active === destination ? 'on' : ''}`} aria-current={active === destination ? 'page' : undefined} onClick={() => onNavigate(destination)}><Icon name={icon} size={18} /><span>{label}</span></button>)}
      </div>
    </section>
  </nav>;
}
