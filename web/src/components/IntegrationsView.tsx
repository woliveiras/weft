import { api } from '../api/client';
import type { Info } from '../api/types';
import { useAsync } from '../hooks/useData';
import { Badge, Btn } from './common';

type ProbeState = 'checking' | 'ok' | 'warning' | 'error';

function Status({
  state,
  idleLabel = 'Not tested',
  successLabel = 'Connected',
}: {
  state?: ProbeState;
  idleLabel?: string;
  successLabel?: string;
}) {
  if (!state) return <Badge>{idleLabel}</Badge>;
  if (state === 'checking') return <Badge tone="info">Testing…</Badge>;
  if (state === 'ok') return <Badge tone="ok">{successLabel}</Badge>;
  if (state === 'warning') return <Badge tone="warn">Check needed</Badge>;
  return <Badge tone="bad">Unavailable</Badge>;
}

export function IntegrationsView({ info, infoError }: { info?: Info; infoError?: string }) {
  const health = useAsync(api.health, []);
  const readiness = useAsync(api.ready, []);
  const checks = readiness.error ? undefined : readiness.data;
  const dependenciesOk = checks && checks.storage === 'ok' && checks.queue === 'ok';

  return (
    <div className="page integrations-page">
      <div className="page-head integrations-head">
        <div>
          <h2>System health</h2>
          <p className="muted">
            Check the services this installation needs to run. These checks do not invoke model providers.
          </p>
        </div>
      </div>

      <section className="integration-section">
        <div className="section-head">
          <div>
            <h2>Weft services</h2>
            <p className="muted small">
              Basic API availability, the storage and queue readiness checks, and the sandbox runtime used by docker-isolated agents.
            </p>
          </div>
          <Btn small onClick={() => void Promise.all([health.reload(), readiness.reload()])} disabled={readiness.loading || health.loading}>
            {readiness.loading ? 'Checking…' : 'Check services'}
          </Btn>
        </div>
        <div className="integration-grid">
          <article className="integration-card">
            <div className="integration-card-head">
              <h3>Control API</h3>
              <Status
                state={health.data?.status === 'ok' ? 'ok' : health.error ? 'error' : undefined}
                idleLabel={health.loading ? 'Checking' : 'Not checked'}
              />
            </div>
            <p className="muted small">
              {health.error ? `Health check failed: ${health.error}` : health.data?.status === 'ok'
                ? `Responding${info ? ` · ${info.instance} · version ${info.version}` : ''}`
                : infoError ?? 'Loading server health…'}
            </p>
          </article>
          <article className="integration-card">
            <div className="integration-card-head">
              <h3>Storage</h3>
              <Status
                state={checks ? (checks.storage === 'ok' ? 'ok' : 'error') : undefined}
                idleLabel={readiness.error ? 'Could not check' : readiness.loading ? 'Checking' : 'Not checked'}
              />
            </div>
            <p className="muted small">{checks?.storage ?? readiness.error ?? info?.storage ?? 'Waiting for check'}</p>
          </article>
          <article className="integration-card">
            <div className="integration-card-head">
              <h3>Queue</h3>
              <Status
                state={checks ? (checks.queue === 'ok' ? 'ok' : 'error') : undefined}
                idleLabel={readiness.error ? 'Could not check' : readiness.loading ? 'Checking' : 'Not checked'}
              />
            </div>
            <p className="muted small">{checks?.queue ?? readiness.error ?? info?.queue ?? 'Waiting for check'}</p>
          </article>
          <article className="integration-card" aria-label="Sandbox runtime">
            <div className="integration-card-head">
              <h3>Sandbox runtime</h3>
              <Status
                state={
                  !checks?.sandbox
                    ? undefined
                    : checks.sandbox === 'ok'
                      ? 'ok'
                      : checks.sandbox === 'not configured'
                        ? 'warning'
                        : 'error'
                }
                idleLabel={readiness.error ? 'Could not check' : readiness.loading ? 'Checking' : 'Not checked'}
              />
            </div>
            <p className="muted small">
              {checks?.sandbox === 'ok'
                ? 'Docker daemon dedicated to Weft is reachable. Agents with docker isolation run their commands there, never on the host.'
                : checks?.sandbox === 'not configured'
                  ? 'No sandbox daemon is configured. Agents with docker isolation cannot start; process isolation runs commands inside the Weft server.'
                  : (checks?.sandbox ?? readiness.error ?? 'Waiting for check')}
            </p>
          </article>
        </div>
        {checks && !dependenciesOk && (
          <p className="integration-note tone-note">One or more dependencies did not pass the readiness check.</p>
        )}
        {readiness.error && (
          <p className="integration-note tone-note">
            Readiness endpoint failed: {readiness.error}. The configured storage/queue names above are not a connection test.
          </p>
        )}
      </section>

    </div>
  );
}
