import { useState } from 'react';
import { createPortal } from 'react-dom';
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

export const NAVIGATION_COMPACT_KEY = 'weft.navigation.compact';

function readPreference() {
  try { return {compact: localStorage.getItem(NAVIGATION_COMPACT_KEY) === 'true', warning: undefined as string | undefined}; }
  catch { return {compact: false, warning: 'Navigation preference could not be read. You can still change this layout for this session.'}; }
}

export function PrimaryNavigation({ active, pending, onNavigate }: {
  active: NavigationDestination;
  pending: number;
  onNavigate: (destination: NavigationDestination) => void;
}) {
  const [{compact, warning}, setPreference] = useState(readPreference);
  const [tooltip, setTooltip] = useState<{label:string;top:number;left:number}>();
  const toggle = () => {
    const next = !compact;
    setTooltip(undefined);
    try {
      localStorage.setItem(NAVIGATION_COMPACT_KEY, String(next));
      setPreference({compact:next, warning:undefined});
    } catch {
      setPreference({compact:next, warning:'Navigation preference could not be saved. This layout applies for this session.'});
    }
  };
  const showTooltip = (button: HTMLButtonElement, label: string) => {
    if (!compact) return;
    const rect = button.getBoundingClientRect();
    setTooltip({label,top:Math.max(8,Math.min(innerHeight-44,rect.top)),left:Math.max(8,Math.min(innerWidth-200,rect.right+8))});
  };
  const entry = (destination: NavigationDestination, label: string, icon: IconName, count = 0) => (
    <button key={destination} type="button" className={`nav-item approvals-btn ${active === destination ? 'on' : ''}`}
      aria-current={active === destination ? 'page' : undefined} title={compact ? label : undefined}
      onMouseEnter={event=>showTooltip(event.currentTarget,label)} onMouseLeave={()=>setTooltip(undefined)}
      onFocus={event=>showTooltip(event.currentTarget,label)} onBlur={()=>setTooltip(undefined)}
      onClick={()=>{setTooltip(undefined);onNavigate(destination);}}>
      <Icon name={icon} size={18}/><span className="nav-item-label">{label}</span>{' '}
      {count > 0 && <span className="count">{count}</span>}
    </button>
  );
  return <nav className={`primary-navigation${compact ? ' is-compact' : ''}`} aria-label="Main navigation"
    onKeyDown={event=>{if(event.key==='Escape')setTooltip(undefined);}}>
    <div className="navigation-header">
      <span className="navigation-label navigation-title">Navigation</span>
      <button type="button" className="icon-btn navigation-toggle" onClick={toggle} aria-expanded={!compact}
        aria-controls="primary-navigation-destinations" aria-label={compact ? 'Expand navigation' : 'Collapse navigation'}
        title={compact ? 'Expand navigation' : 'Collapse navigation'}>
        <Icon name={compact ? 'panel-open' : 'panel-close'} size={18}/>
      </button>
    </div>
    {warning && <span className="navigation-warning small" role="status">{warning}</span>}
    <div className="navigation-destinations" id="primary-navigation-destinations">
      <section className="navigation-group" aria-labelledby="work-navigation-label">
        <h2 id="work-navigation-label" className="navigation-label">Work</h2>
        {entry('activity','Activity','activity')}
        {entry('tasks','Tasks','tasks')}
        {entry('approvals','Approvals','approvals',pending)}
      </section>
      <section className="navigation-group" aria-label="Configuration">
        <h2 className="navigation-label">Configuration</h2>
        <div className="navigation-items">
          {configuration.map(({destination,label,icon})=>entry(destination,label,icon))}
        </div>
      </section>
    </div>
    {tooltip && createPortal(<span role="tooltip" className="navigation-tooltip" style={{top:tooltip.top,left:tooltip.left}}>{tooltip.label}</span>, document.body)}
  </nav>;
}
