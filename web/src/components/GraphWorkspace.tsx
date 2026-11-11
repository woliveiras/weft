import { AgentList } from './AgentList';
import { RunsView } from './RunsView';
import type { RunSection } from './runPresentation';
import { AgentConfiguration } from './AgentConfiguration';
import { changedGraphFields, type AgentSection } from './agentSettingsModel';
import { useEffect, useMemo, useRef, useState } from 'react';
import { ApiError, api, errorMessage } from '../api/client';
import type { Graph, GraphAction, GraphSummary, Info, ValidationResult } from '../api/types';
import { useGraphStatus } from '../hooks/useData';
import {
  addAgent,
  applyRemoval,
  connect,
  describeRemoval,
  indexIssues,
  moveAgents,
  planRemoval,
  updateChannel,
  updateNode,
  type EdgeRef,
} from './canvas/edit';
import { GraphCanvas, type Selection } from './canvas/GraphCanvas';
import { EnvironmentBlocker, environmentIssue } from './EnvironmentBlocker';
import { GraphSettings } from './GraphSettings';
import { ChannelPanel } from './ChannelPanel';
import { Badge, Btn, ErrorBox, Icon, Modal } from './common';
import { EventsView } from './EventsView';
import { FilesView } from './FilesView';
import { IssueSummary } from './GraphIssues';
import { MessagesExplorer } from './MessagesExplorer';
import { NodePanel } from './NodePanel';
import { TaskActivity } from './TaskActivity';
import { YamlEditor } from './YamlEditor';

export type GraphTab = 'canvas' | 'agent' | 'runs' | 'activity' | 'files' | 'events' | 'messages' | 'yaml';

/** A validation result and the draft it was computed for (paths are indices into that draft). */
interface Validation {
  graph: Graph;
  result: ValidationResult;
}

function asValidationResult(details: unknown): ValidationResult | undefined {
  const d = details as Partial<ValidationResult> | undefined;
  return d && Array.isArray(d.errors) ? { valid: false, errors: d.errors, warnings: d.warnings ?? [] } : undefined;
}

export function GraphWorkspace({
  summary,
  info,
  tab,
  selectionKey,
  executionId,
  runSection,
  agentId,
  agentSection = 'general',
  onAgent,
  onTab,
  onSaved,
  onDirtyChange,
  correlationId,
  onCorrelation,
}: {
  summary: GraphSummary;
  info?: Info;
  tab: GraphTab;
  selectionKey?: string;
  executionId?: string;
  runSection?: RunSection;
  agentId?: string;
  agentSection?: AgentSection;
  onAgent?: (id:string,section:AgentSection)=>void;
  onTab: (t: GraphTab) => void;
  onSaved: (s: GraphSummary) => void;
  onDirtyChange?: (id:string,dirty:boolean)=>void;
  correlationId?: string;
  onCorrelation: (c: string | undefined) => void;
}) {
  const gid = summary.graph.metadata.id;
  const { status, error: statusError, setStatus } = useGraphStatus(gid);
  const [draftGraph, setDraftGraph] = useState<Graph>(summary.graph);
  const [draftDirty, setDraftDirty] = useState(false);
  const [draftBase, setDraftBase] = useState(summary);
  useEffect(()=>{onDirtyChange?.(gid,draftDirty);},[gid,draftDirty,onDirtyChange]);
  // Deep link: #/g/<id>/canvas?sel=node:<id> or ?sel=channel:<id>
  const [selection, setSelection] = useState<Selection>(() => {
    const sel = new URLSearchParams(location.hash.split('?')[1] ?? '').get('sel');
    const [kind, id] = sel?.split(':') ?? [];
    return (kind === 'node' || kind === 'channel') && id ? { kind, id } : null;
  });
  const [error, setError] = useState<string>();
  const [saveConflict, setSaveConflict] = useState(false);
  const [reviewOpen, setReviewOpen] = useState(false);
  const canEditAgent = info?.identity?.role === 'admin' || info?.identity?.role === 'operator';
  const [saving, setSaving] = useState(false);
  const [validation, setValidation] = useState<Validation>();
  const [panelOpen, setPanelOpen] = useState(true);
  // Set when a start was refused because the environment is not ready.
  const [envBlock, setEnvBlock] = useState<string>();
  // Graph settings show in the side panel while no agent or channel is selected.
  const [settingsOpen, setSettingsOpen] = useState(false);
  const appliedSelectionKey = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (appliedSelectionKey.current === selectionKey) return;
    appliedSelectionKey.current = selectionKey;
    if (!selectionKey) return;
    const separator = selectionKey.indexOf(':');
    const kind = selectionKey.slice(0, separator), id = selectionKey.slice(separator + 1);
    if ((kind === 'node' || kind === 'channel') && id) {
      setSelection({kind, id}); setSettingsOpen(false); setPanelOpen(true);
    }
  }, [selectionKey]);

  const openSettings = () => {
    setSelection(null);
    setSettingsOpen(true);
    setPanelOpen(true);
  };
  const configureAgent = (id:string, section:AgentSection = 'general') => {
    setSelection(id ? {kind:'node',id} : null);
    if (onAgent) onAgent(id,section); else onTab('agent');
  };
  const configuredId = agentId ?? (selection?.kind === 'node' ? selection.id : '');
  const currentSummary = draftDirty ? { ...draftBase, graph: draftGraph } : summary;
  const issues = useMemo(
    () => validation && indexIssues(validation.graph, validation.result.errors ?? [], validation.result.warnings ?? []),
    [validation],
  );
  const hasErrors = (issues?.errorCount ?? 0) > 0;

  useEffect(() => {
    if (!draftDirty) setDraftGraph(summary.graph);
  }, [summary.graph, draftDirty]);

  // While errors are shown, re-check the draft after each pause in editing,
  // so fixed problems disappear without another save attempt.
  useEffect(() => {
    if (!hasErrors || validation?.graph === draftGraph) return;
    let cancelled = false;
    const timer = setTimeout(() => {
      api
        .validateGraph(draftGraph)
        .then((result) => {
          if (!cancelled) setValidation({ graph: draftGraph, result });
        })
        .catch((cause) => {
          if (!cancelled) setError(errorMessage(cause));
        });
    }, 500);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [draftGraph, hasErrors, validation]);

  const changeGraph = (update: (graph: Graph) => Graph) => {
    if (!draftDirty) setDraftBase(summary);
    setDraftGraph((current) => update(current));
    setDraftDirty(true);
    setError(undefined);
  };

  const resetDraft = (graph: Graph) => {
    setDraftGraph(graph);
    setDraftDirty(false);
    setSaveConflict(false);
    setValidation(undefined);
    setError(undefined);
  };

  const validateDraft = async () => {
    const candidate = draftGraph;
    try {
      const result = await api.validateGraph(candidate);
      setValidation({ graph: candidate, result });
      setError(undefined);
      return result;
    } catch (cause) {
      setError(errorMessage(cause));
      return undefined;
    }
  };

  /** Saves the draft over `version` (the version the draft was based on, or the latest when overwriting). */
  const saveGraph = async (version = draftDirty ? draftBase.record.version : summary.record.version) => {
    const candidate = draftGraph;
    setSaving(true);
    try {
      const result = await validateDraft();
      if (!result?.valid) return;
      const saved = await api.updateGraph(gid, candidate, version);
      setDraftGraph(saved.graph);
      setDraftDirty(false);
      setSaveConflict(false);
      // Warnings stay visible after a save; the saved document has the same indices.
      setValidation((result.warnings ?? []).length ? { graph: candidate, result } : undefined);
      onSaved(saved);
      setError(undefined);
    } catch (e) {
      const details = e instanceof ApiError && e.status === 422 ? asValidationResult(e.details) : undefined;
      if (details) {
        setValidation({ graph: candidate, result: details });
        setError(undefined);
      } else {
        setError(errorMessage(e));
      }
      setSaveConflict(e instanceof ApiError && e.status === 409);
    } finally {
      setSaving(false);
    }
  };

  const reloadLatest = async () => {
    if (!confirm('Reload the latest saved graph and discard your local edits?')) return;
    try {
      const latest = await api.getGraph(gid);
      resetDraft(latest.graph);
      onSaved(latest);
    } catch (cause) {
      setError(errorMessage(cause));
    }
  };

  const overwriteLatest = async () => {
    if (!confirm('Save your local edits over the latest version? Changes made there since you started editing are replaced.'))
      return;
    try {
      const latest = await api.getGraph(gid);
      await saveGraph(latest.record.version);
    } catch (cause) {
      setError(errorMessage(cause));
    }
  };

  const addNode = (position: { x: number; y: number }, positions: Record<string, { x: number; y: number }>) => {
    let added: string | undefined;
    changeGraph((graph) => {
      const result = addAgent(graph, position, positions);
      added = result.id;
      return result.graph;
    });
    if (added) {
      setSelection({ kind: 'node', id: added });
      setPanelOpen(true);
    }
  };

  /** Removes agents/edges after the user confirms what that does to the document. */
  const removeElements = (nodeIds: string[], edges: EdgeRef[], channelIds: string[] = []) => {
    const removal = planRemoval(draftGraph, nodeIds, edges, channelIds);
    const message = describeRemoval(draftGraph, removal);
    if (!message || !confirm(message)) return;
    changeGraph((graph) => applyRemoval(graph, removal));
    if (
      (selection?.kind === 'node' && removal.nodes.includes(selection.id)) ||
      (selection?.kind === 'channel' && removal.channels.includes(selection.id))
    ) {
      setSelection(null);
    }
  };

  const act = async (a: GraphAction) => {
    if (a === 'kill' && !confirm('Kill every turn in flight? Inputs are redelivered; files may be left mid-edit.')) return;
    if (a === 'start' && draftDirty && !confirm(`Start saved version ${summary.record.version}? Your unsaved graph changes are not included.`))
      return;
    try {
      setStatus(await api.graphAction(gid, a));
      setError(undefined);
      setEnvBlock(undefined);
    } catch (e) {
      const blocked = a === 'start' ? environmentIssue(e) : undefined;
      if (blocked) {
        setEnvBlock(blocked);
        setError(undefined);
      } else {
        setError(errorMessage(e));
      }
    }
  };

  const state = status?.state ?? summary.record.state;
  const running = state === 'running' || state === 'paused' || state === 'stopping';

  return (
    <div className="workspace">
      <div className="toolbar">
        <div className="tabs" role="tablist">
          {(['canvas', 'agent', 'runs', 'activity', 'files', 'events', 'messages', 'yaml'] as GraphTab[]).map((t) => (
            <button
              key={t}
              type="button"
              role="tab"
              aria-selected={tab === t}
              className={tab === t ? 'on' : ''}
              onClick={() => t === 'agent' ? configureAgent(configuredId) : onTab(t)}
            >
              {t === 'canvas'
                ? 'Graph'
                : t === 'agent' ? 'Agent configuration'
                : t === 'runs' ? 'Runs'
                : t === 'activity'
                  ? 'Activity'
                  : t === 'files'
                  ? 'Files'
                  : t === 'yaml'
                    ? 'Definition'
                    : t === 'messages'
                      ? 'Messages'
                      : 'Events'}
            </button>
          ))}
        </div>
        <div className="toolbar-actions">
          {(tab === 'canvas' || tab === 'yaml' || tab === 'agent') && (
            <>
              {tab === 'canvas' && (
                <Btn small tone="ghost" onClick={openSettings} aria-pressed={settingsOpen && !selection}>
                  Graph settings
                </Btn>
              )}
              {tab === 'canvas' && selection?.kind === 'node' && (
                <Btn small tone="danger" onClick={() => removeElements([selection.id], [])}>
                  Remove agent
                </Btn>
              )}
              {tab === 'canvas' && selection?.kind === 'channel' && (
                <Btn small tone="danger" onClick={() => removeElements([], [], [selection.id])}>
                  Remove channel
                </Btn>
              )}
              {saving ? (
                <Badge>saving…</Badge>
              ) : (
                draftDirty && <Badge tone="warn">unsaved graph changes</Badge>
              )}
              {saveConflict && (
                <>
                  <Btn small tone="danger" onClick={() => void reloadLatest()}>
                    Reload latest
                  </Btn>
                  <Btn small onClick={() => void overwriteLatest()}>
                    Keep mine and overwrite
                  </Btn>
                </>
              )}
              <Btn small tone="ghost" onClick={() => void validateDraft()} disabled={saving}>
                Validate
              </Btn>
              <Btn small onClick={() => tab === 'agent' ? setReviewOpen(true) : void saveGraph()} disabled={!draftDirty || saving || (tab === 'agent' && !canEditAgent)} tone="primary">
                {tab === 'agent' ? 'Review & save' : 'Save graph'}
              </Btn>
              <Btn small tone="ghost" disabled={!draftDirty || saving} onClick={() => resetDraft(summary.graph)}>
                Discard
              </Btn>
            </>
          )}
          <div className="toolbar-run-controls" hidden={tab === 'agent' || tab === 'runs'}>
            <Badge tone={state === 'running' ? 'ok' : state === 'failed' ? 'bad' : state === 'paused' ? 'warn' : undefined}>
              {state}
            </Badge>
            {status && status.desiredState !== status.state && <span className="muted small">→ {status.desiredState}</span>}
            <span className="muted small">v{summary.record.version}</span>
            {(status?.approvalsPending ?? 0) > 0 && <Badge tone="warn">{status?.approvalsPending} approvals</Badge>}
            <Btn small tone="primary" onClick={() => act('start')} disabled={state === 'running'} title="Run all">
              <Icon name="play" /> Run all
            </Btn>
            <Btn small onClick={() => act('stop')} disabled={!running} title="Stop all after current turns">
              <Icon name="stop" /> Stop all
            </Btn>
            {state === 'paused' ? (
              <Btn small onClick={() => act('resume')}>
                Resume
              </Btn>
            ) : (
              <Btn small onClick={() => act('pause')} disabled={state !== 'running'}>
                <Icon name="pause" /> Pause
              </Btn>
            )}
            <Btn
              small
              tone="danger"
              onClick={() => act('kill')}
              disabled={!running}
              title="Kill all turns in flight"
              aria-label="Kill"
            >
              <Icon name="bolt" />
            </Btn>
          </div>
        </div>
      </div>
      <ErrorBox error={error ?? statusError} />
      {envBlock && (
        <EnvironmentBlocker
          graphId={gid}
          message={envBlock}
          canBuild={info?.identity?.role === 'operator' || info?.identity?.role === 'admin'}
          onStarted={setStatus}
          onDismiss={() => setEnvBlock(undefined)}
        />
      )}
      {saveConflict && (
        <p className="muted small" role="status">
          Someone saved a newer version while you were editing. Your edits are kept here: reload to start from the latest
          version, or overwrite it with yours.
        </p>
      )}
      {issues && (tab === 'canvas' || tab === 'yaml' || tab === 'agent') && (
        <IssueSummary
          index={issues}
          onSelect={(target) => {
            setSelection(target);
            setPanelOpen(true);
            onTab('canvas');
          }}
          onOpenSettings={() => {
            openSettings();
            onTab('canvas');
          }}
        />
      )}
      {tab === 'canvas' && (
        <div className={`floor ${(selection || settingsOpen) && panelOpen ? 'with-panel' : ''}`}>
          <AgentList graph={draftGraph} status={status} statusError={statusError} selected={selection?.kind==='node'?selection.id:undefined}
            onSelect={id=>{setSelection({kind:'node',id});setSettingsOpen(false);setPanelOpen(true);}} onConfigure={id=>configureAgent(id)}/>
          <GraphCanvas
            graph={draftGraph}
            status={status}
            selection={selection}
            editable
            onAddNode={addNode}
            issues={issues}
            onConnect={(connection) => {
              if (!connection.source || !connection.target) return;
              let added: string | undefined;
              changeGraph((graph) => {
                const result = connect(graph, connection.source, connection.target);
                added = result.id;
                return result.graph;
              });
              if (added) {
                setSelection({ kind: 'channel', id: added });
                setPanelOpen(true);
              }
            }}
            onNodePositionChange={(positions) => changeGraph((graph) => moveAgents(graph, positions))}
            onRemove={removeElements}
            onSelect={(s) => {
              setSelection(s);
              setSettingsOpen(false);
              if (s) setPanelOpen(true);
            }}
          />
          {!selection && settingsOpen && panelOpen && (
            <aside className="side-panel" aria-label="Graph settings">
              <button type="button" className="icon-btn panel-close" onClick={() => setSettingsOpen(false)} aria-label="Close panel">
                <Icon name="x" />
              </button>
              <GraphSettings
                graph={draftGraph}
                savedGraph={summary.graph}
                dirty={draftDirty}
                running={running}
                role={info?.identity?.role}
                issues={issues?.graph}
                onChange={changeGraph}
              />
            </aside>
          )}
          {selection && panelOpen && (
            <aside className="side-panel" aria-label="Details">
              <button type="button" className="icon-btn panel-close" onClick={() => setPanelOpen(false)} aria-label="Close panel">
                <Icon name="x" />
              </button>
              {selection.kind === 'node' ? (
                <NodePanel
                  summary={currentSummary}
                  status={status}
                  nodeId={selection.id}
                  onConfigure={() => configureAgent(selection.id)}
                  onSaved={(saved) => {
                    setDraftGraph(saved.graph);
                    setDraftDirty(false);
                    onSaved(saved);
                  }}
                  issues={issues?.nodes.get(selection.id)}
                  onUpdateNode={(nodeId, update) => changeGraph((graph) => updateNode(graph, nodeId, update))}
                  onStatus={setStatus}
                />
              ) : (
                <ChannelPanel
                  summary={currentSummary}
                  status={status}
                  channelId={selection.id}
                  issues={issues?.channels.get(selection.id)}
                  onUpdateChannel={(channelId, update) => changeGraph((graph) => updateChannel(graph, channelId, update))}
                  onShowCorrelation={(c) => {
                    onCorrelation(c);
                    onTab('messages');
                  }}
                />
              )}
            </aside>
          )}
        </div>
      )}
      {tab === 'agent' && <AgentConfiguration summary={currentSummary} nodeId={configuredId} section={agentSection}
        onSection={section => configureAgent(configuredId,section)} onSelect={id => configureAgent(id)}
        onUpdateNode={canEditAgent ? (id,patch) => changeGraph(graph => updateNode(graph,id,patch)) : undefined}
        issues={issues?.nodes.get(configuredId)} dirty={draftDirty}/>}
      {reviewOpen && <Modal title="Review graph changes" onClose={() => !saving && setReviewOpen(false)}>
        <p>Save the entire graph draft based on version {draftBase.record.version}, including other agents, channels and graph settings.</p>
        <p>Changed fields are listed below without copying their values. Review the relevant sections or Definition for their contents.</p>
        <ul className="graph-change-list">{changedGraphFields(draftBase.graph,draftGraph).map(path => <li key={path}><code>{path}</code></li>)}</ul>
        <p>Server validation and version conflict checks run before saving. Tool grants remain explicit; OAuth consent does not grant tools.</p>
        <Btn tone="primary" disabled={saving || !canEditAgent} onClick={() => {setReviewOpen(false); void saveGraph();}}>Save graph draft</Btn>
        <Btn disabled={saving} onClick={() => setReviewOpen(false)}>Continue editing</Btn>
      </Modal>}
      {tab === 'runs' && <RunsView key={gid} graphId={gid} executionId={executionId} section={runSection} role={info?.identity?.role}/>}
      {tab === 'activity' && <TaskActivity key={gid} graphId={gid} role={info?.identity?.role} />}
      {tab === 'events' && <EventsView graph={summary.graph} info={info} />}
      {tab === 'files' && <FilesView summary={summary} status={status} />}
      {tab === 'messages' && (
        <MessagesExplorer graph={summary.graph} info={info} correlationId={correlationId} onCorrelation={onCorrelation} />
      )}
      {tab === 'yaml' && (
        <YamlEditor
          summary={summary}
          draft={draftGraph}
          dirty={draftDirty}
          onChange={(graph) => changeGraph(() => graph)}
          onLoadVersion={(graph) => changeGraph(() => graph)}
          onSave={() => {
            if (draftDirty && !saving) void saveGraph();
          }}
        />
      )}
    </div>
  );
}
