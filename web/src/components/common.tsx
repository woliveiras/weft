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
        {open ? '▾' : '▸'} {label ?? 'JSON'}
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

/** Small icon set (inline SVG keeps the bundle dependency-free). */
export function Icon({ name, size = 14 }: { name: string; size?: number }) {
  const common = {
    width: size,
    height: size,
    viewBox: '0 0 24 24',
    fill: 'none',
    stroke: 'currentColor',
    strokeWidth: 2,
    strokeLinecap: 'round' as const,
    strokeLinejoin: 'round' as const,
    'aria-hidden': true,
  };
  switch (name) {
    case 'play':
      return (
        <svg {...common}>
          <path d="M6 4l14 8-14 8z" fill="currentColor" />
        </svg>
      );
    case 'stop':
      return (
        <svg {...common}>
          <rect x="6" y="6" width="12" height="12" rx="1" fill="currentColor" />
        </svg>
      );
    case 'pause':
      return (
        <svg {...common}>
          <path d="M8 5v14M16 5v14" />
        </svg>
      );
    case 'bolt':
      return (
        <svg {...common}>
          <path d="M13 2L4 14h7l-1 8 9-12h-7z" fill="currentColor" />
        </svg>
      );
    case 'fit':
      return (
        <svg {...common}>
          <path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5" />
        </svg>
      );
    case 'lock':
      return (
        <svg {...common}>
          <rect x="5" y="11" width="14" height="10" rx="2" />
          <path d="M8 11V7a4 4 0 018 0v4" />
        </svg>
      );
    case 'queue':
      return (
        <svg {...common}>
          <path d="M4 7h16M4 12h16M4 17h10" />
        </svg>
      );
    case 'topic':
      return (
        <svg {...common}>
          <circle cx="6" cy="12" r="2" />
          <path d="M8 12h4M12 12l6-5M12 12l6 5M12 12h7" />
        </svg>
      );
    case 'fanout':
      return (
        <svg {...common}>
          <path d="M4 12h6M10 12l8-6M10 12l8 6M10 12h8" />
        </svg>
      );
    case 'broadcast':
      return (
        <svg {...common}>
          <circle cx="12" cy="12" r="2" />
          <path d="M7.8 7.8a6 6 0 000 8.4M16.2 7.8a6 6 0 010 8.4M5 5a10 10 0 000 14M19 5a10 10 0 010 14" />
        </svg>
      );
    case 'check':
      return (
        <svg {...common}>
          <path d="M5 12l5 5L20 7" />
        </svg>
      );
    case 'x':
      return (
        <svg {...common}>
          <path d="M6 6l12 12M18 6L6 18" />
        </svg>
      );
    case 'refresh':
      return (
        <svg {...common}>
          <path d="M20 11a8 8 0 10-2.3 5.7M20 4v7h-7" />
        </svg>
      );
    default:
      return null;
  }
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
