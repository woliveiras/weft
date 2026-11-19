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

  it('explains a raise that remains below committed and reserved spend', async () => {
    let ledger = { ...reached, chargedMicros: 7_000_000 };
    routes({
      'GET /api/v1/cost/ledgers': () => [ledger],
      'POST /api/v1/cost/budgets/user/alice%402026-11/raise': () => {
        ledger = { ...ledger, limitMicros: 6_000_000, raisedBy: 'admin' };
        return ledger;
      },
    });
    vi.stubGlobal('prompt', () => '6');
    render(<CostsSection canEdit />);
    await userEvent.click(await screen.findByRole('button', { name: 'Raise budget' }));
    expect(await screen.findByText('The raised limit is still at or below current spend and reservations.')).toBeTruthy();
    expect(screen.getByText('Runs paused')).toBeTruthy();
    expect(screen.queryByText(/Resume pending/)).toBeNull();
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

    await user.click(screen.getByRole('button', { name: 'Add monthly budget' }));
    const budgets = screen.getByRole('form', { name: 'Add monthly budget' });
    await user.click(within(budgets).getByRole('button', { name: 'Save monthly budget' }));
    expect(await within(budgets).findByText(/Enter a monthly limit greater than zero/)).toBeTruthy();
    await user.type(within(budgets).getByLabelText(/^Monthly limit/), '100');
    await user.click(within(budgets).getByRole('button', { name: 'Save monthly budget' }));
    expect(await screen.findByText('Monthly budget added with a limit of $100.00.')).toBeTruthy();
    expect(api.find('PUT', '/api/v1/cost/policy')).toHaveLength(0);
    const defaults = screen.getByRole('form', {name:'Default budget per task'});
    await user.clear(within(defaults).getByLabelText(/^Task run budget/));
    await user.click(within(defaults).getByRole('button', {name:'Save task defaults'}));
    expect(await within(defaults).findByText(/Defaults saved/)).toBeTruthy();
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

it('identifies an invalid ledger and preserves valid budget controls', async () => {
  routes({ 'GET /api/v1/cost/ledgers': () => [{ ...healthy, invalid: true, revision: 7 }, reached] });
  render(<CostsSection canEdit />);
  const invalid = await screen.findByRole('listitem', { name: 'Budget Installation · 2026-11' });
  expect(within(invalid).getByRole('alert').textContent).toContain('Invalid budget ledger (revision 7)');
  expect(within(invalid).queryByRole('meter')).toBeNull();
  expect(within(invalid).queryByRole('button', { name: 'Raise budget' })).toBeNull();
  expect(within(screen.getByRole('listitem', { name: 'Budget User alice · 2026-11' })).getByRole('button', { name: 'Raise budget' })).toBeTruthy();
});

it('paginates usage by descending cost and combines category and search',async()=>{
  const user=userEvent.setup();
  routes({'GET /api/v1/cost/spend':()=>({...spend,byProvider:Array.from({length:21},(_,index)=>line(`provider-${index}`,index*1_000_000,'fixture'))})});
  render(<CostsSection canEdit={false} section="usage"/>);
  const table=await screen.findByRole('table',{name:'By provider and model'});
  expect(within(table).getAllByRole('row')).toHaveLength(21);
  expect(within(table).getAllByRole('row')[1].textContent).toContain('provider-20');
  await user.click(screen.getByRole('button',{name:'Next'}));
  expect(within(table).getAllByRole('row')).toHaveLength(2);
  expect(within(table).getByText('provider-0')).toBeTruthy();
  await user.type(screen.getByLabelText('Search usage'),'PROVIDER-20');
  expect(within(table).getByText('provider-20')).toBeTruthy();
  expect(screen.queryByRole('button',{name:'Next'})).toBeNull();
  await user.selectOptions(screen.getByLabelText('Breakdown'),'byUser');
  expect(await screen.findByRole('table',{name:'By user'})).toBeTruthy();
  expect(screen.getByText('alice')).toBeTruthy();
  expect(screen.queryByRole('form',{name:'Model prices'})).toBeNull();
});
it('keeps pricing unavailable after a load failure and allows an explicit retry',async()=>{
  let failed=true; const user=userEvent.setup();
  const fixture=routes({'GET /api/v1/cost/prices':()=>failed ? apiError(503,'Prices unavailable','unavailable') : {version:1,currency:'USD',unpriced:'allow',prices:[]}});
  render(<CostsSection canEdit section="prices"/>);
  await screen.findByText('Prices unavailable');
  expect(screen.getByRole('button',{name:'Save prices'}).matches(':disabled')).toBe(true);
  await user.click(screen.getByRole('button',{name:'Save prices'}));
  expect(fixture.find('PUT','/api/v1/cost/prices')).toHaveLength(0);
  failed=false; await user.click(screen.getByRole('button',{name:'Retry model prices'}));
  await waitFor(()=>expect(screen.getByRole('button',{name:'Save prices'}).matches(':disabled')).toBe(false));
});

it('edits a current monthly budget inline without saving task defaults or changing recorded spend',async()=>{
  const user=userEvent.setup();
  let defs=[{scope:'installation',id:'*',limitMicros:100_000_000,warningPercent:65}];
  const fixture=routes({
    'GET /api/v1/cost/budgets':()=>({budgets:defs,period:'2026-11'}),
    'GET /api/v1/cost/ledgers':()=>[{...healthy,limitMicros:defs[0].limitMicros,remainingMicros:defs[0].limitMicros-healthy.chargedMicros}],
    'PUT /api/v1/cost/budgets':call=>{defs=(call.body as {budgets:typeof defs}).budgets;return {budgets:defs,period:'2026-11'};},
  });
  render(<CostsSection canEdit section="budgets"/>);
  expect(await screen.findByText('November 2026 · UTC')).toBeTruthy();
  const card=screen.getByRole('listitem',{name:'Budget Installation · 2026-11'});
  await waitFor(()=>expect(within(card).getByRole('button',{name:'Edit budget'}).matches(':disabled')).toBe(false));
  const edit=within(card).getByRole('button',{name:'Edit budget'});
  await user.click(edit);
  const form=within(card).getByRole('form',{name:'Edit Installation budget'});
  const limit=within(form).getByLabelText(/^Monthly limit/) as HTMLInputElement;
  expect(limit.value).toBe('100');expect(document.activeElement).toBe(limit);
  await user.clear(limit);await user.type(limit,'120');
  await user.click(within(form).getByRole('button',{name:'Cancel'}));
  expect(fixture.find('PUT','/api/v1/cost/budgets')).toHaveLength(0);
  expect(document.activeElement).toBe(edit);
  await user.click(edit);await user.clear(screen.getByLabelText(/^Monthly limit/));await user.type(screen.getByLabelText(/^Monthly limit/),'150');
  await user.click(screen.getByRole('button',{name:'Save monthly budget'}));
  expect(await screen.findByText('Monthly limit updated from $100.00 to $150.00.')).toBeTruthy();
  expect(fixture.find('PUT','/api/v1/cost/policy')).toHaveLength(0);
  expect(defs[0].warningPercent).toBe(65);
  expect(within(card).getByText('$10.00')).toBeTruthy();
  await waitFor(()=>expect(within(card).getByText('$140.00')).toBeTruthy());
  expect(document.activeElement).toBe(edit);
});

it('retains monthly edits after validation or service failure and preserves a manually raised floor notice',async()=>{
  const user=userEvent.setup();let fail=true;
  const fixture=routes({
    'GET /api/v1/cost/ledgers':()=>[{...reached,raisedBy:'admin'}],
    'PUT /api/v1/cost/budgets':call=>fail?apiError(503,'Budget service unavailable','unavailable'):{...(call.body as object),period:'2026-11'},
  });
  render(<CostsSection canEdit section="budgets"/>);
  const card=await screen.findByRole('listitem',{name:'Budget User alice · 2026-11'});
  await waitFor(()=>expect(within(card).getByRole('button',{name:'Edit budget'}).matches(':disabled')).toBe(false));
  await user.click(within(card).getByRole('button',{name:'Edit budget'}));
  expect(screen.getByText(/effective limit cannot fall below/)).toBeTruthy();
  const input=screen.getByLabelText(/^Monthly limit/) as HTMLInputElement;
  await user.clear(input);await user.type(input,'0');await user.click(screen.getByRole('button',{name:'Save monthly budget'}));
  expect(await screen.findByText(/Enter a monthly limit greater than zero/)).toBeTruthy();
  expect(fixture.find('PUT','/api/v1/cost/budgets')).toHaveLength(0);
  await user.clear(input);await user.type(input,'4');
  expect(screen.getByText(/below current spending and reservations/)).toBeTruthy();
  await user.click(screen.getByRole('button',{name:'Save monthly budget'}));
  expect(await screen.findByText('Budget service unavailable')).toBeTruthy();expect(input.value).toBe('4');
  fail=false;await user.click(screen.getByRole('button',{name:'Save monthly budget'}));
  expect(await screen.findByText('Monthly limit updated from $5.00 to $4.00.')).toBeTruthy();
});

it('keeps unavailable monthly settings read-only and restores edit after retry',async()=>{
  const user=userEvent.setup();let fail=true;
  routes({'GET /api/v1/cost/budgets':()=>fail?apiError(503,'Definitions unavailable','unavailable'):{budgets:[],period:'2026-11'}});
  render(<CostsSection canEdit section="budgets"/>);
  expect(await screen.findByText(/Could not load monthly budget settings/)).toBeTruthy();
  expect(screen.getByRole('button',{name:'Add monthly budget'}).matches(':disabled')).toBe(true);
  fail=false;await user.click(screen.getByRole('button',{name:'Retry monthly budgets'}));
  await waitFor(()=>expect(screen.getByRole('button',{name:'Add monthly budget'}).matches(':disabled')).toBe(false));
});

it('saves task defaults independently and retains values on failure',async()=>{
  const user=userEvent.setup();let fail=true;
  const fixture=routes({'PUT /api/v1/cost/policy':call=>fail?apiError(503,'Policy unavailable','unavailable'):call.body});
  render(<CostsSection canEdit section="budgets"/>);
  const form=screen.getByRole('form',{name:'Default budget per task'});
  const limit=within(form).getByLabelText(/^Task run budget/) as HTMLInputElement;
  await waitFor(()=>expect(limit.value).toBe('2'));
  await user.clear(limit);await user.type(limit,'3');
  await user.clear(within(form).getByLabelText(/^Default warning/));await user.type(within(form).getByLabelText(/^Default warning/),'101');
  await user.click(within(form).getByRole('button',{name:'Save task defaults'}));
  expect(await within(form).findByText('Warning level must be between 1 and 100.')).toBeTruthy();
  expect(fixture.find('PUT','/api/v1/cost/policy')).toHaveLength(0);
  await user.clear(within(form).getByLabelText(/^Default warning/));await user.type(within(form).getByLabelText(/^Default warning/),'70');
  await user.click(within(form).getByRole('button',{name:'Save task defaults'}));
  expect(await within(form).findByText('Policy unavailable')).toBeTruthy();expect(limit.value).toBe('3');
  fail=false;await user.click(within(form).getByRole('button',{name:'Save task defaults'}));
  expect(await within(form).findByText(/Defaults saved/)).toBeTruthy();
  expect(fixture.find('PUT','/api/v1/cost/budgets')).toHaveLength(0);
  expect(fixture.find('PUT','/api/v1/cost/policy').at(-1)?.body).toEqual({taskRunBudgetMicros:3_000_000,warningPercent:70});
});

it('validates duplicate scopes and confirms removal without erasing other definitions',async()=>{
  const user=userEvent.setup();
  const fixture=routes({'PUT /api/v1/cost/budgets':call=>({...(call.body as object),period:'2026-11'})});
  render(<CostsSection canEdit section="budgets"/>);
  await user.click(await screen.findByRole('button',{name:'Add monthly budget'}));
  const form=screen.getByRole('form',{name:'Add monthly budget'});
  await user.selectOptions(within(form).getByLabelText('Scope'),'user');
  await user.type(within(form).getByLabelText('Applies to'),'alice');
  await user.type(within(form).getByLabelText(/^Monthly limit/),'2');
  await user.click(within(form).getByRole('button',{name:'Save monthly budget'}));
  expect(await screen.findByText(/A monthly budget already exists/)).toBeTruthy();
  expect(fixture.find('PUT','/api/v1/cost/budgets')).toHaveLength(0);
  await user.click(within(form).getByRole('button',{name:'Cancel'}));
  const card=screen.getByRole('listitem',{name:'Budget User alice · 2026-11'});
  await user.click(within(card).getByRole('button',{name:'Edit budget'}));
  await user.click(screen.getByRole('button',{name:'Remove budget'}));
  expect(screen.getByText(/Remove this recurring limit/)).toBeTruthy();
  expect(fixture.find('PUT','/api/v1/cost/budgets')).toHaveLength(0);
  await user.click(screen.getByRole('button',{name:'Confirm removal'}));
  expect(await screen.findByText('Monthly budget removed. Recorded spending is preserved.')).toBeTruthy();
  expect(fixture.find('PUT','/api/v1/cost/budgets')[0].body).toEqual({budgets:[]});
});

it('does not claim zero spending or allow edits when monthly balances are unavailable',async()=>{
  routes({'GET /api/v1/cost/ledgers':()=>apiError(503,'Balances unavailable','unavailable')});
  render(<CostsSection canEdit section="budgets"/>);
  expect(await screen.findByText('Current balance unavailable. Refresh budget balances before editing.')).toBeTruthy();
  expect(screen.queryByText('No spending recorded for this budget this month.')).toBeNull();
  expect(screen.getByRole('button',{name:'Edit budget'}).matches(':disabled')).toBe(true);
  expect(screen.getByRole('button',{name:'Add monthly budget'}).matches(':disabled')).toBe(true);
});
