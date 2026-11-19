import { expect, it } from 'vitest';
import type { Approval, HumanReview } from '../api/types';
import { decisionEntries, matchingDecisions, defaultApprovalFilters } from './approvalPresentation';
const approval = (id: string, createdAt: string, extra = {}): Approval => ({ id, createdAt, graphId: 'flow', nodeId: 'worker', replica: 0, kind: 'tool', status: 'pending', title: 'Read repository', ...extra });
const review = { id: 'review', graphId: 'other', createdAt: '2026-11-19T10:01:00Z', state: 'approved', request: { title: 'Review candidate' }, decidedBy: 'Maintainer' } as HumanReview;
it('merges sources newest first without changing source arrays and resolves tied or missing dates deterministically', () => {
  const approvals = [approval('a', '2026-11-19T10:00:00Z'), approval('b', '2026-11-19T10:00:00Z'), approval('legacy', 'invalid')];
  const entries = decisionEntries(approvals, [review]);
  expect(entries.map(entry => entry.value.id)).toEqual(['review', 'b', 'a', 'legacy']);
  expect(approvals.map(entry => entry.id)).toEqual(['a', 'b', 'legacy']);
  expect(decisionEntries([], [{ ...review, createdAt: undefined }])[0].createdAt).toBeUndefined();
});
it('combines exact filters and case-insensitive search across factual identifiers', () => {
  const entries = decisionEntries([approval('a', '2026-11-19T10:00:00Z')], [review]);
  expect(matchingDecisions(entries, defaultApprovalFilters).map(entry => entry.value.id)).toEqual(['a']);
  const all = { search: '', status: '', kind: '', workflow: '' };
  for (const query of [' REPOSITORY ', 'WORKER', 'flow', 'a']) expect(matchingDecisions(entries, { ...all, search: query }).some(entry => entry.value.id === 'a')).toBe(true);
  expect(matchingDecisions(entries, { ...all, search: 'maintainer', status: 'approved', kind: 'review', workflow: 'other' })).toHaveLength(1);
  expect(matchingDecisions(entries, { ...all, status: 'pending', kind: 'review' })).toHaveLength(0);
  expect(matchingDecisions(entries, { ...all, search: 'unknown' })).toHaveLength(0);
  expect(matchingDecisions([], all)).toEqual([]);
});
