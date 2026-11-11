import { useEffect, useRef, useState } from 'react';
import { api, errorMessage } from '../api/client';
import { useAsync } from '../hooks/useData';
import { Btn, ErrorBox, JsonView } from './common';

export function EnvironmentProposal({ graphId, disabled }: { graphId: string; disabled: boolean }) {
  const proposal = useAsync(() => api.environmentProposal(graphId), [graphId]);
  const [resume, setResume] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const generation = useRef(0);
  const state = proposal.data?.state;
  useEffect(() => {
    if (state !== 'analyzing' && state !== 'awaiting_approval' && state !== 'building') return;
    const timer = setInterval(() => void proposal.reload(), 2000);
    return () => clearInterval(timer);
  }, [state, proposal.reload]);
  const act = async (action: 'propose' | 'cancel' | 'retry') => {
    const current = ++generation.current;
    setBusy(true); setError(undefined);
    if (action === 'propose') proposal.setData({ id: '', graphId, graphVersion: 0, state: 'analyzing', resume });
    try {
      const result = action === 'propose' ? await api.proposeEnvironment(graphId, resume)
        : action === 'cancel' ? await api.cancelEnvironmentProposal(graphId) : await api.retryEnvironmentProposal(graphId);
      if (current === generation.current) proposal.setData(result);
    } catch (cause) { if (current === generation.current) { setError(errorMessage(cause)); await proposal.reload(); } }
    finally { if (current === generation.current) setBusy(false); }
  };
  const canPropose = !state || ['failed', 'rejected', 'expired', 'cancelled'].includes(state);
  return <section aria-label="Environment analysis">
    <p>No environment definition found. Ask the analysis agent to propose one from manifests and lockfiles. It cannot run repository commands. Approval is required before any build.</p>
    <ErrorBox error={error ?? proposal.error ?? proposal.data?.error} />
    {state && <p role="status">Environment proposal: {state}</p>}
    {proposal.data?.analysis && <>
      <p>{proposal.data.analysis.rationale}</p>
      <JsonView value={proposal.data.analysis} label="Proposed environment" open />
    </>}
    {canPropose && <>
      <label><input type="checkbox" checked={resume} disabled={disabled || busy} onChange={e => setResume(e.target.checked)} /> Start graph when the approved environment is ready</label>
      <Btn disabled={disabled || busy || proposal.loading} onClick={() => void act('propose')}>{busy ? 'Analyzing…' : 'Analyze repository'}</Btn>
    </>}
    {state === 'awaiting_approval' && <p>Open Approvals to accept or reject the full proposal. Admin approval is required.</p>}
    {(state === 'analyzing' || state === 'awaiting_approval') && !!proposal.data?.id && <Btn disabled={disabled || (busy && state !== 'analyzing')} onClick={() => void act('cancel')}>Cancel proposal</Btn>}
    {state === 'failed' && proposal.data?.analysis && <Btn disabled={disabled || busy} onClick={() => void act('retry')}>Retry approved build</Btn>}
    {state === 'building' && <p>Approval saved the environment. Reload the graph to see the saved definition and build logs.</p>}
    {state === 'ready' && <p>The approved environment is ready. Reload the graph to see its saved definition.</p>}
  </section>;
}
