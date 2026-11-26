import {useEffect, useId, useRef, useState, type KeyboardEvent} from 'react';
import {Check, ChevronDown} from 'lucide-react';
import {FieldHelp} from './FieldHelp';
import {projectChoiceLabel} from './taskDefaults';
import './repositoryPicker.css';
import type {Project, TaskRepository} from '../api/tasks';
import type {SCMConnection} from '../api/types';
import {Btn} from './common';

export function repositoryBindings(ids: string[], projects: Project[], previous: TaskRepository[] = []): TaskRepository[] {
  return ids.map(id => {
    const saved = previous.find(row => row.projectId === id);
    const project = projects.find(project => project.id === id);
    if (saved && (saved.baseCommit || !project)) return saved;

    const name = project?.name.split('/').pop() || id;
    const stem = name.toLowerCase().replace(/[^a-z0-9_-]+/g, '-').slice(0, 40);
    return {
      projectId: id, alias: /^[a-z]/.test(stem) ? stem : 'repo-' + stem.slice(0, 35),
      baseCommit: project?.head || '', sourceRef: project?.repositoryRef || 'HEAD', checks: project?.checks
    };
  });
}

interface PickerProps {
  label: string;
  help: string;
  projects: Project[];
  ids: string[];
  onChange: (ids: string[]) => void;
  disabled?: boolean;
  loading?: boolean;
}

export function RepositoryPicker({label, help, projects, ids, onChange, disabled, loading}: PickerProps) {
  const id = useId();
  const listId = id + '-options';
  const root = useRef<HTMLDivElement>(null);
  const field = useRef<HTMLInputElement>(null);
  const [open, setOpen] = useState(false);
  const [above, setAbove] = useState(false);
  const [query, setQuery] = useState('');
  const [activeId, setActiveId] = useState<string>();
  const selectedNames = ids.map(id => projects.find(p => p.id === id)?.name || id);
  const catalog = projects.map(p => ({
    id: p.id, name: p.name, source: p.repositoryUrl || p.path || p.id,
    revision: [p.repositoryRef || 'HEAD', p.head?.slice(0, 12)].filter(Boolean).join(' · '),
    search: projectChoiceLabel(p)
  }));
  for (const id of ids) {
    if (!catalog.some(p => p.id === id)) {
      catalog.push({id, name: id, source: loading ? 'Loading repository…' : 'Repository unavailable', revision: '', search: id});
    }
  }
  const term = query.trim().toLowerCase();
  const options = catalog.filter(p => p.search.toLowerCase().includes(term));
  const active = options.find(p => p.id === activeId) || options[0];
  const count = ids.length + (ids.length === 1 ? ' repository selected' : ' repositories selected');

  function show() {
    const rect = field.current?.getBoundingClientRect();
    if (rect) setAbove(window.innerHeight - rect.bottom < 220 && rect.top > window.innerHeight - rect.bottom);
    setOpen(true);
  }
  function close() {
    setOpen(false);
    setQuery('');
    setActiveId(undefined);
  }
  function toggle(choice: string) {
    if (disabled) return;
    onChange(ids.includes(choice) ? ids.filter(id => id !== choice) : [...ids, choice]);
    setActiveId(choice);
    field.current?.select();
  }
  useEffect(() => {
    if (!open) return;
    const outside = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) close();
    };
    document.addEventListener('pointerdown', outside);
    return () => document.removeEventListener('pointerdown', outside);
  }, [open]);
  useEffect(() => {
    if (open && active) document.getElementById(listId + '-' + active.id)?.scrollIntoView?.({block: 'nearest'});
  }, [open, active?.id, listId]);

  function keyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.nativeEvent.isComposing) return;
    if (!open && e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
      e.preventDefault();
      setQuery(e.key);
      show();
      return;
    }
    switch (e.key) {
      case 'ArrowDown':
      case 'ArrowUp': {
        e.preventDefault();
        const index = options.findIndex(p => p.id === active?.id);
        const next = e.key === 'ArrowDown' ? index + 1 : index - 1;
        const first = e.key === 'ArrowDown' ? 0 : options.length - 1;
        setActiveId(options[open ? Math.max(0, Math.min(options.length - 1, next)) : first]?.id);
        if (!open) show();
        break;
      }
      case 'Enter':
        e.preventDefault();
        if (!open) show();
        else if (active) toggle(active.id);
        break;
      case 'Escape':
        if (open) { e.preventDefault(); e.stopPropagation(); close(); }
        break;
      case 'Tab':
        close();
        break;
    }
  }

  return <div className="help-field repository-picker" ref={root}>
    <div className="field-help-caption">
      <label htmlFor={id}>{label}</label>
      <FieldHelp label={label}>{help}</FieldHelp>
    </div>
    <div className="repository-picker-field">
      <input ref={field} id={id} role="combobox" type="text" disabled={disabled} autoComplete="off"
        aria-autocomplete="list" aria-expanded={open} aria-controls={open ? listId : undefined}
        aria-activedescendant={open && active ? listId + '-' + active.id : undefined}
        aria-describedby={id + '-count'} placeholder={open ? 'Search repositories…' : 'Choose repositories…'}
        value={open ? query : selectedNames.join(', ')} onClick={() => { if (!open) show(); }}
        onChange={e => { setQuery(e.target.value); setActiveId(undefined); if (!open) show(); }}
        onFocus={e => e.currentTarget.select()} onKeyDown={keyDown} onBlur={close} title={selectedNames.join(', ') || undefined}/>
      <span className="repository-picker-end" aria-hidden="true">
        {(ids.length > 1 || open && ids.length === 1) && <span className="repository-picker-count">{ids.length}</span>}
        <button type="button" tabIndex={-1} disabled={disabled} className="repository-picker-arrow"
          aria-label={open ? 'Close list' : 'Open list'} onMouseDown={e => e.preventDefault()}
          onClick={() => { field.current?.focus(); if (open) close(); else show(); }}>
          <ChevronDown size={16} className={open ? 'is-open' : ''}/></button>
      </span>
      {open && !disabled && <div onMouseDown={e => e.preventDefault()} className={'repository-picker-menu' + (above ? ' opens-above' : '')}>
        <ul id={listId} role="listbox" aria-label={label} aria-multiselectable="true">
          {options.map(p => <li key={p.id} id={listId + '-' + p.id} role="option" aria-selected={ids.includes(p.id)}
            className={p.id === active?.id ? 'is-active' : undefined}
            onClick={() => toggle(p.id)}>
            <span className="repository-picker-mark" aria-hidden="true">{ids.includes(p.id) && <Check size={13}/>}</span>
            <span className="repository-picker-info">
              <span className="repository-picker-heading">
                <span className="repository-picker-name">{p.name}</span>
                {p.revision && <small className="repository-picker-revision">{p.revision}</small>}
              </span>
              <small>{p.source}</small>
            </span>
          </li>)}
        </ul>
        {!options.length && <p role="status">{loading ? 'Loading repositories…' : term ? 'No matching repositories.' : 'No registered repositories.'}</p>}
      </div>}
    </div>
    <span className="sr-only" id={id + '-count'} role="status">{count}</span>
  </div>;
}

interface RowProps {
  repos: TaskRepository[];
  projects: Project[];
  connections?: SCMConnection[];
  onChange?: (rows: TaskRepository[]) => void;
}

export function RepositoryRows({repos, projects, connections = [], onChange}: RowProps) {
  function update(id: string, patch: Partial<TaskRepository>) {
    onChange?.(repos.map(row => row.projectId === id ? {...row, ...patch} : row));
  }
  const inputs = repos.map(row => row.alias).join(', ');
  return <div className="repository-rows">{repos.map(row => {
    const project = projects.find(p => p.id === row.projectId);
    const change = (patch: Partial<TaskRepository>) => update(row.projectId, patch);
    const required = row.requiredChecks || [];
    return <section className="repository-row" key={row.projectId} aria-label={project?.name || row.alias}>
      <div className="repository-row-summary">
        <strong>{project?.name || row.projectId}</strong>
        <code>{row.alias}/</code>
        <span>{row.sourceRef || 'HEAD'} @ <code>{row.baseCommit.slice(0, 12)}</code></span>
      </div>
      {!project && onChange && <p className="field-error" role="alert">Repository unavailable. Replace or remove it.</p>}
      <details>
        <summary>Repository details</summary>
        <div className="repository-row-fields">
          <p>Base commit <code>{row.baseCommit}</code></p>
          {onChange && <>
            <label>Base commit<input value={row.baseCommit} onChange={e => change({baseCommit: e.target.value})}/></label>
            {project && <Btn onClick={() => change({
              baseCommit: project.head, sourceRef: project.repositoryRef || 'HEAD', checks: project.checks,
              requiredChecks: required.filter(name => project.checks?.some(c => c.name === name))
            })}>Use current project revision</Btn>}
            <label>Workspace directory<input value={row.alias} onChange={e => change({alias: e.target.value})}/></label>
            <label>PR connection
              <select value={row.scmConnection || ''} onChange={e => {
                const connection = connections.find(c => c.name === e.target.value);
                change({
                  scmConnection: connection?.name, repository: connection?.repository,
                  targetBranch: connection ? (row.targetBranch || project?.repositoryRef || 'main') : undefined,
                  connectionRevision: connection?.updatedAt
                });
              }}>
                <option value="">Local delivery</option>
                {connections.map(c => <option key={c.name} value={c.name}>{c.name} · {c.repository}</option>)}
              </select>
            </label>
            {row.scmConnection && <label>Target branch
              <input value={row.targetBranch || ''} onChange={e => change({targetBranch: e.target.value})}/>
            </label>}
          </>}
          {row.checks?.map(check => <div className="repository-row-check" key={check.name}>
            {onChange ? <label className="check">
              <input type="checkbox" checked={required.includes(check.name)} onChange={e => change({
                requiredChecks: e.target.checked ? [...required, check.name] : required.filter(name => name !== check.name)
              })}/>Require {row.alias}.{check.name}
            </label> : <strong>{row.alias}.{check.name} · {required.includes(check.name) ? 'required' : 'optional'}</strong>}
            <div><code>{check.command}</code> in <code>{row.alias}/{check.cwd || ''}</code> · Inputs: {inputs}</div>
          </div>)}
        </div>
      </details>
      {row.scmConnection && <p>PR → {row.repository} · {row.targetBranch} · {row.scmConnection}</p>}
    </section>;
  })}</div>;
}
