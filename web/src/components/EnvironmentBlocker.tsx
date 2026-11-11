import { useEffect, useRef, useState } from 'react';
import { ApiError, api, errorMessage } from '../api/client';
import type { GraphStatus } from '../api/types';
import { Btn } from './common';

/** The spec.environment issue of a refused start (422), if that is why it was refused. */
export function environmentIssue(e: unknown): string | undefined {
  if (!(e instanceof ApiError) || e.status !== 422) return undefined;
  const details = e.details as { errors?: { path: string; message: string }[] } | undefined;
  return details?.errors?.find((issue) => issue.path === 'spec.environment')?.message;
}

const POLL_MS = 3000;

/**
 * Shown when a start was refused because the graph environment is not ready:
 * build it, or build it and start the graph as soon as it is ready.
 */
export function EnvironmentBlocker({
  graphId,
  message,
  canBuild,
  onStarted,
  onDismiss,
}: {
  graphId: string;
  message: string;
  canBuild: boolean;
  onStarted: (status: GraphStatus) => void;
  onDismiss: () => void;
}) {
  const [phase, setPhase] = useState<'idle' | 'building' | 'starting' | 'built'>('idle');
  const [error, setError] = useState<string>();
  const startAfter = useRef(false);

  useEffect(() => {
    if (phase !== 'building') return;
    let cancelled = false;
    const poll = async () => {
      try {
        const env = await api.environment(graphId);
        if (cancelled) return;
        if (env.state === 'failed') {
          setError(`The environment build failed: ${env.error ?? 'see the build log in Graph settings → Environment'}`);
          setPhase('idle');
          return;
        }
        if (env.state === 'ready' && env.upToDate) {
          if (!startAfter.current) {
            setPhase('built');
            return;
          }
          setPhase('starting');
          onStarted(await api.graphAction(graphId, 'start'));
          onDismiss();
          return;
        }
        timer = setTimeout(() => void poll(), POLL_MS);
      } catch (cause) {
        if (!cancelled) {
          setError(errorMessage(cause));
          setPhase('idle');
        }
      }
    };
    let timer = setTimeout(() => void poll(), POLL_MS);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [phase, graphId]); // eslint-disable-line react-hooks/exhaustive-deps

  const build = async (thenStart: boolean) => {
    startAfter.current = thenStart;
    setError(undefined);
    try {
      const env = await api.buildEnvironment(graphId);
      // Already up to date (someone built it meanwhile): start right away.
      if (env.state === 'ready' && env.upToDate) {
        if (thenStart) {
          setPhase('starting');
          onStarted(await api.graphAction(graphId, 'start'));
          onDismiss();
        } else {
          setPhase('built');
        }
        return;
      }
      setPhase('building');
    } catch (cause) {
      setError(errorMessage(cause));
      setPhase('idle');
    }
  };

  return (
    <div className="environment-blocker" role="alert">
      <p>
        <strong>The graph cannot start yet.</strong> {message}
      </p>
      {error && <p className="field-error">{error}</p>}
      {phase === 'building' && (
        <p className="muted small" role="status">
          Building the environment…{startAfter.current ? ' The graph starts as soon as it is ready.' : ''} Follow the log in
          Graph settings → Environment.
        </p>
      )}
      {phase === 'starting' && <p className="muted small">Environment ready. Starting…</p>}
      {phase === 'built' && <p className="muted small">Environment ready. You can start the graph.</p>}
      <div className="btn-row">
        {canBuild && phase === 'idle' && (
          <>
            <Btn small tone="primary" onClick={() => void build(true)}>
              Build and start when ready
            </Btn>
            <Btn small onClick={() => void build(false)}>
              Build environment
            </Btn>
          </>
        )}
        {!canBuild && <span className="muted small">Ask an operator to build the environment.</span>}
        <Btn small tone="ghost" onClick={onDismiss}>
          {phase === 'building' ? 'Stop waiting' : 'Dismiss'}
        </Btn>
      </div>
    </div>
  );
}
