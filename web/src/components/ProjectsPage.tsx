import { useRef, useState } from 'react';
import { api, errorMessage } from '../api/client';
import { useAsync } from '../hooks/useData';
import { Btn, Empty, ErrorBox, fmtDateTime } from './common';
import { HelpField } from './FieldHelp';
import { ProjectPreference } from './ProjectPreference';
import { RepositoryImport } from './RepositoryImport';

export type ProjectView = 'register' | 'import';

const environmentNames = {
  workflow: 'Workflow environment',
  local: 'Local worktree',
  sandbox: 'Sandbox',
};

export function ProjectsPage({ canEdit, nativeProfile = false, mode }: {
  canEdit: boolean;
  nativeProfile?: boolean;
  mode?: ProjectView;
}) {
  const list = useAsync(api.projects, []);
  const [search, setSearch] = useState('');
  const [environment, setEnvironment] = useState('');
  const searchField = useRef<HTMLInputElement>(null);
  const query = search.trim().toLowerCase();
  const matches = (list.data ?? []).filter(project =>
    (!environment || (project.executionMode ?? 'workflow') === environment) &&
    [project.name, project.id, project.path, project.repositoryUrl ?? '', project.head]
      .some(value => value.toLowerCase().includes(query)),
  );

  if (mode) return <section className="page installation-page projects-page">
    <header className="page-head connection-detail-head">
      <div><a href="#/projects">Projects</a><h1>Add project</h1>
        <p className="muted">Import a repository or register a checkout available to the Weft server.</p>
      </div>
    </header>
    <nav className="section-navigation" aria-label="Project setup">
      <a href="#/projects/new" aria-current={mode === 'register' ? 'page' : undefined}>Register repository</a>
      <a href="#/projects/import" aria-current={mode === 'import' ? 'page' : undefined}>Import from URL</a>
    </nav>
    {mode === 'import'
      ? <RepositoryImport canEdit={canEdit} nativeProfile={nativeProfile} onProject={() => { location.hash = '#/projects'; }} />
      : <RegisterProject canEdit={canEdit} />}
  </section>;

  return <section className="page installation-page projects-page" aria-label="Projects">
    <header className="page-head">
      <div><h1>Projects</h1><p className="muted">Select a registered repository when creating a task.</p></div>
      {canEdit && <a className="btn btn-primary" href="#/projects/new">Add project</a>}
    </header>
    <section className="card connections-panel" aria-label="Registered projects">
      <header className="connections-panel-head">
        <div><h2>Registered repositories</h2><p className="muted">Each project pins a Git commit and can suggest an execution environment for new tasks.</p></div>
        <Btn disabled={list.loading} onClick={() => void list.reload()}>Refresh projects</Btn>
      </header>
      <div className="connection-filter-fields">
        <label>Search projects<input ref={searchField} type="search" placeholder="Name, repository, ID or commit"
          value={search} onChange={event => setSearch(event.target.value)} /></label>
        <label>Execution environment<select value={environment} onChange={event => setEnvironment(event.target.value)}>
          <option value="">All environments</option>
          {Object.entries(environmentNames).map(([value, title]) => <option key={value} value={value}>{title}</option>)}
        </select></label>
        {(search || environment) && <Btn tone="ghost" onClick={() => {
          setSearch('');
          setEnvironment('');
          searchField.current?.focus();
        }}>Clear filters</Btn>}
      </div>
      <ErrorBox error={list.error} />
      <p className="small muted connections-count" role="status">
        {list.loading ? 'Loading projects…' : list.error ? '' : list.data?.length ? `${matches.length} of ${list.data.length} projects` : ''}
      </p>
      {!list.loading && !list.error && list.data?.length === 0 && <Empty>No projects registered yet.</Empty>}
      {!list.loading && !!list.data?.length && matches.length === 0 && <Empty>No projects match these filters.</Empty>}
      {matches.length > 0 && <div className="connection-table projects-table" role="region" aria-label="Project list" tabIndex={0}>
        <table><caption className="sr-only">Registered projects</caption>
          <thead><tr>
            <th scope="col">Project</th><th scope="col">Repository</th><th scope="col">Registered commit</th>
            <th scope="col">Execution environment</th><th scope="col">Registered</th><th scope="col">Actions</th>
          </tr></thead>
          <tbody>{matches.map(project => <tr key={project.id}>
            <td><strong>{project.name}</strong><code className="small muted connection-meta">{project.id}</code></td>
            <td>{project.repositoryUrl && <span className="project-repository-url">{project.repositoryUrl}</span>}
              <details className="project-location"><summary>Server path</summary><code>{project.path}</code></details>
              {project.repositoryRef && <span className="small muted connection-meta">{project.repositoryRef}</span>}
            </td>
            <td><code>{project.head}</code></td>
            <td>{environmentNames[project.executionMode ?? 'workflow']}
              <details className="project-preference">
                <summary>{canEdit ? 'Configure preference' : 'View preference'}</summary>
                <ProjectPreference key={`${project.id}:${project.revision}`} project={project} canEdit={canEdit}
                  nativeProfile={nativeProfile} onSaved={() => void list.reload()} />
              </details>
            </td>
            <td>{project.createdAt ? <time dateTime={project.createdAt}>{fmtDateTime(project.createdAt)}</time> : '—'}</td>
            <td><a href="#/tasks/new">Create task</a></td>
          </tr>)}</tbody>
        </table>
      </div>}
    </section>
  </section>;
}

function RegisterProject({ canEdit }: { canEdit: boolean }) {
  const [name, setName] = useState('');
  const [path, setPath] = useState('');
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  const save = async () => {
    if (!canEdit || !confirmed || busy) return;
    const route = location.hash;
    setBusy(true);
    setError(undefined);
    try {
      await api.createProject({ name, path });
      if (location.hash === route) location.hash = '#/projects';
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  return <form className="card form project-registration" onSubmit={event => { event.preventDefault(); void save(); }} aria-label="Register project">
    <h2>Register repository</h2>
    <p className="muted">Choose an existing Git checkout on the Weft server. For Docker installations, use its path inside the container.</p>
    {!canEdit && <p>An administrator must register repositories.</p>}
    <fieldset disabled={!canEdit || busy}>
      <label>Project name<input value={name} required maxLength={200} onChange={event => setName(event.target.value)} /></label>
      <HelpField label="Repository path" help="An absolute path to an existing Git repository on the Weft server. For a Docker installation, use the mounted path inside the container.">
        <input value={path} required maxLength={4096} placeholder="/absolute/path/to/repository" onChange={event => {
          setPath(event.target.value);
          setConfirmed(false);
        }} />
      </HelpField>
      <label className="check project-registration-consent"><input type="checkbox" checked={confirmed}
        onChange={event => setConfirmed(event.target.checked)} />
        <span>I selected this repository on the Weft server and authorize reading its Git metadata.</span>
      </label>
      <p className="muted">Tasks start from the registered commit in a separate worktree. Review the workflow’s execution environment and permissions before starting work.</p>
    </fieldset>
    <ErrorBox error={error} />
    <div className="btn-row">
      <Btn type="submit" tone="primary" disabled={!canEdit || busy || !confirmed}>{busy ? 'Registering project…' : 'Register project'}</Btn>
      {busy ? <Btn disabled>Cancel</Btn> : <a href="#/projects">Cancel</a>}
    </div>
    <a className="small" href="https://woliveiras.com/posts/why-git-worktrees-matter-coding-agents/" target="_blank" rel="noreferrer">Learn about worktrees</a>
  </form>;
}
