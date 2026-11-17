import { describe, expect, it } from 'vitest';
import { budgetLabel, fromMicros, money, toMicros, usedPercent } from './cost';

describe('cost helpers', () => {
  it('converts between currency units and micro-units without drift', () => {
    expect(toMicros('2.5')).toBe(2_500_000);
    expect(toMicros('0.000001')).toBe(1);
    expect(toMicros(' 10 ')).toBe(10_000_000);
    for (const bad of ['', '-1', '1,5', 'abc', '1.0000001']) expect(toMicros(bad)).toBeUndefined();
    expect(fromMicros(2_500_000)).toBe('2.5');
    expect(fromMicros(undefined)).toBe('');
  });
  it('formats money, keeping tiny amounts visible', () => {
    expect(money(2_500_000, 'USD')).toBe('$2.50');
    expect(money(15, 'USD')).toBe('$0.000015');
    expect(money(0, 'EUR')).toContain('0.00');
    expect(money(1_500_000, '')).toBe('$1.50');
    expect(money(1_000_000, 'XXX1')).toBe('1.00 XXX1');
  });
  it('labels budgets and computes usage', () => {
    expect(budgetLabel({ scope: 'user', id: 'alice@2026-11' })).toBe('User alice · 2026-11');
    expect(budgetLabel({ scope: 'installation', id: '*@2026-11' })).toBe('Installation · 2026-11');
    expect(budgetLabel({ scope: 'run', id: 'task-run-1' })).toBe('Task run task-run-1');
    expect(usedPercent({ limitMicros: 200, chargedMicros: 100, reservedMicros: 60 })).toBe(80);
    expect(usedPercent({ limitMicros: 0, chargedMicros: 5, reservedMicros: 0 })).toBe(0);
  });
});
