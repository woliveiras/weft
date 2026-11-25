import {useEffect, useId, useRef, useState} from 'react';
import {createPortal} from 'react-dom';
import {api, ApiError, errorMessage} from '../api/client';
import type {TaskDraft} from '../api/tasks';
import {Btn, ErrorBox} from './common';

/** Display eligibility only; the server atomically enforces the same boundary. */
export function canDeleteTaskDraft(task: TaskDraft | undefined): boolean {
  return !!task && task.status === 'draft' && !task.executionGraphId && !task.environmentApprovalId;
}

export function TaskDraftDelete({task, canEdit, disabled = false, onDeleted}: {
  task: TaskDraft;
  canEdit: boolean;
  disabled?: boolean;
  onDeleted: () => void;
}) {
  const [reviewed, setReviewed] = useState<TaskDraft>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const pending = useRef(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const dialog = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const descriptionId = useId();
  useEffect(() => {
    if (reviewed && dialog.current && !dialog.current.open) dialog.current.showModal();
  }, [reviewed]);
  const close = () => {
    if (pending.current) return;
    setReviewed(undefined);
    setError(undefined);
    trigger.current?.focus();
  };
  const remove = async () => {
    if (!reviewed || pending.current || !canEdit || disabled || !canDeleteTaskDraft(task)) return;
    pending.current = true;
    setBusy(true);
    setError(undefined);
    try {
      // Use the reviewed revision, even if a background refresh changes the prop.
      await api.deleteTask(reviewed.id, reviewed.revision);
      setReviewed(undefined);
      onDeleted();
    } catch (cause) {
      setError(cause instanceof ApiError && cause.status === 409
        ? 'This draft changed or execution has already been prepared. Cancel, refresh the task and review it before trying again.'
        : `Could not confirm deletion. ${errorMessage(cause)} Check Tasks or refresh the task before trying again.`);
    } finally {
      pending.current = false;
      setBusy(false);
    }
  };
  if (!canEdit || !canDeleteTaskDraft(task)) return null;
  return <>
    <button ref={trigger} type="button" className="btn btn-danger" disabled={disabled || busy}
      onClick={() => {setError(undefined); setReviewed({...task});}}>Delete draft</button>
    {reviewed && createPortal(<dialog ref={dialog} className="field-help-dialog" aria-labelledby={titleId}
      aria-describedby={descriptionId} onClose={close} onCancel={event => {event.preventDefault(); close();}}>
      <h2 id={titleId}>Delete task draft?</h2>
      <div id={descriptionId}>
        <p>Delete <strong>{reviewed.title || reviewed.id}</strong> from Tasks? This cannot be undone.</p>
        <p>Repositories, imports and workspaces are retained. Tasks with prepared or started execution cannot be deleted.</p>
      </div>
      <ErrorBox error={error}/>
      {busy && <p role="status">Deleting draft…</p>}
      <div className="row">
        <Btn autoFocus disabled={busy} onClick={close}>Cancel</Btn>
        <Btn tone="danger" disabled={busy || disabled} onClick={() => void remove()}>{busy ? 'Deleting…' : 'Delete draft permanently'}</Btn>
      </div>
    </dialog>, document.body)}
  </>;
}
