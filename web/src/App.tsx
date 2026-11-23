import {parseSkillsRoute,skillsHash,type SkillsRoute} from './components/skillsNavigation';
import { parseModelsRoute, modelsHash, type ModelsRoute } from './components/modelsNavigation';
import { AgentsPage } from './components/AgentsPage';
import {ProjectsPage, type ProjectView} from './components/ProjectsPage';
import { ActivityPage } from './components/ActivityPage';
import { activityHash, parseActivityState, type ActivityRouteState } from './components/activityView';
import { TasksPage } from './components/TasksPage';
import { TaskExecutionPage, taskOwnerId } from './components/TaskExecutionPage';
import { PrimaryNavigation } from './components/PrimaryNavigation';
import { runSection, type RunSection } from './components/runPresentation';
import { agentSection, type AgentSection } from './components/agentSettingsModel';
import { Activity, useCallback, useEffect, useRef, useState } from 'react';
import { api, errorMessage, getToken, MOCK, onUnauthorized, setToken } from './api/client';
import type { GraphSummary } from './api/types';
import { ApprovalsView } from './components/ApprovalsView';
import { Badge, Btn, ErrorBox, Icon, Modal } from './components/common';
import { GraphSelector } from './components/GraphSelector';
import { GraphList } from './components/GraphList';
import { GraphWorkspace, type GraphTab } from './components/GraphWorkspace';
import { settingsPath, settingsSection, type SettingsSection } from './components/graphSettingsSections';
import { AdministrationPage, administrationSections, ConnectionsPage, ModelsPage, type AdministrationSection } from './components/InstallationPages';
import { scmCreateLink } from './components/SCMConnectionsSection';
import { connectionTabs, type ConnectionTab } from './components/mcpNavigation';
import { SkillsView } from './components/SkillsView';
import { useAsync, useThrottled } from './hooks/useData';
import { EventStreamProvider, useEventStream } from './hooks/useEventStream';
import { BrandMark } from './components/BrandMark';
import { ThemeSwitcher } from './components/ThemeSwitcher';

/**
 * Routes live in the hash so a reload (or a shared link) returns to the same
 * place without server-side routing:
 *   #/activity[?window=&status=&workflow=&agent=&focus=&tab=health]
 *   #/graphs  #/approvals  #/connections  #/models  #/administration/<section>
 * Legacy #/integrations links remain MCP aliases, including OAuth returns.
 *   #/g/<id>/<tab>[?c=<correlation id>]  #/g/<id>/settings/<section>
 */
type Route =
  | { view: 'graphs' }
  | { view: 'agents'; agentId?: string; create?: boolean }
  | { view: 'activity'; activity: ActivityRouteState }
  | { view: 'projects'; projectView?: ProjectView }
  | { view: 'tasks'; taskId?: string; create?: boolean }
  | { view: 'approvals' }
  | { view: 'connections'; connectionID?: string; connectionTab?: ConnectionTab; create?: boolean; sourceControlCreate?: boolean; issueEdition?: 'jira'; issueConnectionCreate?: boolean; issueConnectionID?: string; issueTransitions?: boolean; issueMapping?: {name?: string; create?: boolean}; history?: boolean; connectQuery?: string; oauthQuery?: string }
  | ({ view: 'models' } & ModelsRoute)
  | { view: 'administration'; section: AdministrationSection; profileId?:string; profileCreate?:boolean }
  | { view: 'skills'; skills?: SkillsRoute }
  | { view: 'graph'; id: string; tab: GraphTab; settingsSection?: SettingsSection; selectionKey?: string; agentId?: string; agentSection?: AgentSection; executionId?: string; runSection?: RunSection; correlationId?: string; connectionOrigin?: string };

export function parseRoute(hash: string): Route {
  const [path, qs] = hash.replace(/^#/, '').split('?');
  const parts = path.split('/').filter(Boolean);
  if (parts[0] === 'activity') return { view: 'activity', activity: parseActivityState(qs) };
  if (parts[0] === 'agents') {
    return {view:'agents', ...(parts[1] === 'new' ? {create:true} : parts[1] ? {agentId:decodeURIComponent(parts[1])} : {})};
  }
  if (parts[0] === 'projects') return {
    view: 'projects',
    ...(parts[1] === 'new' ? { projectView: 'register' as const } : parts[1] === 'import' ? { projectView: 'import' as const } : {}),
  };
  if (parts[0] === 'tasks') return {view:'tasks', ...(parts[1] === 'new' ? {create:true} : parts[1] ? {taskId:decodeURIComponent(parts[1])} : {})};
  if (parts[0] === 'approvals') return { view: 'approvals' };
  if (parts[0] === 'skills') return { view: 'skills', skills:parseSkillsRoute(parts,qs) };
  if (parts[0] === 'models') return { view: 'models', ...parseModelsRoute(parts) };
  if(parts[0]==='administration'&&parts[1]==='delegation'&&parts[2]==='profiles')return {view:'administration',section:'delegation',...(parts[3]==='new'?{profileCreate:true}:/^[a-z0-9][a-z0-9-]{0,62}$/.test(parts[3]??'')?{profileId:parts[3]}:{})};
  if (parts[0] === 'administration') return { view: 'administration', section: administrationSections.includes(parts[1] as AdministrationSection) ? parts[1] as AdministrationSection : 'health' };
  if (parts[0] === 'connections' && parts[1] === 'c') return { view: 'connections', connectionID: parts[2] ?? '', connectionTab: connectionTabs.includes(parts[3] as ConnectionTab) ? parts[3] as ConnectionTab : 'overview' };
  if (parts[0] === 'connections' && parts[1] === 'source-control' && parts[2] === 'new') return { view: 'connections', sourceControlCreate: true };
  if (parts[0] === 'connections' && parts[1] === 'jira-cloud' && parts[2] === 'new') return {view:'connections', issueEdition:'jira',issueConnectionCreate:true};
  if (parts[0] === 'connections' && parts[1] === 'jira-cloud' && parts[2] === 'c') return {view:'connections',issueEdition:'jira',issueConnectionID:decodeURIComponent(parts[3]??''),...(parts[4]==='transitions'?{issueTransitions:true}:parts[4]==='mappings'?{issueMapping:parts[5]==='new'?{create:true}:parts[5]==='c'&&parts[6]?{name:decodeURIComponent(parts[6])}:{}}:{})};
  if (parts[0] === 'connections' && parts[1] === 'issues' && parts[2] === 'new') return {view: 'connections', issueConnectionCreate: true};
  if (parts[0] === 'connections' && parts[1] === 'issues' && parts[2] === 'c') return {view: 'connections', issueConnectionID: decodeURIComponent(parts[3] ?? ''), ...(parts[4] === 'transitions' ? {issueTransitions: true} : parts[4] === 'mappings' ? {issueMapping: parts[5] === 'new' ? {create:true} : parts[5] === 'c' && parts[6] ? {name: decodeURIComponent(parts[6])} : {}} : {})};
  if (parts[0] === 'connections' && parts[1] === 'new') return { view: 'connections', create: true };
  if (parts[0] === 'connections' && parts[1] === 'history') return { view: 'connections', history: true };
  if (parts[0] === 'integrations' || parts[0] === 'connections') return { view: 'connections', connectQuery: parts[1] === 'connect' ? (hash.includes('?') ? hash.slice(hash.indexOf('?') + 1) : '') : undefined, oauthQuery: parts[1] === 'oauth' ? (qs ?? '') : undefined };
  if (parts[0] === 'g' && parts[1]) {
    const tab = (['canvas', 'agent', 'runs', 'activity', 'files', 'events', 'messages', 'yaml', 'settings'] as GraphTab[]).includes(parts[2] as GraphTab)
      ? (parts[2] as GraphTab)
      : 'canvas';
    const query = new URLSearchParams(qs);
    return {
      view: 'graph',
      id: decodeURIComponent(parts[1]),
      tab,
      correlationId: query.get('c') ?? undefined,
      ...(query.has('connection') ? { connectionOrigin: query.get('connection')! } : {}),
      ...(query.has('sel') ? { selectionKey: query.get('sel')! } : {}),
      ...(tab === 'settings' ? { settingsSection: settingsSection(parts[3]) } : {}),
      ...(tab === 'runs' ? { executionId: query.get('execution') ?? undefined, runSection: runSection(query.get('section')) } : {}),
      ...(tab === 'agent' ? { agentId: query.get('agent') ?? '', agentSection: agentSection(query.get('section')) } : {}),
    };
  }
  return { view: 'graphs' };
}

export function routeHash(r: Route): string {
  if (r.view === 'activity') return activityHash(r.activity);
  if (r.view === 'agents') {
    return r.create ? '#/agents/new' : r.agentId ? `#/agents/${encodeURIComponent(r.agentId)}` : '#/agents';
  }
  if (r.view === 'projects') return r.projectView === 'register' ? '#/projects/new' : r.projectView === 'import' ? '#/projects/import' : '#/projects';
  if (r.view === 'tasks') return r.create ? '#/tasks/new' : r.taskId ? `#/tasks/${encodeURIComponent(r.taskId)}` : '#/tasks';
  if (r.view === 'approvals') return '#/approvals';
  if (r.view === 'skills') return skillsHash(r.skills);
  if (r.view === 'connections') {
    if (r.connectionID !== undefined) return `#/connections/c/${encodeURIComponent(r.connectionID)}/${r.connectionTab ?? 'overview'}`;
    if (r.sourceControlCreate) return scmCreateLink;
    if (r.issueConnectionCreate) return r.issueEdition==='jira'?'#/connections/jira-cloud/new':'#/connections/issues/new';
    if (r.issueConnectionID !== undefined) return `#/connections/${r.issueEdition==='jira'?'jira-cloud':'issues'}/c/${encodeURIComponent(r.issueConnectionID)}${r.issueTransitions ? "/transitions" : r.issueMapping ? `/mappings${r.issueMapping.create ? "/new" : r.issueMapping.name ? `/c/${encodeURIComponent(r.issueMapping.name)}` : ""}` : ""}`;
    if (r.create) return '#/connections/new';
    if (r.history) return '#/connections/history';
    if (r.connectQuery !== undefined) return `#/connections/connect?${r.connectQuery}`;
    if (r.oauthQuery !== undefined) return `#/connections/oauth?${r.oauthQuery}`;
    return '#/connections';
  }
  if (r.view === 'models') return modelsHash(r);
  if(r.view==='administration'&&(r.profileCreate||r.profileId))return r.profileCreate?'#/administration/delegation/profiles/new':`#/administration/delegation/profiles/${encodeURIComponent(r.profileId!)}/edit`;
  if (r.view === 'administration') return `#/administration/${r.section}`;
  if (r.view === 'graph') {
    const path = r.tab === 'settings' ? settingsPath(r.id, r.settingsSection) : `#/g/${encodeURIComponent(r.id)}/${r.tab}${r.tab === 'runs' ? (r.executionId ? `?execution=${encodeURIComponent(r.executionId)}&section=${r.runSection ?? 'result'}` : '') : r.tab === 'agent' ? `?agent=${encodeURIComponent(r.agentId ?? '')}&section=${r.agentSection ?? 'general'}` : r.tab === 'canvas' && r.selectionKey ? `?sel=${encodeURIComponent(r.selectionKey)}` : r.correlationId ? `?c=${encodeURIComponent(r.correlationId)}` : ''}`;
    return r.connectionOrigin ? `${path}${path.includes('?') ? '&' : '?'}connection=${encodeURIComponent(r.connectionOrigin)}` : path;
  }
  return '#/graphs';
}

export function App() {
  const [token, setTok] = useState(getToken());
  const [needLogin, setNeedLogin] = useState(false);
  useEffect(() => onUnauthorized(() => setNeedLogin(true)), []);

  return (
    <EventStreamProvider token={token}>
      <Shell />
      {needLogin && (
        <LoginDialog
          onDone={(t) => {
            setToken(t);
            setTok(t);
            setNeedLogin(false);
            location.reload();
          }}
        />
      )}
    </EventStreamProvider>
  );
}

function Shell() {
  const [route, setRoute] = useState<Route>(() => parseRoute(location.hash));
  const administrationDraft=useRef({dirty:false,busy:false});
  const onAdministrationDraft=useCallback((value:{dirty:boolean;busy:boolean})=>{administrationDraft.current=value;},[]);
  useEffect(() => {
    const f = () => {
      const previous = route;
      const next = parseRoute(location.hash);
      if(previous.view==='administration'&&routeHash(next)!==routeHash(previous)) {
        if(administrationDraft.current.busy||(administrationDraft.current.dirty&&!window.confirm('Discard unsaved administration changes?'))){history.replaceState(null,'',routeHash(previous));return;}
        administrationDraft.current={dirty:false,busy:false};
      }
      if (next.view === 'graph' && previous.view === 'graph' && next.id === previous.id && !next.connectionOrigin && previous.connectionOrigin) {
        next.connectionOrigin = previous.connectionOrigin;
        history.replaceState(null, '', routeHash(next));
      }
      setRoute(next);
    };
    window.addEventListener('hashchange', f);
    return () => window.removeEventListener('hashchange', f);
  }, [route]);
  const go = useCallback((r: Route) => {
    location.hash = routeHash(r);
  }, []);

  const info = useAsync(() => api.info(), []);
  const graphs = useAsync(() => api.listGraphs(), []);
  const approvals = useAsync(() => api.approvals({ status: 'pending' }), []);
  const reloadGraphs = useThrottled(() => void graphs.reload(), 500);
  const reloadApprovals = useThrottled(() => void approvals.reload(), 500);
  const { connected } = useEventStream({ prefixes: ['graph.', 'approval.'] }, (ev) =>
    ev.type.startsWith('graph.') ? reloadGraphs() : reloadApprovals(),
  );

  const list = graphs.data ?? [];
  const ownedTask = route.view === 'graph' ? taskOwnerId(route.id) : undefined;
  const workflows = list.filter(g => !g.graph.metadata.id.startsWith('task-run-') && !g.graph.metadata.labels?.['weft.task']);
  const current = route.view === 'graph' ? workflows.find((g) => g.graph.metadata.id === route.id) : undefined;
  const pending = approvals.data?.length ?? 0;
  type GraphRoute = Extract<Route, {view: 'graph'}>;
  const [sessions, setSessions] = useState<Record<string, {route: GraphRoute; summary: GraphSummary}>>({});
  const [dirty, setDirty] = useState<Record<string, boolean>>({});
  const onDirtyChange = useCallback((id: string, value: boolean) => {
    setDirty(previous => previous[id] === value ? previous : {...previous, [id]: value});
  }, []);
  useEffect(() => {
    if (route.view === 'graph' && current) {
      setSessions(previous => ({...previous, [route.id]: {route, summary: current}}));
    }
  }, [route, current]);
  useEffect(() => {
    if (!Object.values(dirty).some(Boolean)) return;
    const warn = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ''; };
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  }, [dirty]);
  const visibleSessions = {...sessions};
  if (route.view === 'graph' && current) visibleSessions[route.id] = {route, summary: current};


  const onSaved = (s: GraphSummary) => {
    graphs.setData(previous => (previous ?? []).map((g) => (g.graph.metadata.id === s.graph.metadata.id ? s : g)));
  };

  return (
    <div className="app">
      <header className="topbar">
        <button type="button" className="brand" aria-label="Weft" onClick={() => go({ view: 'graphs' })}>
          <BrandMark size={26} /><span className="brand-name" aria-hidden="true">weft</span>
        </button>
        <GraphSelector graphs={workflows} currentId={route.view === 'graph' ? route.id : undefined} dirty={dirty} loading={graphs.loading} error={graphs.error}
          onReload={() => void graphs.reload()}
          onSelect={id => go(sessions[id]?.route ?? {view: 'graph', id, tab: 'canvas'})}/>
        <span className="spacer" />
        {info.data && (
          <span className="infra" title={`instance ${info.data.instance} · version ${info.data.version}`}>
            <span className="infra-meta">{info.data.queue} · {info.data.storage}</span>
            {MOCK && <Badge tone="warn">mock</Badge>}
          </span>
        )}
        <ThemeSwitcher />
        <span className={`conn ${connected ? 'on' : ''}`} title="Event stream">
          {connected ? '● live' : '○ offline'}
        </span>
        {info.data?.grafanaUrl && (
          <a href={info.data.grafanaUrl} target="_blank" rel="noreferrer" className="small">
            Grafana <Icon name="external" size={12} />
          </a>
        )}
      </header>
      <div className="workspace-shell">
        <PrimaryNavigation active={ownedTask ? 'tasks' : route.view === 'graph' ? 'graphs' : route.view} pending={pending}
          onNavigate={view => go(view === 'administration' ? {view, section: 'health'} : view === 'activity' ? {view, activity: parseActivityState('')} : {view})} />
        <main>
          <ErrorBox error={info.error && !graphs.error ? `info: ${info.error}` : undefined} />
          {ownedTask && route.view === 'graph' && <TaskExecutionPage key={route.id} graphId={route.id} tab={route.tab} executionId={route.executionId} section={route.runSection}/>}
          {route.view === 'activity' && <ActivityPage state={route.activity} onChange={activity => go({ view: 'activity', activity })} />}
          {route.view === 'projects' && <ProjectsPage key={route.projectView ?? 'list'} mode={route.projectView} nativeProfile={info.data?.nativeProfile===true} canEdit={info.data?.identity?.role==='admin'}/>}
          {route.view === 'tasks' && <TasksPage key={route.taskId ?? (route.create ? 'new' : 'list')} id={route.taskId} create={route.create} info={info.data} graphs={list}/>}
          {route.view === 'graphs' && (
            <GraphList
              graphs={list}
              canDelete={info.data?.identity?.role === 'admin'}
              canCreate={info.data?.identity?.role === 'admin'}
              canControl={info.data?.identity?.role === 'admin' || info.data?.identity?.role === 'operator'}
              loading={graphs.loading}
              error={graphs.error}
              onOpen={(id) => go({ view: 'graph', id, tab: 'canvas' })}
              onAgent={(id, agentId) => go({ view: 'graph', id, tab: 'agent', agentId, agentSection: 'general' })}
              onChanged={() => void graphs.reload()}
            />
          )}
          {route.view === 'agents' && <AgentsPage key={routeHash(route)} id={route.agentId} create={route.create}
            workflows={workflows} workflowsLoading={graphs.loading} workflowsError={graphs.error} onReloadWorkflows={graphs.reload} canEdit={info.data?.identity?.role==='admin'}/>}
          {route.view === 'skills' && <SkillsView role={info.data?.identity?.role} route={route.skills} onNavigate={skills=>go({view:'skills',skills})} onChanged={() => void graphs.reload()} />}
          {route.view === 'approvals' && <ApprovalsView role={info.data?.identity?.role} onChanged={() => void approvals.reload()} />}
          {route.view === 'connections' && <ConnectionsPage info={info.data} connectionID={route.connectionID} connectionTab={route.connectionTab} create={route.create} sourceControlCreate={route.sourceControlCreate} issueEdition={route.issueEdition} issueConnectionCreate={route.issueConnectionCreate} issueConnectionID={route.issueConnectionID} issueTransitions={route.issueTransitions} issueMapping={route.issueMapping} history={route.history} connectQuery={route.connectQuery} oauthQuery={route.oauthQuery} />}
          {route.view === 'models' && <ModelsPage info={info.data} modelSection={route.modelSection} providerEditor={route.providerEditor} />}
          {route.view === 'administration' && <AdministrationPage info={info.data} infoError={info.error} section={route.section} profileEditor={route.profileCreate?{create:true}:route.profileId?{id:route.profileId}:undefined} onProfileNavigate={editor=>go({view:'administration',section:'delegation',...(editor?.create?{profileCreate:true}:editor?.id?{profileId:editor.id}:{})})} onDraftState={onAdministrationDraft} />}
          {Object.entries(visibleSessions).map(([id, session]) => {
            const active = route.view === 'graph' && route.id === id;
            const workspaceRoute = active ? route : session.route;
            const summary = list.find(g => g.graph.metadata.id === id) ?? session.summary;
            return <Activity key={id} mode={active ? 'visible' : 'hidden'}>
              {active && !current && <ErrorBox error={graphs.error ?? 'Graph unavailable. Your local draft is retained; reload graphs before saving.'}/>}
              {workspaceRoute.connectionOrigin && <aside className="card" aria-label="Connection context"><a href={`#/connections/c/${encodeURIComponent(workspaceRoute.connectionOrigin)}/overview`}>Back to connection {workspaceRoute.connectionOrigin}</a><p>You are viewing agents and runs in graph {summary.graph.metadata.name || id}. Connection setup is separate; opening this graph does not validate or run its agents.</p></aside>}
              <GraphWorkspace summary={summary} info={info.data} tab={workspaceRoute.tab} selectionKey={workspaceRoute.selectionKey}
                executionId={workspaceRoute.executionId} runSection={workspaceRoute.runSection}
                agentId={workspaceRoute.agentId} agentSection={workspaceRoute.agentSection}
                onAgent={(agentId, agentSection) => go({...workspaceRoute, tab: 'agent', agentId, agentSection})}
                settingsSection={workspaceRoute.settingsSection}
                onTab={(tab, settingsSection) => go({...workspaceRoute, tab, settingsSection})} onSaved={onSaved} onDirtyChange={onDirtyChange}
                correlationId={workspaceRoute.correlationId}
                onCorrelation={c => go({...workspaceRoute, tab: 'messages', correlationId: c})}/>
            </Activity>;
          })}
          {route.view === 'graph' && !ownedTask && !visibleSessions[route.id] && (graphs.loading
            ? <p className="muted page">Loading…</p>
            : <ErrorBox error={graphs.error ?? `Graph “${route.id}” not found.`}/>)}

        </main>
      </div>
    </div>
  );
}

function LoginDialog({ onDone }: { onDone: (token: string) => void }) {
  const [t, setT] = useState(getToken());
  const [error, setError] = useState<string>();
  const submit = async () => {
    setToken(t.trim());
    try {
      await api.info();
      onDone(t.trim());
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  return (
    <Modal title="Sign in" onClose={() => undefined}>
      <form
        className="form"
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <p className="muted small">
          The API needs a bearer token configured by the server administrator.
        </p>
        <label>
          API token
          <input type="password" autoFocus value={t} onChange={(e) => setT(e.target.value)} autoComplete="current-password" />
        </label>
        <ErrorBox error={error} />
        <Btn tone="primary" type="submit" disabled={!t.trim()}>
          Continue
        </Btn>
      </form>
    </Modal>
  );
}
