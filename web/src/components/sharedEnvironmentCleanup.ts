import type { EnvironmentCleanupItem } from '../api/types';

export const CLEANUP_LIMIT = 100;

export function canClean(item: EnvironmentCleanupItem): boolean {
  return item.eligible && !item.error && !item.missing && !item.removed && !(item.usedBy?.length);
}

export function cleanupOutcome(item: EnvironmentCleanupItem): string {
  if (item.error) return `Failed: ${item.error}`;
  if (item.missing) return 'Already absent';
  if (item.removed) return 'Removed';
  if (item.usedBy?.length) return `Retained — ${item.usedBy.join(', ')}`;
  return item.eligible ? 'Unused' : 'Retained';
}
