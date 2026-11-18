import { AgentPicker } from './AgentBinding';
import { nodeWithAgent, type AgentBinding, type AgentDefinition } from '../api/agents';
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
import type { SettingsSection } from './graphSettingsSections';
import { ChannelPanel } from './ChannelPanel';
import { Badge, Btn, ErrorBox, Icon, Modal } from './common';
import { EventsView } from './EventsView';
import { FilesView } from './FilesView';
import { IssueSummary } from './GraphIssues';
import { MessagesExplorer } from './MessagesExplorer';
import { NodePanel } from './NodePanel';
import { TaskActivity } from './TaskActivity';
import { YamlEditor } from './YamlEditor';

export type GraphTab = 'canvas' | 'agent' | 'runs' | 'activity' | 'files' | 'events' | 'messages' | 'yaml' | 'settings';

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
  settingsSection = 'general',
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
  settingsSection?: SettingsSection;
  onTab: (t: GraphTab, section?: SettingsSection) => void;
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
  const [catalogPicker, setCatalogPicker] = useState(false);
  const canBindAgent = info?.identity?.role === 'admin';
  const [reviewOpen, setReviewOpen] = useState(false);
  const canEditAgent = info?.identity?.role === 'admin' || info?.identity?.role === 'operator';
  const [saving, setSaving] = useState(false);
  const [validation, setValidation] = useState<Validation>();
  const [panelOpen, setPanelOpen] = useState(true);
  // Set when a start was refused because the environment is not ready.
  const [envBlock, setEnvBlock] = useState<string>();
  const [removalRequest, setRemovalRequest] = useState<{
    nodeIds: string[];
    edges: EdgeRef[];
    channelIds: string[];
  }>();
  const pendingRemoval = removalRequest && planRemoval(draftGraph, removalRequest.nodeIds, removalRequest.edges, removalRequest.channelIds);
  const removalDescription = pendingRemoval && describeRemoval(draftGraph, pendingRemoval);
  const removalOpener = useRef<HTMLElement | null>(null);
  const canvasTab = useRef<HTMLButtonElement>(null);
  const lastSettingsSection = useRef<SettingsSection>('general');
  useEffect(() => {
    if (tab === 'settings') lastSettingsSection.current = settingsSection;
    if (tab !== 'canvas') setRemovalRequest(undefined);
  }, [tab, settingsSection]);
  const appliedSelectionKey = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (appliedSelectionKey.current === selectionKey) return;
    appliedSelectionKey.current = selectionKey;
    if (!selectionKey) return;
    const separator = selectionKey.indexOf(':');
    const kind = selectionKey.slice(0, separator), id = selectionKey.slice(separator + 1);
    if ((kind === 'node' || kind === 'channel') && id) {
      setSelection({kind, id}); setPanelOpen(true);
    }
  }, [selectionKey]);

  const openSettings = (section = lastSettingsSection.current) => onTab('settings', section);
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

  const addNode = (position: { x: number; y: number }, positions: Record<string, { x: number; y: number }>, selection?: {binding:AgentBinding; definition:AgentDefinition}) => {
    let added: string | undefined;
    changeGraph((graph) => {
      const result = addAgent(graph, position, positions);
      added = result.id;
      if (!selection) return result.graph;
      const node = result.graph.spec.nodes.find(n => n.id === result.id);
      return node ? updateNode(result.graph, result.id, nodeWithAgent(node, selection.binding, selection.definition)) : result.graph;
    });
    if (added) {
      setSelection({ kind: 'node', id: added });
      setPanelOpen(true);
    }
  };

  /** Preview all affected elements before editing the draft. */
  const removeElements = (nodeIds: string[], edges: EdgeRef[], channelIds: string[] = []) => {
    if (removalDescription || saving) return;
    removalOpener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    setRemovalRequest({ nodeIds, edges, channelIds });
  };

  const cancelRemoval = () => {
    setRemovalRequest(undefined);
    removalOpener.current?.focus();
  };

  const confirmRemoval = () => {
    if (!pendingRemoval) return;
    setRemovalRequest(undefined);
    canvasTab.current?.focus();
    changeGraph((graph) => applyRemoval(graph, pendingRemoval));
    if (
      (selection?.kind === 'node' && pendingRemoval.nodes.includes(selection.id)) ||
      (selection?.kind === 'channel' && pendingRemoval.channels.includes(selection.id))
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

  const selectedAgent = selection?.kind === 'node' ? draftGraph.spec.nodes.find((node) => node.id === selection.id) : undefined;
  const state = status?.state ?? summary.record.state;
  const running = state === 'running' || state === 'paused' || state === 'stopping';

  return (
    <div className={tab === 'settings' ? 'workspace workspace-settings' : 'workspace'}>
      <div className="toolbar">
        <div className="tabs" role="tablist">
          {(['canvas', 'agent', 'runs', 'activity', 'files', 'events', 'messages', 'yaml', 'settings'] as GraphTab[]).map((t) => (
            <button
              key={t}
              ref={t === 'canvas' ? canvasTab : undefined}
              type="button"
              role="tab"
              aria-selected={tab === t}
              className={tab === t ? 'on' : ''}
              onClick={() => t === 'agent' ? configureAgent(configuredId) : t === 'settings' ? openSettings() : onTab(t)}
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
                      : t === 'settings' ? 'Settings' : 'Events'}
            </button>
          ))}
        </div>
        <div className="toolbar-actions">
          {(tab === 'canvas' || tab === 'yaml' || tab === 'agent' || tab === 'settings') && (
            <>
              {tab === 'canvas' && (
                <Btn small tone="ghost" onClick={() => openSettings()}>
                  Graph settings
                </Btn>
              )}
              {tab === 'canvas' && selectedAgent && (
                <Btn small tone="danger" onClick={() => removeElements([selectedAgent.id], [])} disabled={saving}>
                  Remove {selectedAgent.name || selectedAgent.id}
                </Btn>
              )}
              {tab === 'canvas' && selection?.kind === 'channel' && (
                <Btn small tone="danger" onClick={() => removeElements([], [], [selection.id])} disabled={saving}>
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
      {issues && (tab === 'canvas' || tab === 'yaml' || tab === 'agent' || tab === 'settings') && (
        <IssueSummary
          index={issues}
          onSelect={(target) => {
            setSelection(target);
            setPanelOpen(true);
            onTab('canvas');
          }}
          graphId={gid}
        />
      )}
      {tab === 'canvas' && (
        <div className={`floor ${selection && panelOpen ? 'with-panel' : ''}`}>
          {canBindAgent && <Btn disabled={saving} onClick={()=>setCatalogPicker(true)}>Add from catalog</Btn>}
          <AgentList graph={draftGraph} status={status} statusError={statusError} selected={selection?.kind==='node'?selection.id:undefined}
            onSelect={id=>{setSelection({kind:'node',id});setPanelOpen(true);}} onConfigure={id=>configureAgent(id)}/>
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
              if (s) setPanelOpen(true);
            }}
          />
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
                  onRemove={saving ? undefined : () => removeElements([selection.id], [])}
                  onSaved={(saved) => {
                    setDraftGraph(saved.graph);
                    setDraftDirty(false);
                    onSaved(saved);
                  }}
                  issues={issues?.nodes.get(selection.id)}
                  onUpdateNode={draftGraph.spec.nodes.find(n=>n.id===selection.id)?.agent ? undefined : (nodeId, update) => changeGraph((graph) => updateNode(graph, nodeId, update))}
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
      {tab === 'settings' && (
        <GraphSettings
          section={settingsSection}
          graph={draftGraph}
          savedGraph={summary.graph}
          dirty={draftDirty}
          running={running}
          role={info?.identity?.role}
          issues={issues?.graph}
          onChange={changeGraph}
        />
      )}
      {tab === 'canvas' && pendingRemoval && removalDescription && (
        <Modal title="Confirm removal" onClose={cancelRemoval} keyboard>
          {removalDescription.split('\n').map((line) => <p key={line}>{line}</p>)}
          {pendingRemoval.nodes.length > 0 && (
            <p>Agent {pendingRemoval.nodes.length === 1 ? 'ID' : 'IDs'}: {pendingRemoval.nodes.map((id, index) => (
              <span key={id}>{index > 0 && ', '}<code>{id}</code></span>
            ))}.</p>
          )}
          {pendingRemoval.nodes.length > 0 && pendingRemoval.channels.length === 0 && <p>No channels will be removed.</p>}
          <p>This changes the graph draft. Select <strong>Save graph</strong> to apply the removal, or <strong>Discard</strong> to undo it before saving.</p>
          <div className="btn-row">
            <Btn autoFocus onClick={cancelRemoval}>Cancel</Btn>
            <Btn tone="danger" onClick={confirmRemoval}>Remove</Btn>
          </div>
        </Modal>
      )}
      {tab === 'agent' && <AgentConfiguration summary={currentSummary} nodeId={configuredId} section={agentSection}
        onSection={section => configureAgent(configuredId,section)} onSelect={id => configureAgent(id)}
        onUpdateNode={canEditAgent ? (id,patch) => changeGraph(graph => updateNode(graph,id,patch)) : undefined}
        issues={issues?.nodes.get(configuredId)} dirty={draftDirty} canBind={canBindAgent}/>}
      {catalogPicker && <AgentPicker onCancel={()=>setCatalogPicker(false)} onSelect={(binding,definition)=>{
        setCatalogPicker(false);
        const rightEdge = Math.max(0,...draftGraph.spec.nodes.map(node=>node.position?.x ?? 0));
        addNode({x:rightEdge+280,y:0},{},{binding,definition});
      }}/>}
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
