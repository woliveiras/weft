import {useFieldValidation} from './useFieldValidation';
import { HelpField } from './FieldHelp';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { api, errorMessage } from '../api/client';
import type { BudgetDefinition, BudgetView, Price, PriceCatalogue, SpendLine, SpendReport } from '../api/types';
import { useAsync, useThrottled } from '../hooks/useData';
import { useOptionalEventStream } from '../hooks/useEventStream';
import { Badge, Btn, ErrorBox } from './common';
import { budgetLabel, budgetPeriodLabel, fromMicros, money, toMicros, usedPercent } from './cost';

/** Monthly spend, budgets and prices. Viewers read; administrators edit and raise budgets. */
export function CostsSection({ canEdit, section }: { canEdit: boolean; section?:'usage'|'budgets'|'prices' }) {
  const spend = useAsync(() => section==='prices' ? Promise.resolve(undefined) : api.spend(), [section]);
  const ledgers = useAsync(() => !section || section==='budgets' ? api.budgetLedgers() : Promise.resolve(undefined), [section]);
  const refreshLedgers = useThrottled(() => void ledgers.reload(), 300);
  useOptionalEventStream({ types: ['budget.raised', 'budget.graph_transition', 'budget.ledger_invalid'] }, refreshLedgers);
  const currency = spend.data?.currency ?? 'USD';
  const showUsage=!section || section==='usage';
  const showBudgets=!section || section==='budgets';
  const showPrices=!section || section==='prices';
  const reload = async () => {
    await Promise.all([spend.reload(), ledgers.reload()]);
  };
  return (
    <section className="integration-section" aria-labelledby="costs-heading">
      <div className="section-head">
        <div>
          <h2 id="costs-heading">{section==='budgets'?'Budgets':section==='prices'?'Model prices':'Costs'}</h2>
          <p className="muted small">
            {section==='budgets' ? 'Review current monthly limits or set the default budget for new task runs.' : 'Spend is computed from the usage providers report and the prices entered here. Weft ships no vendor prices; local plugins cost nothing unless priced.'}
          </p>
        </div>
      </div>
      <ErrorBox error={spend.error ? `Could not load spend: ${spend.error}` : undefined} />
      {showUsage && <>
        {spend.loading && !spend.data && <p role="status">Loading usage…</p>}
        {spend.error && <Btn onClick={()=>void spend.reload()}>Retry usage</Btn>}
        {spend.data && <UsageReport report={spend.data}/>}
      </>}
      <ErrorBox error={ledgers.error ? `Could not load budgets: ${ledgers.error}` : undefined} />
      {showBudgets && ledgers.loading && !ledgers.data && <p role="status">Loading budgets…</p>}
      {showBudgets && ledgers.error && <Btn onClick={()=>void ledgers.reload()}>Retry budgets</Btn>}
      {showBudgets && <MonthlyBudgets budgets={ledgers.data} ledgersUnavailable={!!ledgers.error || ledgers.loading} period={spend.data?.period} canEdit={canEdit} currency={currency} onSaved={reload} />}
      {showBudgets && <DefaultTaskBudget canEdit={canEdit} currency={currency} />}
      {showPrices && <PriceSettings canEdit={canEdit} onSaved={reload} />}
    </section>
  );
}

const breakdowns = {byProvider:'By provider and model',byUser:'By user',byProject:'By project',byWorkflow:'By workflow',byTask:'By task run'} as const;
type Breakdown = keyof typeof breakdowns;
function UsageReport({report}: {report:SpendReport}) {
  const [breakdown,setBreakdown]=useState<Breakdown>('byProvider');
  const [search,setSearch]=useState('');
  const [page,setPage]=useState(0);
  const query=search.trim().toLowerCase();
  const lines=report[breakdown].filter(line=>`${line.key} ${line.model??''}`.toLowerCase().includes(query))
    .slice().sort((a,b)=>b.costMicros-a.costMicros || a.key.localeCompare(b.key));
  const pages=Math.max(1,Math.ceil(lines.length/20));
  const current=Math.min(page,pages-1);
  return <div className="cost-spend" role="group" aria-label={`Spend in ${report.period}`}>
    <h3>Spend in {report.period}</h3>
    <div className="model-usage-summary">
      <div className="card"><span className="muted small">Recorded cost</span><strong>{money(report.total.costMicros,report.currency)}</strong></div>
      <div className="card"><span className="muted small">Model calls</span><strong>{report.total.calls.toLocaleString()}</strong></div>
      <div className="card"><span className="muted small">Input / output tokens</span><strong>{report.total.inputTokens.toLocaleString()} / {report.total.outputTokens.toLocaleString()}</strong></div>
    </div>
    {report.total.estimatedMicros>0 && <p>{money(report.total.estimatedMicros,report.currency)} estimated (usage not reported).</p>}
    {report.total.unpricedCalls>0 && <p>{report.total.unpricedCalls} unpriced calls. Configure prices to account for these models.</p>}
    {report.truncated && <p role="note">Only the most recent cost records were read.</p>}
    <div className="card model-usage-details">
      <div className="model-provider-filters">
        <label>Breakdown<select value={breakdown} onChange={event=>{setBreakdown(event.target.value as Breakdown);setSearch('');setPage(0);}}>{Object.entries(breakdowns).map(([value,label])=><option key={value} value={value}>{label}</option>)}</select></label>
        <label>Search usage<input type="search" value={search} onChange={event=>{setSearch(event.target.value);setPage(0);}} placeholder="Name, ID or model"/></label>
      </div>
      <p className="muted small">{lines.length} records · Highest cost first</p>
      {lines.length===0 ? <p className="empty">{query?'No usage matches this search.':'No usage recorded for this breakdown.'}</p> : <div className="model-table-scroll"><SpendTable title={breakdowns[breakdown]} lines={lines.slice(current*20,(current+1)*20)} currency={report.currency} model={breakdown==='byProvider'}/></div>}
      {pages>1 && <div className="btn-row"><Btn disabled={current===0} onClick={()=>setPage(current-1)}>Previous</Btn><span>Page {current+1} of {pages}</span><Btn disabled={current>=pages-1} onClick={()=>setPage(current+1)}>Next</Btn></div>}
    </div>
  </div>;
}
function SpendTable({title,lines,currency,model}: {title:string;lines:SpendLine[];currency:string;model?:boolean}) {
  return <table className="table model-spend-table" aria-label={title}><caption>{title}</caption><thead><tr>
    <th>{model?'Provider':'Name / ID'}</th>{model && <th>Model</th>}<th>Calls</th><th>Tokens in / out</th><th>Cost</th>
  </tr></thead><tbody>{lines.map(line=><tr key={line.key+(line.model??'')}><td>{line.key}</td>{model && <td>{line.model}</td>}
    <td>{line.calls.toLocaleString()}</td><td>{line.inputTokens.toLocaleString()} / {line.outputTokens.toLocaleString()}</td><td>{money(line.costMicros,currency)}{line.unpricedCalls>0 && ` (${line.unpricedCalls} unpriced)`}</td>
  </tr>)}</tbody></table>;
}

/** One budget as a bar with its warning and hard levels. */
export function BudgetBar({ budget, currency, canRaise, onRaised, showPause = true, onEdit, editDisabled, editor, detailed }: { budget: BudgetView; currency: string; canRaise: boolean; showPause?: boolean; onRaised: () => Promise<void> | void; onEdit?: (button:HTMLButtonElement) => void; editDisabled?: boolean; editor?: ReactNode; detailed?:boolean }) {
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const pct = usedPercent(budget);
  const pending = showPause ? budget.notResumed ?? [] : [];
  const raiseStillReached = showPause && budget.raisedBy && budget.reached && pending.length === 0 &&
    budget.chargedMicros + budget.reservedMicros >= budget.limitMicros;
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
    <li className={`budget-bar ${editor?'is-editing':''}`} aria-label={`Budget ${budgetLabel(budget)}`}>
      <div className="integration-card-head">
        <strong>{budgetLabel(budget)}</strong>
        <span className="integration-card-status">
          {budget.reached ? <Badge tone="bad">Reached</Badge> : pct >= 80 ? <Badge tone="warn">Warning</Badge> : <Badge tone="ok">Within budget</Badge>}
          {showPause && budget.paused && <Badge tone="warn">Runs paused</Badge>}
        </span>
      </div>
      <meter min={0} max={100} low={80} high={99} optimum={0} value={Math.min(pct, 100)} aria-label={`${pct}% used`} />
      {detailed ? <><dl className="budget-amounts">
        <div><dt>{budget.scope==='run'?'Run limit':'Monthly limit'}</dt><dd>{money(budget.limitMicros,currency)}</dd></div>
        <div><dt>Spent</dt><dd>{money(budget.chargedMicros,currency)}</dd></div>
        <div><dt>Remaining</dt><dd>{money(budget.remainingMicros,currency)}</dd></div>
      </dl>
      {budget.reservedMicros>0 && <p className="small">{money(budget.reservedMicros,currency)} reserved for calls in progress; included in the remaining balance.</p>}
      {budget.estimatedMicros>0 && <p className="small">{money(budget.estimatedMicros,currency)} estimated.</p>}
      {budget.raisedBy && <p className="small">Raised by {budget.raisedBy}.</p>}</> : <p className="small">
        {money(budget.chargedMicros,currency)} spent
        {budget.reservedMicros>0 && ` · ${money(budget.reservedMicros,currency)} reserved`} of {money(budget.limitMicros,currency)} · {money(budget.remainingMicros,currency)} left
        {budget.estimatedMicros>0 && ` · ${money(budget.estimatedMicros,currency)} estimated`}
        {budget.raisedBy && ` · raised by ${budget.raisedBy}`}
      </p>}
      {raiseStillReached && <p className="small" role="status">The raised limit is still at or below current spend and reservations.</p>}
      {pending.length > 0 && <p className="small" role="status">Resume pending for {pending.join(', ')}. Weft will retry automatically.</p>}
      {canRaise && showPause && (budget.reached || (budget.paused && pending.length === 0)) && (
        <Btn small tone="primary" disabled={busy} onClick={() => void raise()}>
          Raise budget
        </Btn>
      )}
      <ErrorBox error={error} />
      {onEdit && <Btn small onClick={event=>onEdit(event.currentTarget)} disabled={editDisabled} aria-expanded={!!editor}>Edit budget</Btn>}
      {editor}
    </li>
  );
}

/** Monthly definitions and current ledgers stay together; saving one never edits task defaults. */
function MonthlyBudgets({budgets,ledgersUnavailable,period,canEdit,currency,onSaved}: {
  budgets?:BudgetView[];ledgersUnavailable:boolean;period?:string;canEdit:boolean;currency:string;onSaved:()=>Promise<void>;
}) {
  const defs=useAsync(api.budgetDefinitions,[]);
  const [editing,setEditing]=useState<string>();
  const [notice,setNotice]=useState<string>();
  const returnFocus=useRef<HTMLButtonElement|null>(null);
  const heading=useRef<HTMLHeadingElement>(null);
  const keyOf=(budget:{scope:string;id:string})=>`${budget.scope}/${budget.id.replace(/@\d{4}-\d{2}$/,'')}`;
  const entries=(budgets??[]).map(budget=>({budget,definition:defs.data?.budgets.find(d=>keyOf(d)===keyOf(budget))}));
  const unused=defs.data?.budgets.filter(d=>!entries.some(e=>keyOf(e.budget)===keyOf(d)))??[];
  const disabled=!canEdit || !defs.data || defs.loading || !!defs.error || ledgersUnavailable;
  useEffect(()=>{
    if(!editing && !disabled && returnFocus.current) {
      (returnFocus.current.isConnected?returnFocus.current:heading.current)?.focus();
      returnFocus.current=null;
    }
  },[editing,disabled]);
  const open=(key:string,button:HTMLButtonElement)=>{returnFocus.current=button;setNotice(undefined);setEditing(key);};
  const close=()=>setEditing(undefined);
  const editor=(definition?:BudgetDefinition,budget?:BudgetView)=>
    <MonthlyBudgetEditor key={definition?keyOf(definition):'new'} definition={definition} budget={budget} currency={currency}
      definitions={defs.data!.budgets} disabled={disabled} onCancel={close} onSaved={async(result,message)=>{
        defs.setData(result);setNotice(message);close();await onSaved();
      }}/>;
  const currentPeriod=defs.data?.period??period;
  return <section className="monthly-budgets" aria-labelledby="monthly-budgets-heading">
    <div className="section-head"><div><h3 id="monthly-budgets-heading" ref={heading} tabIndex={-1}>Current month budgets</h3>
      {currentPeriod && <p className="muted">{budgetPeriodLabel(currentPeriod)} · UTC</p>}
      <p className="muted small">Monthly limits apply to this month and future months. Recorded spending is preserved.</p>
    </div>{canEdit && <Btn disabled={disabled || !!editing} onClick={event=>open('new',event.currentTarget)}>Add monthly budget</Btn>}</div>
    {defs.loading && !defs.data && <p role="status">Loading monthly budget settings…</p>}
    <ErrorBox error={defs.error?`Could not load monthly budget settings: ${defs.error}`:undefined}/>
    {defs.error && <Btn onClick={()=>void defs.reload()}>Retry monthly budgets</Btn>}
    {notice && <p role="status">{notice}</p>}
    {budgets && entries.length===0 && unused.length===0 && <p className="empty">No budget has been used this month.</p>}
    <ul className="integration-grid monthly-budget-list">
      {entries.map(({budget,definition})=><BudgetBar key={budget.scope+budget.id} budget={budget} currency={budget.currency||currency} canRaise={canEdit}
        detailed onRaised={onSaved} onEdit={canEdit && definition ? button=>open(keyOf(definition),button):undefined}
        editDisabled={disabled || !!editing} editor={definition && editing===keyOf(definition)?editor(definition,budget):undefined}/>)}
      {unused.map(definition=><li className={`budget-bar ${editing===keyOf(definition)?'is-editing':''}`} key={keyOf(definition)} aria-label={`Configured budget ${budgetLabel(definition)}`}>
        <strong>{budgetLabel(definition)}</strong><p className="small">Monthly limit: {money(definition.limitMicros,currency)}</p>
        <p className="muted small">{ledgersUnavailable?'Current balance unavailable. Refresh budget balances before editing.':'No spending recorded for this budget this month.'}</p>
        {canEdit && <Btn small disabled={disabled || !!editing} aria-expanded={editing===keyOf(definition)} onClick={event=>open(keyOf(definition),event.currentTarget)}>Edit budget</Btn>}
        {editing===keyOf(definition) && editor(definition)}
      </li>)}
    </ul>
    {editing==='new' && editor()}
  </section>;
}

function MonthlyBudgetEditor({definition,budget,definitions,currency,disabled,onCancel,onSaved}: {
  definition?:BudgetDefinition;budget?:BudgetView;definitions:BudgetDefinition[];currency:string;disabled:boolean;
  onCancel:()=>void;onSaved:(result:{budgets:BudgetDefinition[];period:string},message:string)=>Promise<void>;
}) {
  const [scope,setScope]=useState<BudgetDefinition['scope']>(definition?.scope??'installation');
  const [id,setId]=useState(definition?.id??'*');
  const [limit,setLimit]=useState(fromMicros(definition?.limitMicros));
  const [interrupt,setInterrupt]=useState(definition?.interruptPercent?String(definition.interruptPercent):'');
  const [removing,setRemoving]=useState(false);
  const [error,setError]=useState<string>();
  const [busy,setBusy]=useState(false);
  const input=useRef<HTMLInputElement>(null);
  useEffect(()=>{input.current?.focus();},[]);
  const micros=toMicros(limit);
  const nextId=scope==='installation'?'*':id.trim();
  const validation=useFieldValidation({
    limit: !removing && (!micros || micros>1_000_000_000_000_000) ? 'Enter a monthly limit greater than zero, up to 1,000,000,000.' : undefined,
    id: !removing && !nextId ? 'Enter the ID this budget applies to.' : !removing && definitions.some(d=>d!==definition && d.scope===scope && d.id===nextId) ? 'A monthly budget already exists for this scope and ID. Edit that budget instead.' : undefined,
    interrupt: !removing && interrupt!=='' && (!/^\d+$/.test(interrupt) || Number(interrupt)<101 || Number(interrupt)>1000) ? 'Interrupt level must be between 101 and 1000, or empty for none.' : undefined,
  });
  const title=definition?`Edit ${budgetLabel(definition)} budget`:'Add monthly budget';
  const save=async()=>{
    setError(undefined);
    if(!validation.validate()) return;
    const next:BudgetDefinition={...definition,scope,id:nextId,limitMicros:micros!,interruptPercent:interrupt?Number(interrupt):undefined};
    const updated=definition?definitions.flatMap(d=>d===definition?(removing?[]:[next]):[d]):[...definitions,next];
    setBusy(true);
    try {
      const result=await api.saveBudgetDefinitions(updated);
      const message=removing?'Monthly budget removed. Recorded spending is preserved.':definition
        ? `Monthly limit updated from ${money(definition.limitMicros,currency)} to ${money(micros!,currency)}.`
        : `Monthly budget added with a limit of ${money(micros!,currency)}.`;
      await onSaved(result,message);
    } catch(e) {setError(errorMessage(e));} finally {setBusy(false);}
  };
  return <form ref={validation.ref} className="budget-inline-editor card form" aria-label={title} onSubmit={event=>{event.preventDefault();void save();}}>
    <h4>{title}</h4>
    <p className="small">Changes apply to the current month and future months. Recorded spending is preserved.</p>
    {budget?.raisedBy && <p role="note">This month includes a manual raise. Its effective limit cannot fall below that raised amount; the recurring limit below may differ from the card.</p>}
    {budget && toMicros(limit)!==undefined && toMicros(limit)! < budget.chargedMicros+budget.reservedMicros && <p role="note">This limit is below current spending and reservations. New calls may be blocked and affected runs may pause.</p>}
    <fieldset disabled={disabled || busy}>
      {!removing && <div className="budget-editor-fields">
        <HelpField error={scope==='installation'?validation.errors.id:undefined} label="Scope" help="Installation covers all usage. User, project and workflow budgets limit the specified ID; the tightest applicable budget wins.">
          <select value={scope} onChange={event=>{setScope(event.target.value as BudgetDefinition['scope']);setId(event.target.value==='installation'?'*':'');}}>
            <option value="installation">Installation</option><option value="user">User</option><option value="project">Project</option><option value="workflow">Workflow</option>
          </select>
        </HelpField>
        {scope!=='installation' && <HelpField error={validation.errors.id} label="Applies to" help="Enter the user, project or workflow ID covered by this budget."><input value={id} onChange={event=>setId(event.target.value)}/></HelpField>}
        <HelpField error={validation.errors.limit} label={`Monthly limit (${currency})`} help="The recurring spending ceiling for a UTC calendar month. It also updates this month, subject to any manually raised floor."><input ref={input} value={limit} onChange={event=>setLimit(event.target.value)} inputMode="decimal"/></HelpField>
        <HelpField error={validation.errors.interrupt} label="Interrupt level (%)" help="Optional: 101–1000 percent of the monthly limit. Stops a running call that crosses the threshold and pauses affected work. Empty disables this threshold."><input value={interrupt} onChange={event=>setInterrupt(event.target.value)} placeholder="None" inputMode="numeric"/></HelpField>
      </div>}
      {removing && <p role="note">Remove this recurring limit? Other applicable budgets still apply. Recorded spending is retained.</p>}
      <div className="btn-row budget-editor-actions">
        <Btn type="submit" tone={removing?'danger':'primary'}>{busy?'Saving…':removing?'Confirm removal':'Save monthly budget'}</Btn>
        <Btn onClick={onCancel}>Cancel</Btn>
        {definition && !removing && <Btn tone="danger" onClick={()=>setRemoving(true)}>Remove budget</Btn>}
      </div>
    </fieldset>
    <ErrorBox error={error}/>
  </form>;
}

/** Only policy defaults are saved here; current monthly definitions are independent. */
function DefaultTaskBudget({canEdit,currency}: {canEdit:boolean;currency:string}) {
  const policy=useAsync(api.costPolicy,[]);
  const [runBudget,setRunBudget]=useState('');
  const [warning,setWarning]=useState('80');
  const [error,setError]=useState<string>();
  const [saved,setSaved]=useState<string>();
  const [busy,setBusy]=useState(false);
  useEffect(()=>{if(policy.data){setRunBudget(policy.data.taskRunBudgetMicros?fromMicros(policy.data.taskRunBudgetMicros):'');setWarning(String(policy.data.warningPercent));}},[policy.data]);
  const run=runBudget.trim()===''?0:toMicros(runBudget);
  const validation=useFieldValidation({
    run: run===undefined || run>1_000_000_000_000_000 ? 'Enter the task run budget as an amount up to 1,000,000,000, or leave it empty for none.' : undefined,
    warning: !/^\d+$/.test(warning) || Number(warning)<1 || Number(warning)>100 ? 'Warning level must be between 1 and 100.' : undefined,
  });
  const save=async()=>{
    setError(undefined);setSaved(undefined);
    if(!validation.validate()) return;
    setBusy(true);
    try {
      const result=await api.saveCostPolicy({taskRunBudgetMicros:run!,warningPercent:Number(warning)});
      policy.setData(result);setSaved('Defaults saved. New task runs use this budget; existing run limits are unchanged.');
    } catch(e) {setError(errorMessage(e));} finally {setBusy(false);}
  };
  return <form ref={validation.ref} className="card form default-task-budget" aria-label="Default budget per task" onSubmit={event=>{event.preventDefault();void save();}}>
    <h3>Default budget per task</h3><p className="muted small">Suggested ceiling for new task runs. Changing it does not change existing run limits or monthly budgets.</p>
    {policy.loading && <p role="status">Loading task defaults…</p>}
    <ErrorBox error={policy.error}/>
    {policy.error && <Btn onClick={()=>void policy.reload()}>Retry task defaults</Btn>}
    <fieldset disabled={!canEdit || busy || policy.loading || !!policy.error || !policy.data}>
      <div className="budget-editor-fields">
        <HelpField error={validation.errors.run} label={`Task run budget (${currency})`} help="Default ceiling for each new task run. Leave empty to use only applicable monthly limits. Existing runs keep their own limits."><input value={runBudget} onChange={event=>setRunBudget(event.target.value)} inputMode="decimal" placeholder="No per-task limit"/></HelpField>
        <HelpField error={validation.errors.warning} label="Default warning level (%)" help="Default warning threshold for budgets without their own override. Monthly budgets may also inherit this warning level; it does not change any spending ceiling."><input value={warning} onChange={event=>setWarning(event.target.value)} inputMode="numeric"/></HelpField>
      </div><p className="muted small">Monthly budgets also inherit this warning level unless they define their own.</p>
      <div><Btn type="submit" tone="primary">{busy?'Saving…':'Save task defaults'}</Btn></div>
    </fieldset>
    {saved && <p role="status">{saved}</p>}<ErrorBox error={error}/>
  </form>;
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
  const [busy,setBusy]=useState(false);
  useEffect(() => {
    if (catalogue.data) {
      setCurrency(catalogue.data.currency);
      setUnpriced(catalogue.data.unpriced);
      setRows(catalogue.data.prices.map(rowFrom));
    }
  }, [catalogue.data]);
  const set = (i: number, patch: Partial<PriceRow>) => {
    setSaved(undefined);
    setRows((cur) => cur.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  };
  const validation=useFieldValidation(Object.fromEntries(rows.flatMap((row,index)=>(['input','output','cacheRead','cacheWrite'] as const).map(key=>[`${index}.${key}`, (key==='cacheRead'||key==='cacheWrite')&&!row[key] ? undefined : toMicros(row[key])===undefined ? 'Enter a valid nonnegative price per million tokens.' : undefined]))));
  const save = async () => {
    setError(undefined);
    setSaved(undefined);
    if(!validation.validate()) return;
    const prices: Price[] = [];
    for (const r of rows) {
      const input = toMicros(r.input), output = toMicros(r.output);
      const read = r.cacheRead ? toMicros(r.cacheRead) : undefined, write = r.cacheWrite ? toMicros(r.cacheWrite) : undefined;
      if (input === undefined || output === undefined || (r.cacheRead && read === undefined) || (r.cacheWrite && write === undefined)) {
        return setError(`Enter prices per million tokens as amounts for ${r.name || 'every row'}.`);
      }
      prices.push({ [r.target]: r.name.trim(), model: r.model.trim() || '*', inputMicrosPerMTok: input, outputMicrosPerMTok: output, cacheReadMicrosPerMTok: read, cacheWriteMicrosPerMTok: write } as Price);
    }
    setBusy(true);
    try {
      const next = await api.savePrices({ currency, unpriced, prices });
      await catalogue.reload();
      await onSaved();
      setSaved(`Prices saved as version ${next.version}.`);
    } catch (e) {
      setError(errorMessage(e));
    } finally {setBusy(false);}
  };
  return (
    <form ref={validation.ref} className="card form" aria-label="Model prices" onSubmit={(e) => { e.preventDefault(); void save(); }}>
      <h3>Model prices</h3>
      {catalogue.data && (
        <p className="muted small">
          {catalogue.data.version ? `Version ${catalogue.data.version}${catalogue.data.updatedBy ? ` by ${catalogue.data.updatedBy}` : ''}.` : 'No prices saved yet.'} Prices are per million
          tokens; the most specific row (provider, then plugin; exact model, then *) wins.
        </p>
      )}
      <ErrorBox error={catalogue.error} />
      {catalogue.error && <Btn onClick={()=>void catalogue.reload()}>Retry model prices</Btn>}
      {catalogue.loading && <p role="status">Loading model prices…</p>}
      <fieldset disabled={!canEdit || busy || catalogue.loading || !!catalogue.error}>
        <HelpField label={"Currency"} help={"The three-letter currency code for prices, such as USD. Prices and budgets use the configured currency without an exchange-rate conversion."}>
          <input value={currency} onChange={(e) => setCurrency(e.target.value.toUpperCase())} maxLength={3} />
        </HelpField>
        <HelpField label={"Unpriced models"} help={"Allow calls with missing prices and report them as unpriced, or refuse those calls while a budget applies. Add prices for models used in budgeted work."}>
          <select value={unpriced} onChange={(e) => setUnpriced(e.target.value as PriceCatalogue['unpriced'])}>
            <option value="allow">Allow and show as unpriced</option>
            <option value="refuse">Refuse while a budget applies</option>
          </select>
        </HelpField>
        {rows.map((r, i) => (
          <div className="model-price-row" key={i} role="group" aria-label={`Price ${i + 1}`}>
            <HelpField label={"Applies to"} help={"Choose a named provider or a provider plugin. A matching provider price takes precedence over a plugin price."}>
              <select value={r.target} onChange={(e) => set(i, { target: e.target.value as PriceRow['target'] })}>
                <option value="provider">Provider</option>
                <option value="type">Plugin</option>
              </select>
            </HelpField>
            <label>
              {r.target === 'provider' ? 'Provider name' : 'Plugin id'}
              <input value={r.name} onChange={(e) => set(i, { name: e.target.value })} />
            </label>
            <HelpField label={"Model"} help={"Enter an exact model ID or * as a fallback. An exact model price wins within the same provider scope."}>
              <input value={r.model} onChange={(e) => set(i, { model: e.target.value })} placeholder="*" />
            </HelpField>
            <HelpField error={validation.errors[`${i}.input`]} label={`Input (${currency}/M tokens)`} help="Price per million ordinary input tokens. Use the provider rate for this model and currency.">
              <input value={r.input} onChange={(e) => set(i, { input: e.target.value })} inputMode="decimal" />
            </HelpField>
            <HelpField error={validation.errors[`${i}.output`]} label={`Output (${currency}/M tokens)`} help="Price per million tokens generated by the model. This can differ from the input price.">
              <input value={r.output} onChange={(e) => set(i, { output: e.target.value })} inputMode="decimal" />
            </HelpField>
            <HelpField error={validation.errors[`${i}.cacheRead`]} label={"Cache read"} help={"The price per million tokens read from the provider\u2019s token cache. Leave it empty to use the input rate."}>
              <input value={r.cacheRead} onChange={(e) => set(i, { cacheRead: e.target.value })} placeholder="Input rate" inputMode="decimal" />
            </HelpField>
            <HelpField error={validation.errors[`${i}.cacheWrite`]} label={"Cache write"} help={"The price per million tokens written to the provider\u2019s token cache. Leave it empty to use the input rate."}>
              <input value={r.cacheWrite} onChange={(e) => set(i, { cacheWrite: e.target.value })} placeholder="Input rate" inputMode="decimal" />
            </HelpField>
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
            {busy?'Saving…':'Save prices'}
          </Btn>
        </div>
      </fieldset>
      {saved && <p role="status">{saved}</p>}
      <ErrorBox error={error} />
    </form>
  );
}
