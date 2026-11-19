import { useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { Project } from '../api/tasks';
import { Btn, ErrorBox } from './common';
import { HelpField } from './FieldHelp';

export function ProjectPreference({ project, canEdit, nativeProfile, onSaved }: {
  project: Project;
  canEdit: boolean;
  nativeProfile: boolean;
  onSaved: () => void;
}) {
  const [mode, setMode] = useState(project.executionMode ?? 'workflow');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const save = async () => {
    setBusy(true);
    setError(undefined);
    try {
      await api.saveProjectPreference(project.id, project.revision, mode);
      onSaved();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  return <section className="project-preference-fields" aria-label={`Execution preference for ${project.name}`}>
    <HelpField label="Preferred execution environment" help="Select a project default for future tasks when project preferences are enabled. A workflow or explicit task choice may supply a different environment.">
      <select disabled={!canEdit || busy} value={mode} onChange={event => setMode(event.target.value as typeof mode)}>
        <option value="workflow">Use workflow environment</option>
        <option value="local" disabled={!nativeProfile}>Local worktree</option>
        <option value="sandbox">Sandbox</option>
      </select>
    </HelpField>
    <p className="muted small">Suggested for new tasks. Each task requires compatible workflow settings and explicit review.</p>
    <ErrorBox error={error} />
    <Btn disabled={!canEdit || busy || mode === (project.executionMode ?? 'workflow')} onClick={() => void save()}>
      {busy ? 'Saving…' : 'Save environment preference'}
    </Btn>
  </section>;
}
