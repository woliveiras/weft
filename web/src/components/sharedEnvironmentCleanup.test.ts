import { describe, expect, it } from 'vitest';
import type { EnvironmentCleanupItem } from '../api/types';
import { canClean, cleanupOutcome } from './sharedEnvironmentCleanup';

const unused: EnvironmentCleanupItem = { id: 'a', image: 'image', eligible: true, removed: false };
describe('shared cleanup policy', () => {
  it('only selects unused, present resources with no inspection failure', () => {
    expect(canClean(unused)).toBe(true);
    for (const change of [{ eligible: false }, { error: 'daemon unavailable' }, { missing: true }, { removed: true }, { usedBy: ['graph:other'] }]) {
      expect(canClean({ ...unused, ...change })).toBe(false);
    }
  });
  it('distinguishes partial failure, missing, removed and retained results', () => {
    expect(cleanupOutcome({ ...unused, error: 'volume busy' })).toBe('Failed: volume busy');
    expect(cleanupOutcome({ ...unused, missing: true })).toBe('Already absent');
    expect(cleanupOutcome({ ...unused, removed: true })).toBe('Removed');
    expect(cleanupOutcome({ ...unused, usedBy: ['container:stopped'] })).toContain('container:stopped');
    expect(cleanupOutcome({ ...unused, eligible: false })).toBe('Retained');
    expect(cleanupOutcome(unused)).toBe('Unused');
  });
});
