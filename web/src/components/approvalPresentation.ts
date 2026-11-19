import type { Approval, HumanReview } from '../api/types';

export const approvalKinds: Record<string, string> = {
  tool: 'Tool',
  turn: 'Turn',
  message: 'Message',
  question: 'Question',
  environment: 'Environment',
  task_environment: 'Task environment',
  delegation: 'Delegation',
  review: 'Application review',
};

export const approvalStatuses: Record<string, string> = {
  pending: 'Pending',
  approved: 'Approved',
  rejected: 'Rejected',
  expired: 'Expired',
  changes_requested: 'Changes requested',
  cancelled: 'Cancelled',
  superseded: 'Superseded',
};

export type DecisionEntry =
  | { key: string; source: 'approval'; title: string; createdAt?: string; kind: string; status: string; graphId: string; nodeId?: string; value: Approval }
  | { key: string; source: 'review'; title: string; createdAt?: string; kind: string; status: string; graphId: string; nodeId?: string; value: HumanReview };

export type ApprovalFilters = { search: string; status: string; kind: string; workflow: string };
export const defaultApprovalFilters: ApprovalFilters = { search: '', status: 'pending', kind: '', workflow: '' };

export function decisionEntries(approvals: Approval[], reviews: HumanReview[]): DecisionEntry[] {
  const entries: DecisionEntry[] = [
    ...approvals.map(value => ({
      key: `approval:${value.id}`, source: 'approval' as const, title: value.title, createdAt: value.createdAt,
      kind: value.kind, status: value.status, graphId: value.graphId, nodeId: value.nodeId, value,
    })),
    ...reviews.map(value => ({
      key: `review:${value.id}`, source: 'review' as const, title: value.request.title, createdAt: value.createdAt,
      kind: 'review', status: value.state, graphId: value.graphId, nodeId: value.nodeId, value,
    })),
  ];
  return entries.sort((a, b) => {
    const aTime = Date.parse(a.createdAt ?? '') || 0;
    const bTime = Date.parse(b.createdAt ?? '') || 0;
    return bTime - aTime || b.key.localeCompare(a.key);
  });
}

export function matchingDecisions(entries: DecisionEntry[], filters: ApprovalFilters) {
  const query = filters.search.trim().toLowerCase();
  return entries.filter(entry =>
    (!filters.status || entry.status === filters.status) &&
    (!filters.kind || entry.kind === filters.kind) &&
    (!filters.workflow || entry.graphId === filters.workflow) &&
    [entry.title, entry.value.id, entry.graphId, entry.nodeId ?? '', entry.value.decidedBy ?? '']
      .some(value => value.toLowerCase().includes(query)),
  );
}
