import { Activity as ActivityGlyph, BookOpen, Check, ChevronDown, ChevronRight, Cpu, ExternalLink, FolderGit2, ListOrdered, ListTodo, Lock, Maximize, Pause, Play, Plug, Radio, RadioTower, RefreshCw, Settings, ShieldCheck, Split, Square, Workflow, X, Zap, type LucideIcon } from 'lucide-react';
import { useState, type ReactNode } from 'react';
import type { ReplicaState } from '../api/types';

export const STATE_LABEL: Record<string, string> = {
  awaiting_approval: 'awaiting approval',
  dead_lettered: 'dead-lettered',
};

export function stateLabel(s: string): string {
  return STATE_LABEL[s] ?? s;
}

export function StateDot({ state, title }: { state: ReplicaState | string; title?: string }) {
  return <span className={`dot st-${state}`} title={title ?? stateLabel(state)} aria-label={stateLabel(state)} />;
}

export function Badge({ children, tone, title }: { children: ReactNode; tone?: string; title?: string }) {
  return (
    <span className={`badge ${tone ? `tone-${tone}` : ''}`} title={title}>
      {children}
    </span>
  );
}

export function Btn(
  props: React.ButtonHTMLAttributes<HTMLButtonElement> & { tone?: 'primary' | 'danger' | 'ghost'; small?: boolean },
) {
  const { tone, small, className, ...rest } = props;
  return (
    <button
      type="button"
      {...rest}
      className={`btn ${tone ? `btn-${tone}` : ''} ${small ? 'btn-sm' : ''} ${className ?? ''}`}
    />
  );
}

export function ErrorBox({ error }: { error?: string }) {
  if (!error) return null;
  return (
    <div className="error-box" role="alert">
      {error}
    </div>
  );
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="empty">{children}</div>;
}

/** Collapsible pretty JSON. */
export function JsonView({ value, open: initial = false, label }: { value: unknown; open?: boolean; label?: string }) {
  const [open, setOpen] = useState(initial);
  const text = typeof value === 'string' ? value : JSON.stringify(value, null, 2);
  return (
    <div className="json-view">
      <button type="button" className="link" onClick={() => setOpen(!open)} aria-expanded={open}>
        <Icon name={open ? 'chevron-down' : 'chevron-right'} size={12} /> {label ?? 'JSON'}
      </button>
      {open && <pre>{text}</pre>}
    </div>
  );
}

export function fmtTime(iso?: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleTimeString();
}

export function fmtDateTime(iso?: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}

export function fmtDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${Math.round(s % 60)}s`;
  return `${Math.floor(m / 60)}h ${m % 60}m`;
}

export function since(startIso: string, endIso?: string): string {
  const a = new Date(startIso).getTime();
  const b = endIso ? new Date(endIso).getTime() : Date.now();
  return Number.isNaN(a) || Number.isNaN(b) ? '' : fmtDuration(b - a);
}

/**
 * The console's single icon set (Lucide, docs/brand/brand-guidelines.md §6):
 * line icons, round caps and joins, 1.75 stroke, coloured by currentColor.
 */
const icons = {
  play: Play, stop: Square, pause: Pause, bolt: Zap, fit: Maximize, lock: Lock,
  queue: ListOrdered, topic: Radio, fanout: Split, broadcast: RadioTower,
  check: Check, x: X, refresh: RefreshCw, external: ExternalLink,
  activity: ActivityGlyph, tasks: ListTodo, projects: FolderGit2, approvals: ShieldCheck, workflows: Workflow,
  connections: Plug, models: Cpu, skills: BookOpen, administration: Settings,
  'chevron-down': ChevronDown, 'chevron-right': ChevronRight,
} satisfies Record<string, LucideIcon>;

export type IconName = keyof typeof icons;

export function Icon({ name, size = 14 }: { name: IconName; size?: number }) {
  const Glyph = icons[name];
  // Filled glyphs keep their recognisable solid shape at small sizes.
  const filled = name === 'play' || name === 'stop' || name === 'bolt';
  return <Glyph className="icon" size={size} strokeWidth={1.75} aria-hidden="true" focusable="false" fill={filled ? 'currentColor' : 'none'} />;
}

export function Modal({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="modal" role="dialog" aria-modal="true" aria-label={title}>
        <div className="modal-head">
          <h2>{title}</h2>
          <button type="button" className="icon-btn" onClick={onClose} aria-label="Close">
            <Icon name="x" />
          </button>
        </div>
        <div className="modal-body">{children}</div>
      </div>
    </div>
  );
}


/** Optional guidance stays beside the control it explains, without hiding warnings. */
export function ContextHelp({title, children}: {title: string; children: ReactNode}) {
  return <details className="context-help"><summary>{title}</summary><div>{children}</div></details>;
}

export function PageHeading({title, description, children}: {title: string; description: string; children?: ReactNode}) {
  return <header className="page-head"><div><h1>{title}</h1><p className="muted">{description}</p></div>{children}</header>;
}
