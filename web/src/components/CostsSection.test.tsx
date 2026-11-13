import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { BudgetView, SpendReport } from '../api/types';
import { apiError, fakeApi } from '../test/fakeApi';
import { CostsSection } from './CostsSection';

const line = (key: string, cost: number, model?: string) => ({ key, model, calls: 2, inputTokens: 100, outputTokens: 20, costMicros: cost, estimatedMicros: 0, unpricedCalls: 0 });
const spend: SpendReport = {
  period: '2026-11', currency: 'USD',
  total: { ...line('total', 3_500_000), estimatedMicros: 500_000, unpricedCalls: 1 },
  byProvider: [line('team', 3_000_000, 'gpt-x')], byTask: [line('task-run-1', 3_000_000)], byUser: [line('alice', 3_000_000)], byProject: [], byWorkflow: [line('factory', 3_000_000)],
};
const reached: BudgetView = { scope: 'user', id: 'alice@2026-11', currency: 'USD', limitMicros: 5_000_000, chargedMicros: 5_000_000, estimatedMicros: 0, reservedMicros: 0, remainingMicros: 0, reached: true, paused: true };
const healthy: BudgetView = { ...reached, scope: 'installation', id: '*@2026-11', limitMicros: 100_000_000, chargedMicros: 10_000_000, remainingMicros: 90_000_000, reached: false, paused: false };

function routes(over: Record<string, Parameters<typeof fakeApi>[0][string]> = {}) {
  return fakeApi({
    'GET /api/v1/cost/spend': () => spend,
    'GET /api/v1/cost/ledgers': () => [reached, healthy],
    'GET /api/v1/cost/policy': () => ({ taskRunBudgetMicros: 2_000_000, warningPercent: 80 }),
    'GET /api/v1/cost/budgets': () => ({ budgets: [{ scope: 'user', id: 'alice', limitMicros: 5_000_000 }], period: '2026-11' }),
    'GET /api/v1/cost/prices': () => ({ version: 2, currency: 'USD', unpriced: 'allow', prices: [{ provider: 'team', model: '*', inputMicrosPerMTok: 2_500_000, outputMicrosPerMTok: 10_000_000 }], updatedBy: 'admin' }),
    ...over,
  });
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('CostsSection', () => {
  it('shows monthly spend and budget levels read-only to viewers', async () => {
    routes();
    render(<CostsSection canEdit={false} />);
    const group = await screen.findByRole('group', { name: 'Spend in 2026-11' });
    expect(group.textContent).toContain('$3.50');
    expect(group.textContent).toContain('$0.50 estimated');
    expect(group.textContent).toContain('1 unpriced calls');
    expect(within(screen.getByRole('table', { name: 'By provider and model' })).getByText('gpt-x')).toBeTruthy();
    const alice = screen.getByRole('listitem', { name: 'Budget User alice · 2026-11' });
    expect(within(alice).getByText('Reached')).toBeTruthy();
    expect(within(alice).getByText('Runs paused')).toBeTruthy();
    expect(within(alice).queryByRole('button', { name: 'Raise budget' })).toBeNull();
    expect(within(screen.getByRole('listitem', { name: 'Budget Installation · 2026-11' })).getByText('Within budget')).toBeTruthy();
    // Settings are visible but disabled.
    await waitFor(() => expect((screen.getByLabelText(/^Task run budget/) as HTMLInputElement).value).toBe('2'));
    expect((screen.getByRole('button', { name: 'Save prices' }) as HTMLButtonElement).closest('fieldset')?.disabled).toBe(true);
  });

  it('lets administrators raise a reached budget', async () => {
    const api = routes({ 'POST /api/v1/cost/budgets/user/alice%402026-11/raise': () => ({ ...reached, limitMicros: 9_000_000, reached: false, paused: false }) });
    vi.stubGlobal('prompt', () => '9');
    render(<CostsSection canEdit />);
    const alice = await screen.findByRole('listitem', { name: 'Budget User alice · 2026-11' });
    await userEvent.click(within(alice).getByRole('button', { name: 'Raise budget' }));
    await waitFor(() => expect(api.find('POST', '/api/v1/cost/budgets/user/alice%402026-11/raise')).toHaveLength(1));
    expect(api.find('POST', '/api/v1/cost/budgets/user/alice%402026-11/raise')[0].body).toEqual({ limitMicros: 9_000_000 });
    vi.stubGlobal('prompt', () => '1');
    await userEvent.click(within(alice).getByRole('button', { name: 'Raise budget' }));
    expect(await within(alice).findByText('Enter an amount higher than the current limit.')).toBeTruthy();
  });

  it('shows pending resumptions from the ledger after reload', async () => {
    let ledger = reached;
    routes({
      'GET /api/v1/cost/ledgers': () => [ledger],
      'POST /api/v1/cost/budgets/user/alice%402026-11/raise': () => {
        ledger = { ...reached, limitMicros: 9_000_000, reached: false, notResumed: ['task-run-fixture'] };
        return ledger;
      },
    });
    vi.stubGlobal('prompt', () => '9');
    const { unmount } = render(<CostsSection canEdit />);
    await userEvent.click(await screen.findByRole('button', { name: 'Raise budget' }));
    const pending = 'Resume pending for task-run-fixture. Weft will retry automatically.';
    expect(await screen.findByText(pending)).toBeTruthy();
    expect(screen.getByText('Runs paused')).toBeTruthy();
    expect(screen.queryByRole('alert')).toBeNull();
    unmount();
    render(<CostsSection canEdit={false} />);
    expect(await screen.findByText(pending)).toBeTruthy();
  });

  it('saves prices and budgets as micro-units and reports server validation', async () => {
    let failPrices = true;
    const api = routes({
      'PUT /api/v1/cost/prices': (c) => (failPrices ? apiError(422, 'invalid price catalogue: currency', 'invalid') : { ...(c.body as object), version: 3 }),
      'PUT /api/v1/cost/policy': (c) => c.body,
      'PUT /api/v1/cost/budgets': (c) => ({ ...(c.body as object), period: '2026-11' }),
    });
    const user = userEvent.setup();
    render(<CostsSection canEdit />);
    const prices = await screen.findByRole('form', { name: 'Model prices' });
    await waitFor(() => expect(within(prices).getAllByRole('group')).toHaveLength(1));
    await user.click(within(prices).getByRole('button', { name: 'Add price' }));
    const row = within(prices).getByRole('group', { name: 'Price 2' });
    await user.selectOptions(within(row).getByLabelText('Applies to'), 'type');
    await user.type(within(row).getByLabelText('Plugin id'), 'ollama');
    await user.type(within(row).getByLabelText(/^Input/), '0');
    await user.type(within(row).getByLabelText(/^Output/), '0.5');
    await user.click(within(prices).getByRole('button', { name: 'Save prices' }));
    expect(await within(prices).findByText(/invalid price catalogue/)).toBeTruthy();
    failPrices = false;
    await user.click(within(prices).getByRole('button', { name: 'Save prices' }));
    expect(await within(prices).findByText('Prices saved as version 3.')).toBeTruthy();
    expect(api.find('PUT', '/api/v1/cost/prices').at(-1)?.body).toEqual({
      currency: 'USD', unpriced: 'allow', prices: [
        { provider: 'team', model: '*', inputMicrosPerMTok: 2_500_000, outputMicrosPerMTok: 10_000_000 },
        { type: 'ollama', model: '*', inputMicrosPerMTok: 0, outputMicrosPerMTok: 500_000 },
      ],
    });

    const budgets = screen.getByRole('form', { name: 'Budget settings' });
    await user.click(within(budgets).getByRole('button', { name: 'Add monthly budget' }));
    await user.click(within(budgets).getByRole('button', { name: 'Save budgets' }));
    expect(await within(budgets).findByText(/Enter a limit for the installation budget/)).toBeTruthy();
    await user.type(within(within(budgets).getByRole('group', { name: 'Monthly budget 2' })).getByLabelText(/^Monthly limit/), '100');
    await user.clear(within(budgets).getByLabelText(/^Task run budget/));
    await user.click(within(budgets).getByRole('button', { name: 'Save budgets' }));
    expect(await within(budgets).findByText('Budgets saved.')).toBeTruthy();
    expect(api.find('PUT', '/api/v1/cost/policy')[0].body).toEqual({ taskRunBudgetMicros: 0, warningPercent: 80 });
    expect(api.find('PUT', '/api/v1/cost/budgets')[0].body).toEqual({ budgets: [
      { scope: 'user', id: 'alice', limitMicros: 5_000_000 },
      { scope: 'installation', id: '*', limitMicros: 100_000_000 },
    ] });
  });

  it('surfaces load failures', async () => {
    routes({ 'GET /api/v1/cost/spend': () => apiError(503, 'events unavailable', 'unavailable') });
    render(<CostsSection canEdit />);
    expect(await screen.findByText('Could not load spend: events unavailable')).toBeTruthy();
  });
});
