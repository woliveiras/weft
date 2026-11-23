import {api} from '../api/client';
import type {TaskDraft} from '../api/tasks';
import {useAsync} from '../hooks/useData';
import {Btn, ErrorBox} from './common';
import {FilesView} from './FilesView';
import {RunsView} from './RunsView';
import type {RunSection} from './runPresentation';

/** A task copy is inspected through its owning task, never opened as an editable workflow. */
export function taskOwnerId(graphId: string): string | undefined {
  const prefix = 'task-run-';
  return graphId.startsWith(prefix) && graphId.length > prefix.length ? `tsk_${graphId.slice(prefix.length)}` : undefined;
}

export function ownsTaskExecution(task: TaskDraft, graphId: string): boolean {
  return task.id === taskOwnerId(graphId) && task.executionGraphId === graphId;
}

export function TaskExecutionPage({graphId, tab, executionId, section}: {
  graphId: string;
  tab: string;
  executionId?: string;
  section?: RunSection;
}) {
  const taskId = taskOwnerId(graphId);
  const owner = useAsync(() => taskId ? api.task(taskId) : Promise.resolve(undefined), [taskId]);
  const task = owner.data;
  const valid = !!task && ownsTaskExecution(task, graphId);
  return <section className="page installation-page" aria-label="Task execution">
    <a className="task-back-link" href={taskId ? `#/tasks/${encodeURIComponent(taskId)}` : '#/tasks'}>Back to task</a>
    <header className="page-head"><div><h1>Task execution</h1><p className="muted">{valid ? task.title : 'Inspect recorded execution evidence for this task.'}</p></div></header>
    {owner.loading && <p role="status">Loading task execution…</p>}
    <ErrorBox error={owner.error}/>
    {owner.error && <Btn onClick={() => void owner.reload()}>Retry task execution</Btn>}
    {!owner.loading && !owner.error && !valid && <p role="alert">This execution copy does not belong to the selected task.</p>}
    {!owner.loading && !owner.error && valid && <>
      <nav className="section-navigation" aria-label="Task execution sections">
        <a href={`#/g/${encodeURIComponent(graphId)}/runs`} aria-current={tab === 'runs' ? 'page' : undefined}>Executions</a>
        <a href={`#/g/${encodeURIComponent(graphId)}/files`} aria-current={tab === 'files' ? 'page' : undefined}>Workspace files</a>
      </nav>
      {tab === 'runs' ? <RunsView graphId={graphId} executionId={executionId} section={section} role="viewer"/>
        : tab === 'files' ? <TaskExecutionFiles key={graphId} graphId={graphId}/>
        : <p>This task-owned copy has no workflow editor. <a href={`#/tasks/${encodeURIComponent(task.id)}?tab=settings`}>View task settings and access</a>.</p>}
    </>}
  </section>;
}

function TaskExecutionFiles({graphId}: {graphId: string}) {
  const graph = useAsync(() => api.getGraph(graphId), [graphId]);
  const status = useAsync(() => api.status(graphId), [graphId]);
  const missingNodes = graph.data?.graph.spec.nodes.filter(node => node.workspace?.mode === 'worktree' && !status.data?.nodes?.some(item => item.nodeId === node.id)) ?? [];
  return <section aria-label="Task workspace files">
    {(graph.loading || status.loading) && <p role="status">Loading task workspace…</p>}
    <ErrorBox error={graph.error}/>
    {graph.error && <Btn onClick={() => void graph.reload()}>Retry workspace</Btn>}
    <ErrorBox error={status.error && `Workspace replica inventory unavailable: ${status.error}`}/>
    {status.error && <><p>Weft cannot determine which replica workspaces are available. No replica is assumed for this historical task.</p><Btn onClick={() => void status.reload()}>Retry workspace inventory</Btn></>}
    {!graph.loading && !status.loading && !graph.error && !status.error && graph.data && status.data && (graph.data.graph.metadata.id !== graphId
      ? <p role="alert">The workspace response does not match this execution copy.</p>
      : status.data.graphId !== graphId ? <p role="alert">The workspace inventory does not match this execution copy.</p>
      : missingNodes.length ? <><p role="alert">Workspace replica inventory is incomplete for {missingNodes.map(node => node.name || node.id).join(', ')}. No replica is assumed.</p><Btn onClick={() => void status.reload()}>Retry workspace inventory</Btn></>
      : <FilesView summary={graph.data} status={status.data}/>)}
  </section>;
}
