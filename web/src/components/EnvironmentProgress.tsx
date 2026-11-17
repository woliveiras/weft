import type { EnvironmentView } from '../api/types';
import { environmentPhase, reasonLabel } from './environmentPresentation';

export function EnvironmentProgress({ view, showPhase = true }: { view: EnvironmentView; showPhase?: boolean }) {
  return <div aria-label="Environment progress" aria-live="polite">
    {showPhase && <p><strong>{environmentPhase(view)}</strong></p>}
    {view.state === 'building' && view.phase === 'waiting_capacity' && <p>The build is queued for the installation CPU, memory or concurrency budget. It will continue when capacity is available.</p>}
    {view.state === 'building' && view.phase === 'waiting_reuse' && <p>Another build holds the compatible image lease. This build will check for reuse when it finishes.</p>}
    {!!view.changeReasons?.length && <p>Rebuild needed: {view.changeReasons.map(reasonLabel).join('; ')}.</p>}
    {!!view.buildReasons?.length && <p>Last build requested: {view.buildReasons.map(reasonLabel).join('; ')}.</p>}
    {view.reuseDecision === 'no_compatible_generation' && <p>No compatible registered image/cache generation was available.</p>}
    {view.reuseDecision === 'forced' && <p>Shared image reuse was bypassed for this explicit rebuild.</p>}
    {view.reuseDecision === 'private' && <p>This environment is built privately; repository-scoped reuse does not apply.</p>}
    {(view.reuseDecision === 'compatible' || view.reused) && <p>{view.state === 'ready' ? 'Reused compatible prepared image and cache snapshots.' : 'Compatible image selected; validation and private setup must still succeed.'}</p>}
  </div>;
}
