import {ProjectsPage} from './components/ProjectsPage';
import { TasksPage } from './components/TasksPage';
import { PrimaryNavigation } from './components/PrimaryNavigation';
import { runSection, type RunSection } from './components/runPresentation';
import { agentSection, type AgentSection } from './components/agentSettingsModel';
import { Activity, useCallback, useEffect, useState } from 'react';
import { api, errorMessage, getToken, MOCK, onUnauthorized, setToken } from './api/client';
import type { GraphSummary } from './api/types';
import { ApprovalsView } from './components/ApprovalsView';
import { Badge, Btn, ErrorBox, Modal } from './components/common';
import { GraphSelector } from './components/GraphSelector';
import { GraphList } from './components/GraphList';
import { GraphWorkspace, type GraphTab } from './components/GraphWorkspace';
import { AdministrationPage, administrationSections, ConnectionsPage, ModelsPage, type AdministrationSection } from './components/InstallationPages';
import { connectionTabs, type ConnectionTab } from './components/mcpNavigation';
import { SkillsView } from './components/SkillsView';
import { useAsync, useThrottled } from './hooks/useData';
import { EventStreamProvider, useEventStream } from './hooks/useEventStream';

/**
 * Routes live in the hash so a reload (or a shared link) returns to the same
 * place without server-side routing:
 *   #/graphs  #/approvals  #/connections  #/models  #/administration/<section>
 * Legacy #/integrations links remain MCP aliases, including OAuth returns.
 *   #/g/<id>/<tab>[?c=<correlation id>]
 */
type Route =
  | { view: 'graphs' }
  | { view: 'projects' }
  | { view: 'tasks'; taskId?: string; create?: boolean }
  | { view: 'approvals' }
  | { view: 'connections'; connectionID?: string; connectionTab?: ConnectionTab; create?: boolean; history?: boolean; connectQuery?: string; oauthQuery?: string }
  | { view: 'models' }
  | { view: 'administration'; section: AdministrationSection }
  | { view: 'skills' }
  | { view: 'graph'; id: string; tab: GraphTab; selectionKey?: string; agentId?: string; agentSection?: AgentSection; executionId?: string; runSection?: RunSection; correlationId?: string; connectionOrigin?: string };

export function parseRoute(hash: string): Route {
  const [path, qs] = hash.replace(/^#/, '').split('?');
  const parts = path.split('/').filter(Boolean);
  if (parts[0] === 'projects') return {view:'projects'};
  if (parts[0] === 'tasks') return {view:'tasks', ...(parts[1] === 'new' ? {create:true} : parts[1] ? {taskId:decodeURIComponent(parts[1])} : {})};
  if (parts[0] === 'approvals') return { view: 'approvals' };
  if (parts[0] === 'skills') return { view: 'skills' };
  if (parts[0] === 'models') return { view: 'models' };
  if (parts[0] === 'administration') return { view: 'administration', section: administrationSections.includes(parts[1] as AdministrationSection) ? parts[1] as AdministrationSection : 'health' };
  if (parts[0] === 'connections' && parts[1] === 'c') return { view: 'connections', connectionID: parts[2] ?? '', connectionTab: connectionTabs.includes(parts[3] as ConnectionTab) ? parts[3] as ConnectionTab : 'overview' };
  if (parts[0] === 'connections' && parts[1] === 'new') return { view: 'connections', create: true };
  if (parts[0] === 'connections' && parts[1] === 'history') return { view: 'connections', history: true };
  if (parts[0] === 'integrations' || parts[0] === 'connections') return { view: 'connections', connectQuery: parts[1] === 'connect' ? (hash.includes('?') ? hash.slice(hash.indexOf('?') + 1) : '') : undefined, oauthQuery: parts[1] === 'oauth' ? (qs ?? '') : undefined };
  if (parts[0] === 'g' && parts[1]) {
    const tab = (['canvas', 'agent', 'runs', 'activity', 'files', 'events', 'messages', 'yaml'] as GraphTab[]).includes(parts[2] as GraphTab)
      ? (parts[2] as GraphTab)
      : 'canvas';
    const c = new URLSearchParams(qs ?? '').get('c') ?? undefined;
    return { view: 'graph', id: decodeURIComponent(parts[1]), tab, correlationId: c, ...(new URLSearchParams(qs ?? '').has('connection') ? {connectionOrigin:new URLSearchParams(qs ?? '').get('connection')!} : {}), ...(new URLSearchParams(qs ?? '').has('sel') ? {selectionKey:new URLSearchParams(qs ?? '').get('sel')!} : {}), ...(tab === 'runs' ? {executionId:new URLSearchParams(qs ?? '').get('execution') ?? undefined, runSection:runSection(new URLSearchParams(qs ?? '').get('section'))} : {}), ...(tab === 'agent' ? {agentId: new URLSearchParams(qs ?? '').get('agent') ?? '', agentSection: agentSection(new URLSearchParams(qs ?? '').get('section'))} : {}) };
  }
  return { view: 'graphs' };
}

export function routeHash(r: Route): string {
  if (r.view === 'projects') return '#/projects';
  if (r.view === 'tasks') return r.create ? '#/tasks/new' : r.taskId ? `#/tasks/${encodeURIComponent(r.taskId)}` : '#/tasks';
  if (r.view === 'approvals') return '#/approvals';
  if (r.view === 'skills') return '#/skills';
  if (r.view === 'connections') {
    if (r.connectionID !== undefined) return `#/connections/c/${encodeURIComponent(r.connectionID)}/${r.connectionTab ?? 'overview'}`;
    if (r.create) return '#/connections/new';
    if (r.history) return '#/connections/history';
    if (r.connectQuery !== undefined) return `#/connections/connect?${r.connectQuery}`;
    if (r.oauthQuery !== undefined) return `#/connections/oauth?${r.oauthQuery}`;
    return '#/connections';
  }
  if (r.view === 'models') return '#/models';
  if (r.view === 'administration') return `#/administration/${r.section}`;
  if (r.view === 'graph') {
    const path = `#/g/${encodeURIComponent(r.id)}/${r.tab}${r.tab === 'runs' ? (r.executionId ? `?execution=${encodeURIComponent(r.executionId)}&section=${r.runSection ?? 'result'}` : '') : r.tab === 'agent' ? `?agent=${encodeURIComponent(r.agentId ?? '')}&section=${r.agentSection ?? 'general'}` : r.tab === 'canvas' && r.selectionKey ? `?sel=${encodeURIComponent(r.selectionKey)}` : r.correlationId ? `?c=${encodeURIComponent(r.correlationId)}` : ''}`;
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
  useEffect(() => {
    const f = () => {
      const previous = route;
      const next = parseRoute(location.hash);
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
  const current = route.view === 'graph' ? list.find((g) => g.graph.metadata.id === route.id) : undefined;
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
        <button type="button" className="brand" onClick={() => go({ view: 'graphs' })}>
          <img src="/favicon.svg?v=2" alt="" width={22} height={22} /> Weft
        </button>
        <GraphSelector graphs={list} currentId={route.view === 'graph' ? route.id : undefined} dirty={dirty} loading={graphs.loading} error={graphs.error}
          onReload={() => void graphs.reload()}
          onSelect={id => go(sessions[id]?.route ?? {view: 'graph', id, tab: 'canvas'})}/>
        <span className="spacer" />
        {info.data && (
          <span className="infra" title={`instance ${info.data.instance} · version ${info.data.version}`}>
            <span className="infra-meta">{info.data.queue} · {info.data.storage}</span>
            {MOCK && <Badge tone="warn">mock</Badge>}
          </span>
        )}
        <span className={`conn ${connected ? 'on' : ''}`} title="Event stream">
          {connected ? '● live' : '○ offline'}
        </span>
        {info.data?.grafanaUrl && (
          <a href={info.data.grafanaUrl} target="_blank" rel="noreferrer" className="small">
            Grafana ↗
          </a>
        )}
      </header>
      <div className="workspace-shell">
        <PrimaryNavigation active={route.view === 'graph' ? 'graphs' : route.view} pending={pending}
          onNavigate={view => go(view === 'administration' ? {view, section: 'health'} : {view})} />
        <main>
          <ErrorBox error={info.error && !graphs.error ? `info: ${info.error}` : undefined} />
          {route.view === 'projects' && <ProjectsPage nativeProfile={info.data?.nativeProfile===true} canEdit={info.data?.identity?.role==='admin'}/>}
          {route.view === 'tasks' && <TasksPage key={route.taskId ?? (route.create ? 'new' : 'list')} id={route.taskId} create={route.create} info={info.data} graphs={list}/>}
          {route.view === 'graphs' && (
            <GraphList
              graphs={list}
              error={graphs.error}
              onOpen={(id) => go({ view: 'graph', id, tab: 'canvas' })}
              onAgent={(id, agentId) => go({ view: 'graph', id, tab: 'agent', agentId, agentSection: 'general' })}
              onChanged={() => void graphs.reload()}
            />
          )}
          {route.view === 'skills' && <SkillsView role={info.data?.identity?.role} onChanged={() => void graphs.reload()} />}
          {route.view === 'approvals' && <ApprovalsView onChanged={() => void approvals.reload()} />}
          {route.view === 'connections' && <ConnectionsPage info={info.data} connectionID={route.connectionID} connectionTab={route.connectionTab} create={route.create} history={route.history} connectQuery={route.connectQuery} oauthQuery={route.oauthQuery} />}
          {route.view === 'models' && <ModelsPage info={info.data} />}
          {route.view === 'administration' && <AdministrationPage info={info.data} infoError={info.error} section={route.section} />}
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
                onTab={tab => go({...workspaceRoute, tab})} onSaved={onSaved} onDirtyChange={onDirtyChange}
                correlationId={workspaceRoute.correlationId}
                onCorrelation={c => go({...workspaceRoute, tab: 'messages', correlationId: c})}/>
            </Activity>;
          })}
          {route.view === 'graph' && !visibleSessions[route.id] && (graphs.loading
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
