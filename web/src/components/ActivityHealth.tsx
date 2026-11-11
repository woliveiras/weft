import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { api, errorMessage } from '../api/client';
import type { HealthRanking, HealthRow, HealthView } from '../api/activity';
import { useThrottled } from '../hooks/useData';
import { useEventStream } from '../hooks/useEventStream';
import { Btn, ErrorBox, PageHeading, fmtDuration, fmtTime } from './common';
import { money } from './cost';
import { runLink } from './runPresentation';
import {
  healthHistoryLink, healthRowTitle, healthWindow, healthWindows, percent, sectionErrors, tokens,
  type ActivityRouteState,
} from './activityView';

/** Health changes only when executions finish or cost is recorded. */
const LIVE_PREFIXES = ['execution.', 'cost.recorded'];
const REFETCH_MS = 5000;

type Scope = 'agents' | 'workflows';

function useHealth(window: string) {
  const [view, setView] = useState<HealthView>();
  const [error, setError] = useState<string>();
  const [loading, setLoading] = useState(true);
  const generation = useRef(0);
  const load = useCallback(async () => {
    const mine = ++generation.current;
    setLoading(true);
    try {
      const v = await api.activityHealth({ window });
      if (mine !== generation.current) return;
      setView(v);
      setError(undefined);
    } catch (e) {
      if (mine === generation.current) setError(errorMessage(e));
    } finally {
      if (mine === generation.current) setLoading(false);
    }
  }, [window]);
  useEffect(() => {
    void load();
    return () => { generation.current++; };
  }, [load]);
  const refetch = useThrottled(() => void load(), REFETCH_MS);
  useEventStream({ prefixes: LIVE_PREFIXES }, refetch);
  return { view, error, loading, reload: load };
}

function cost(micros: number | null | undefined, currency?: string) {
  return micros === null || micros === undefined ? <span className="muted" title="Cost could not be read">—</span> : money(micros, currency);
}

function duration(ms: number | null | undefined) {
  return ms === null || ms === undefined ? <span className="muted" title="No finished run">—</span> : fmtDuration(ms);
}

interface Board { title: string; label: string; ranking: HealthRanking | null | undefined; metric: (r: HealthRow) => ReactNode; empty: string }

export function ActivityHealth({ state, onChange, tabs }: { state: ActivityRouteState; onChange: (next: ActivityRouteState) => void; tabs: ReactNode }) {
  const window = healthWindow(state.window);
  const { view, error, loading, reload } = useHealth(window);
  const [scope, setScope] = useState<Scope>('agents');
  const rows = view?.[scope] ?? null;
  const byKey = new Map((rows ?? []).map(r => [r.key, r]));
  const r = view?.rankings;
  const boards: Board[] = [
    { title: 'Fails most', label: 'failure rate', ranking: r?.failsMost, metric: row => `${percent(row.failureRate)} · ${row.failed} of ${row.finished} failed`, empty: 'Nothing failed in this window.' },
    { title: 'Slowest', label: 'p90 duration', ranking: r?.slowest, metric: row => <>p90 {duration(row.p90Ms)} · median {duration(row.p50Ms)}</>, empty: 'No run finished in this window.' },
    { title: 'Most expensive', label: 'cost', ranking: r?.mostExpensive, metric: row => cost(row.costMicros, view?.currency), empty: r && !r.mostExpensive ? 'Cost could not be read.' : 'No cost was recorded in this window.' },
  ];
  const unit = scope === 'agents' ? 'agent' : 'workflow';

  return <div className="page activity-page" aria-label="Activity">
    <PageHeading title="Activity" description="Which agents and workflows fail, slow down or cost the most.">
      <div className="btn-row">
        <label>Window <select value={window} onChange={e => onChange({ ...state, window: e.target.value })}>
          {healthWindows.map(w => <option key={w} value={w}>last {w}</option>)}
        </select></label>
        <Btn onClick={() => void reload()} disabled={loading}>Refresh</Btn>
      </div>
    </PageHeading>
    {tabs}
    <p role="status" className="muted small">
      {loading && !view ? 'Loading health…' : view ? `Updated ${fmtTime(view.generatedAt)} · executions started in the last ${view.window}.` : ''}
    </p>
    {error && <ErrorBox error={view ? `Could not refresh health: ${error}. Showing data from ${fmtTime(view.generatedAt)}.` : error} />}

    {view && <>
      <div className="seg" role="radiogroup" aria-label="Group by">
        {(['agents', 'workflows'] as Scope[]).map(s => <button key={s} type="button" role="radio" aria-checked={scope === s} className={scope === s ? 'on' : ''}
          onClick={() => setScope(s)}>{s === 'agents' ? 'Agents' : 'Workflows'}</button>)}
      </div>
      <SectionNotes view={view} />
      {view.truncated.executions && <p className="muted small">More than {view.limits.executions} executions started in this window; health describes the newest {view.limits.executions}.</p>}
      {view.truncated.cost && <p className="muted small">Cost reads the newest {view.limits.costEvents} cost records; older costs in this window are not included.</p>}

      {rows && <section className="activity-section" aria-label="Rankings">
        <h2>Rankings</h2>
        <div className="activity-rankings">
          {boards.map(b => {
            const keys = (scope === 'agents' ? b.ranking?.agents : b.ranking?.workflows) ?? [];
            return <article key={b.title} className="card" aria-label={b.title}>
              <header><strong>{b.title}</strong> <span className="muted small">by {b.label}</span></header>
              {keys.length === 0 ? <p className="muted small">{b.empty}</p> : <ol className="activity-ranking">
                {keys.map(k => byKey.get(k)).filter((row): row is HealthRow => !!row).map(row => <li key={row.key}>
                  <a href={healthHistoryLink(window, row, b.title === 'Fails most')}>{healthRowTitle(row)}</a>
                  <span className="muted small">{b.metric(row)}</span>
                </li>)}
              </ol>}
            </article>;
          })}
        </div>
      </section>}

      {rows && <section className="activity-section" aria-label={scope === 'agents' ? 'Agent health' : 'Workflow health'}>
        <h2>{scope === 'agents' ? 'Agents' : 'Workflows'}</h2>
        <p className="muted small">Busiest first. Failure rate counts failed or interrupted runs among finished ones; durations are of finished runs.</p>
        {rows.length === 0 ? <p>No executions started in this window.</p> : <div className="connection-table"><table>
          <caption className="sr-only">Health per {unit}</caption>
          <thead><tr>
            {scope === 'agents' && <th>Agent</th>}<th>Workflow</th><th>Runs</th><th>Failure rate</th><th>Median</th><th>p90</th><th>Tokens</th><th>Cost</th><th>Most common errors</th><th>Details</th>
          </tr></thead>
          <tbody>{rows.map(row => <tr key={row.key}>
            {scope === 'agents' && <td>{row.nodeId}</td>}
            <td>{row.workflowName || row.workflow}</td>
            <td>{row.runs}</td>
            <td>{percent(row.failureRate)}{row.failed > 0 && <div className="muted small">{row.failed} of {row.finished}</div>}</td>
            <td>{duration(row.p50Ms)}</td>
            <td>{duration(row.p90Ms)}</td>
            <td>{tokens(row.usage)}</td>
            <td>{cost(row.costMicros, view.currency)}</td>
            <td>{row.topErrors.length === 0 ? <span className="muted">—</span> : <ul className="activity-errors">
              {row.topErrors.map(e => <li key={e.message}>
                <span className="activity-error">{e.message}</span> <span className="muted small">×{e.count}</span>{' '}
                <a href={runLink(e.lastRun.graphId, e.lastRun.id)} aria-label={`Latest run with: ${e.message}`}>latest</a>
              </li>)}
              {row.otherErrors > 0 && <li className="muted small">+{row.otherErrors} other</li>}
            </ul>}</td>
            <td><div className="activity-links">
              <a href={healthHistoryLink(window, row)}>History</a>
              {row.lastRun && <a href={runLink(row.lastRun.graphId, row.lastRun.id)}>Last run</a>}
              {row.lastFailure && <a href={runLink(row.lastFailure.graphId, row.lastFailure.id)}>Last failure</a>}
            </div></td>
          </tr>)}</tbody>
        </table></div>}
        {view.truncated[scope] && <p className="muted small">Only the {view.limits.rows} busiest {unit}s and every ranked one are listed.</p>}
      </section>}
    </>}
  </div>;
}

function SectionNotes({ view }: { view: HealthView }) {
  return <>{sectionErrors(view, 'executions', 'cost').map(e => e.code === 'failed'
    ? <ErrorBox key={e.section} error={e.message} />
    : <p key={e.section} className="muted small" role="note">{e.message}</p>)}</>;
}
