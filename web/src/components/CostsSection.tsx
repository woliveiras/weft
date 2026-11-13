import { useEffect, useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { BudgetDefinition, BudgetView, Price, PriceCatalogue, SpendLine } from '../api/types';
import { useAsync, useThrottled } from '../hooks/useData';
import { useOptionalEventStream } from '../hooks/useEventStream';
import { Badge, Btn, ErrorBox } from './common';
import { budgetLabel, fromMicros, money, toMicros, usedPercent } from './cost';

/** Monthly spend, budgets and prices. Viewers read; administrators edit and raise budgets. */
export function CostsSection({ canEdit }: { canEdit: boolean }) {
  const spend = useAsync(() => api.spend(), []);
  const ledgers = useAsync(api.budgetLedgers, []);
  const refreshLedgers = useThrottled(() => void ledgers.reload(), 300);
  useOptionalEventStream({ types: ['budget.raised', 'budget.graph_transition', 'budget.ledger_invalid'] }, refreshLedgers);
  const currency = spend.data?.currency ?? 'USD';
  const reload = async () => {
    await Promise.all([spend.reload(), ledgers.reload()]);
  };
  return (
    <section className="integration-section" aria-labelledby="costs-heading">
      <div className="section-head">
        <div>
          <h2 id="costs-heading">Costs</h2>
          <p className="muted small">
            Spend is computed from the usage providers report and the prices entered here. Weft ships no vendor prices; local plugins cost nothing unless priced.
          </p>
        </div>
      </div>
      <ErrorBox error={spend.error ? `Could not load spend: ${spend.error}` : undefined} />
      {spend.data && (
        <div className="cost-spend" role="group" aria-label={`Spend in ${spend.data.period}`}>
          <h3>Spend in {spend.data.period}</h3>
          <p>
            <strong>{money(spend.data.total.costMicros, currency)}</strong> across {spend.data.total.calls} model calls
            {spend.data.total.estimatedMicros > 0 && <> · {money(spend.data.total.estimatedMicros, currency)} estimated (usage not reported)</>}
            {spend.data.total.unpricedCalls > 0 && <> · {spend.data.total.unpricedCalls} unpriced calls</>}
          </p>
          {spend.data.truncated && <p className="muted small">Only the most recent cost records were read.</p>}
          <SpendTable title="By provider and model" lines={spend.data.byProvider} currency={currency} model />
          <SpendTable title="By user" lines={spend.data.byUser} currency={currency} />
          <SpendTable title="By project" lines={spend.data.byProject} currency={currency} />
          <SpendTable title="By workflow" lines={spend.data.byWorkflow} currency={currency} />
          <SpendTable title="By task run" lines={spend.data.byTask} currency={currency} />
        </div>
      )}
      <ErrorBox error={ledgers.error ? `Could not load budgets: ${ledgers.error}` : undefined} />
      {ledgers.data && <BudgetBars budgets={ledgers.data} currency={currency} canRaise={canEdit} onChanged={reload} />}
      <BudgetSettings canEdit={canEdit} currency={currency} onSaved={reload} />
      <PriceSettings canEdit={canEdit} onSaved={reload} />
    </section>
  );
}

function SpendTable({ title, lines, currency, model }: { title: string; lines: SpendLine[]; currency: string; model?: boolean }) {
  if (lines.length === 0) return null;
  return (
    <table className="data-table" aria-label={title}>
      <caption>{title}</caption>
      <thead>
        <tr>
          <th>{model ? 'Provider' : 'Name'}</th>
          {model && <th>Model</th>}
          <th>Calls</th>
          <th>Tokens in / out</th>
          <th>Cost</th>
        </tr>
      </thead>
      <tbody>
        {lines.slice(0, 10).map((l) => (
          <tr key={l.key + (l.model ?? '')}>
            <td>{l.key}</td>
            {model && <td>{l.model}</td>}
            <td>{l.calls}</td>
            <td>
              {l.inputTokens} / {l.outputTokens}
            </td>
            <td>
              {money(l.costMicros, currency)}
              {l.unpricedCalls > 0 && ` (${l.unpricedCalls} unpriced)`}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

/** One budget as a bar with its warning and hard levels. */
export function BudgetBar({ budget, currency, canRaise, onRaised, showPause = true }: { budget: BudgetView; currency: string; canRaise: boolean; showPause?: boolean; onRaised: () => Promise<void> | void }) {
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const pct = usedPercent(budget);
  const pending = showPause ? budget.notResumed ?? [] : [];
  const raise = async () => {
    const text = prompt(`New limit for ${budgetLabel(budget)} (${currency})`, fromMicros(budget.limitMicros * 2));
    if (text === null) return;
    const micros = toMicros(text);
    if (!micros || micros <= budget.limitMicros) {
      setError('Enter an amount higher than the current limit.');
      return;
    }
    setBusy(true);
    setError(undefined);
    try {
      await api.raiseBudget(budget.scope, budget.id, micros);
      await onRaised();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  if (budget.invalid) {
    return (
      <li className="budget-bar" aria-label={`Budget ${budgetLabel(budget)}`}>
        <strong>{budgetLabel(budget)}</strong>
        <ErrorBox error={`Invalid budget ledger (revision ${budget.revision}). ${canRaise ? 'See the ledger recovery section in docs/cost-control.md.' : 'Contact an administrator.'}`} />
      </li>
    );
  }
  return (
    <li className="budget-bar" aria-label={`Budget ${budgetLabel(budget)}`}>
      <div className="integration-card-head">
        <strong>{budgetLabel(budget)}</strong>
        <span className="integration-card-status">
          {budget.reached ? <Badge tone="bad">Reached</Badge> : pct >= 80 ? <Badge tone="warn">Warning</Badge> : <Badge tone="ok">Within budget</Badge>}
          {showPause && budget.paused && <Badge tone="warn">Runs paused</Badge>}
        </span>
      </div>
      <meter min={0} max={100} low={80} high={99} optimum={0} value={Math.min(pct, 100)} aria-label={`${pct}% used`} />
      <p className="small">
        {money(budget.chargedMicros, currency)} spent
        {budget.reservedMicros > 0 && ` · ${money(budget.reservedMicros, currency)} reserved`} of {money(budget.limitMicros, currency)} ·{' '}
        {money(budget.remainingMicros, currency)} left
        {budget.estimatedMicros > 0 && ` · ${money(budget.estimatedMicros, currency)} estimated`}
        {budget.raisedBy && ` · raised by ${budget.raisedBy}`}
      </p>
      {pending.length > 0 && <p className="small" role="status">Resume pending for {pending.join(', ')}. Weft will retry automatically.</p>}
      {canRaise && showPause && (budget.reached || (budget.paused && pending.length === 0)) && (
        <Btn small tone="primary" disabled={busy} onClick={() => void raise()}>
          Raise budget
        </Btn>
      )}
      <ErrorBox error={error} />
    </li>
  );
}

function BudgetBars({ budgets, currency, canRaise, onChanged }: { budgets: BudgetView[]; currency: string; canRaise: boolean; onChanged: () => Promise<void> }) {
  return (
    <div role="group" aria-labelledby="budget-bars-heading">
      <h3 id="budget-bars-heading">Budgets this month</h3>
      {budgets.length === 0 ? (
        <p className="empty">No budget has been used this month.</p>
      ) : (
        <ul className="integration-grid">
          {budgets.map((b) => (
            <BudgetBar key={b.scope + b.id} budget={b} currency={b.currency || currency} canRaise={canRaise} onRaised={onChanged} />
          ))}
        </ul>
      )}
    </div>
  );
}

type DefinitionRow = { scope: BudgetDefinition['scope']; id: string; limit: string; interrupt: string };

function BudgetSettings({ canEdit, currency, onSaved }: { canEdit: boolean; currency: string; onSaved: () => Promise<void> }) {
  const policy = useAsync(api.costPolicy, []);
  const defs = useAsync(api.budgetDefinitions, []);
  const [runBudget, setRunBudget] = useState('');
  const [warning, setWarning] = useState('80');
  const [rows, setRows] = useState<DefinitionRow[]>([]);
  const [error, setError] = useState<string>();
  const [saved, setSaved] = useState<string>();
  useEffect(() => {
    if (policy.data) {
      setRunBudget(policy.data.taskRunBudgetMicros ? fromMicros(policy.data.taskRunBudgetMicros) : '');
      setWarning(String(policy.data.warningPercent || 80));
    }
  }, [policy.data]);
  useEffect(() => {
    if (defs.data) setRows(defs.data.budgets.map((d) => ({ scope: d.scope, id: d.id, limit: fromMicros(d.limitMicros), interrupt: d.interruptPercent ? String(d.interruptPercent) : '' })));
  }, [defs.data]);
  const set = (i: number, patch: Partial<DefinitionRow>) => setRows((cur) => cur.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  const save = async () => {
    setError(undefined);
    setSaved(undefined);
    const run = runBudget.trim() === '' ? 0 : toMicros(runBudget);
    if (run === undefined) return setError('Enter the task run budget as an amount, e.g. 2.50, or leave it empty for none.');
    const budgets: BudgetDefinition[] = [];
    for (const r of rows) {
      const limit = toMicros(r.limit);
      if (!limit) return setError(`Enter a limit for the ${r.scope} budget ${r.id || ''}.`);
      budgets.push({ scope: r.scope, id: r.scope === 'installation' ? '*' : r.id.trim(), limitMicros: limit, interruptPercent: r.interrupt ? Number(r.interrupt) : undefined });
    }
    try {
      await api.saveCostPolicy({ taskRunBudgetMicros: run, warningPercent: Number(warning) || 80 });
      await api.saveBudgetDefinitions(budgets);
      await Promise.all([policy.reload(), defs.reload()]);
      await onSaved();
      setSaved('Budgets saved.');
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  return (
    <form className="card form" aria-label="Budget settings" onSubmit={(e) => { e.preventDefault(); void save(); }}>
      <h3>Budget settings</h3>
      <ErrorBox error={policy.error || defs.error} />
      <fieldset disabled={!canEdit}>
        <label>
          Task run budget ({currency})
          <input value={runBudget} onChange={(e) => setRunBudget(e.target.value)} placeholder="No budget" inputMode="decimal" />
        </label>
        <label>
          Warning level (%)
          <input value={warning} onChange={(e) => setWarning(e.target.value)} inputMode="numeric" />
        </label>
        <p className="muted small">Monthly budgets restart every calendar month (UTC). The tightest applicable budget decides; reaching one pauses runs and refuses new tasks until it is raised.</p>
        {rows.map((r, i) => (
          <div className="row" key={i} role="group" aria-label={`Monthly budget ${i + 1}`}>
            <label>
              Scope
              <select value={r.scope} onChange={(e) => set(i, { scope: e.target.value as DefinitionRow['scope'] })}>
                <option value="installation">Installation</option>
                <option value="user">User</option>
                <option value="project">Project</option>
                <option value="workflow">Workflow</option>
              </select>
            </label>
            {r.scope !== 'installation' && (
              <label>
                Applies to
                <input value={r.id} onChange={(e) => set(i, { id: e.target.value })} placeholder={r.scope === 'user' ? 'user name' : r.scope === 'project' ? 'project id' : 'workflow id'} />
              </label>
            )}
            <label>
              Monthly limit ({currency})
              <input value={r.limit} onChange={(e) => set(i, { limit: e.target.value })} inputMode="decimal" />
            </label>
            <label>
              Interrupt level (%)
              <input value={r.interrupt} onChange={(e) => set(i, { interrupt: e.target.value })} placeholder="None" inputMode="numeric" />
            </label>
            <Btn type="button" small onClick={() => setRows((cur) => cur.filter((_, j) => j !== i))}>
              Remove
            </Btn>
          </div>
        ))}
        <div className="row">
          <Btn type="button" small onClick={() => setRows((cur) => [...cur, { scope: 'installation', id: '*', limit: '', interrupt: '' }])}>
            Add monthly budget
          </Btn>
          <Btn type="submit" tone="primary">
            Save budgets
          </Btn>
        </div>
      </fieldset>
      {saved && <p role="status">{saved}</p>}
      <ErrorBox error={error} />
    </form>
  );
}

type PriceRow = { target: 'provider' | 'type'; name: string; model: string; input: string; output: string; cacheRead: string; cacheWrite: string };

function rowFrom(p: Price): PriceRow {
  return {
    target: p.provider ? 'provider' : 'type', name: p.provider || p.type || '', model: p.model,
    input: fromMicros(p.inputMicrosPerMTok), output: fromMicros(p.outputMicrosPerMTok),
    cacheRead: fromMicros(p.cacheReadMicrosPerMTok), cacheWrite: fromMicros(p.cacheWriteMicrosPerMTok),
  };
}

function PriceSettings({ canEdit, onSaved }: { canEdit: boolean; onSaved: () => Promise<void> }) {
  const catalogue = useAsync(api.prices, []);
  const [currency, setCurrency] = useState('USD');
  const [unpriced, setUnpriced] = useState<PriceCatalogue['unpriced']>('allow');
  const [rows, setRows] = useState<PriceRow[]>([]);
  const [error, setError] = useState<string>();
  const [saved, setSaved] = useState<string>();
  useEffect(() => {
    if (catalogue.data) {
      setCurrency(catalogue.data.currency);
      setUnpriced(catalogue.data.unpriced);
      setRows(catalogue.data.prices.map(rowFrom));
    }
  }, [catalogue.data]);
  const set = (i: number, patch: Partial<PriceRow>) => setRows((cur) => cur.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  const save = async () => {
    setError(undefined);
    setSaved(undefined);
    const prices: Price[] = [];
    for (const r of rows) {
      const input = toMicros(r.input), output = toMicros(r.output);
      const read = r.cacheRead ? toMicros(r.cacheRead) : undefined, write = r.cacheWrite ? toMicros(r.cacheWrite) : undefined;
      if (input === undefined || output === undefined || (r.cacheRead && read === undefined) || (r.cacheWrite && write === undefined)) {
        return setError(`Enter prices per million tokens as amounts for ${r.name || 'every row'}.`);
      }
      prices.push({ [r.target]: r.name.trim(), model: r.model.trim() || '*', inputMicrosPerMTok: input, outputMicrosPerMTok: output, cacheReadMicrosPerMTok: read, cacheWriteMicrosPerMTok: write } as Price);
    }
    try {
      const next = await api.savePrices({ currency, unpriced, prices });
      await catalogue.reload();
      await onSaved();
      setSaved(`Prices saved as version ${next.version}.`);
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  return (
    <form className="card form" aria-label="Model prices" onSubmit={(e) => { e.preventDefault(); void save(); }}>
      <h3>Model prices</h3>
      {catalogue.data && (
        <p className="muted small">
          {catalogue.data.version ? `Version ${catalogue.data.version}${catalogue.data.updatedBy ? ` by ${catalogue.data.updatedBy}` : ''}.` : 'No prices saved yet.'} Prices are per million
          tokens; the most specific row (provider, then plugin; exact model, then *) wins.
        </p>
      )}
      <ErrorBox error={catalogue.error} />
      <fieldset disabled={!canEdit}>
        <label>
          Currency
          <input value={currency} onChange={(e) => setCurrency(e.target.value.toUpperCase())} maxLength={3} />
        </label>
        <label>
          Unpriced models
          <select value={unpriced} onChange={(e) => setUnpriced(e.target.value as PriceCatalogue['unpriced'])}>
            <option value="allow">Allow and show as unpriced</option>
            <option value="refuse">Refuse while a budget applies</option>
          </select>
        </label>
        {rows.map((r, i) => (
          <div className="row" key={i} role="group" aria-label={`Price ${i + 1}`}>
            <label>
              Applies to
              <select value={r.target} onChange={(e) => set(i, { target: e.target.value as PriceRow['target'] })}>
                <option value="provider">Provider</option>
                <option value="type">Plugin</option>
              </select>
            </label>
            <label>
              {r.target === 'provider' ? 'Provider name' : 'Plugin id'}
              <input value={r.name} onChange={(e) => set(i, { name: e.target.value })} />
            </label>
            <label>
              Model
              <input value={r.model} onChange={(e) => set(i, { model: e.target.value })} placeholder="*" />
            </label>
            <label>
              Input ({currency}/M tokens)
              <input value={r.input} onChange={(e) => set(i, { input: e.target.value })} inputMode="decimal" />
            </label>
            <label>
              Output ({currency}/M tokens)
              <input value={r.output} onChange={(e) => set(i, { output: e.target.value })} inputMode="decimal" />
            </label>
            <label>
              Cache read
              <input value={r.cacheRead} onChange={(e) => set(i, { cacheRead: e.target.value })} placeholder="Input rate" inputMode="decimal" />
            </label>
            <label>
              Cache write
              <input value={r.cacheWrite} onChange={(e) => set(i, { cacheWrite: e.target.value })} placeholder="Input rate" inputMode="decimal" />
            </label>
            <Btn type="button" small onClick={() => setRows((cur) => cur.filter((_, j) => j !== i))}>
              Remove
            </Btn>
          </div>
        ))}
        <div className="row">
          <Btn type="button" small onClick={() => setRows((cur) => [...cur, { target: 'provider', name: '', model: '*', input: '', output: '', cacheRead: '', cacheWrite: '' }])}>
            Add price
          </Btn>
          <Btn type="submit" tone="primary">
            Save prices
          </Btn>
        </div>
      </fieldset>
      {saved && <p role="status">{saved}</p>}
      <ErrorBox error={error} />
    </form>
  );
}
