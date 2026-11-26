import type { ChatSkillReview } from '../api/chat';
import { Btn } from './common';
import { skillsHash } from './skillsNavigation';

export function ChatSkillCard({review, now, canConfirm, onDecide, onView}: {
  review: ChatSkillReview; now: number; canConfirm: boolean;
  onDecide: (decision: 'confirm'|'decline') => void; onView: (anchor:string) => void;
}) {
  const {skill} = review;
  const anchor = 'chat-skill-' + review.id;
  return <section className="chat-agent-review" aria-label="Managed skill revision">
    <h3>{skill.delete ? 'Delete' : skill.before ? 'Revise' : 'Create'} managed skill</h3>
    <p>{skill.after.id} · {skill.after.enabled ? 'Enabled' : 'Disabled'} · {skill.after.global ? 'Installation defaults' : 'Selected workflows'}</p>
    {skill.before && <><h4>Current revision {skill.revision}</h4><p>{skill.before.title}</p><p>{skill.before.body}</p></>}
    <h4>Proposed revision {skill.revision + 1}</h4><p>{skill.after.title}</p><p>{skill.after.body}</p>
    {skill.before && skill.before.enabled !== skill.after.enabled && <p>Activation: {skill.before.enabled ? 'Enabled → Disabled' : 'Disabled → Enabled'}</p>}
    {skill.before && skill.before.global !== skill.after.global && <p>Audience: {skill.before.global ? 'Installation defaults → Selected workflows' : 'Selected workflows → Installation defaults'}</p>}
    {skill.delete && <p>The skill will be removed from the active catalog. Prior revisions remain available.</p>}
    {skill.after.enabled && <p>Eligible future turns can load this revision.</p>}
    {review.state === 'awaiting_confirmation' && (now < Date.parse(review.expiresAt)
      ? <><Btn disabled={!canConfirm} tone="primary" onClick={() => onDecide('confirm')}>{skill.delete ? 'Delete skill' : 'Save skill revision'}</Btn>{' '}
        <Btn disabled={!canConfirm} onClick={() => onDecide('decline')}>Decline</Btn></>
      : <p role="status">Confirmation expired. Request a fresh review.</p>)}
    {review.state === 'applied' && <p role="status">{skill.delete ? `Skill deleted at revision ${review.resultVersion}.` : `Skill revision ${review.resultVersion} saved.`}</p>}
    {review.state === 'applying' && <p role="status">Saving skill revision…</p>}
    {review.state === 'conflict' && <p role="status">The saved skill changed. Request a fresh review.</p>}
    {review.state === 'declined' && <p role="status">Proposal declined.</p>}
    {review.state === 'unknown' && <p role="status">The save outcome is unknown. Inspect the saved skill before retrying.</p>}
    {(skill.before || review.state === 'applied') &&
      <a id={anchor} href={skillsHash({skillId:skill.after.id,tab:'content'})} onClick={() => onView(anchor)}>View skill</a>}
  </section>;
}
