import type { BudgetView } from '../api/types';

/** Amounts travel as integer micro-units (1 currency unit = 1 000 000). */
export const MICROS = 1_000_000;

/** Formats micro-units as money in the catalogue currency. */
export function money(micros: number, currency?: string): string {
  currency ||= 'USD';
  const value = micros / MICROS;
  try {
    return new Intl.NumberFormat('en', { style: 'currency', currency, maximumFractionDigits: value !== 0 && Math.abs(value) < 0.01 ? 6 : 2 }).format(value);
  } catch {
    return `${value.toFixed(2)} ${currency}`;
  }
}

/** Parses an amount typed in currency units ("2.5") into micro-units; undefined when invalid. */
export function toMicros(text: string): number | undefined {
  const t = text.trim();
  if (!/^\d+(\.\d{1,6})?$/.test(t)) return undefined;
  return Math.round(Number(t) * MICROS);
}

/** Shows micro-units in currency units for an input field. */
export function fromMicros(micros?: number): string {
  if (micros === undefined) return '';
  return String(micros / MICROS);
}

/** Share of a budget used (charged plus reserved), 0-100+. */
export function usedPercent(v: Pick<BudgetView, 'limitMicros' | 'chargedMicros' | 'reservedMicros'>): number {
  if (v.limitMicros <= 0) return 0;
  return Math.round(((v.chargedMicros + v.reservedMicros) * 100) / v.limitMicros);
}

const scopeLabels: Record<string, string> = { run: 'Task run', installation: 'Installation', user: 'User', project: 'Project', workflow: 'Workflow' };

/** Human label for a budget ("User alice", "Installation"). Monthly ids carry "@YYYY-MM". */
export function budgetLabel(v: Pick<BudgetView, 'scope' | 'id'>): string {
  const [id, period] = v.id.split('@');
  const scope = scopeLabels[v.scope] ?? v.scope;
  const who = v.scope === 'installation' ? '' : ` ${id}`;
  return `${scope}${who}${period ? ` · ${period}` : ''}`;
}
