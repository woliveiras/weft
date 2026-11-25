# Models, prices and budgets

Open **Configuration → Models**. The tabs are **Providers** (`#/models`),
**Inference plugins** (`#/models/plugins`), **Usage & costs** (`#/models/usage`),
**Budgets** (`#/models/budgets`) and **Model prices** (`#/models/prices`).
They are separate resources, not one form saved together.

## Enable and configure a provider

1. In Inference plugins, enable the supported server/API kind the installation
   may use. Enabling a plugin is not yet a configured provider.
2. Select **Add provider**. Creation/editing use full pages at
   `#/models/providers/new` and `#/models/providers/<name>/edit`, not the previous
   modal. Supply its stable name, enabled type, base URL, default model and
   provider-specific fields such as Vertex project/region.
3. Enter credentials if required. Existing secrets are not displayed for editing;
   use the supported keep/replace behavior. Review limits such as timeout,
   retries, concurrency, context window and model guidance.
4. **Test settings** explicitly tests the draft. Save to persist configuration.
   Draft testing/configuration requires admin; saved-provider tests are available
   to operators. Tests can invoke the provider and incur usage; they do not run
   an agent task.
5. Search/filter the Providers list. Test one or all providers and inspect busy,
   success or failure feedback. **Not tested** does not establish health.
6. Configure installation default/provider choices and choose provider/model
   overrides in agent/workflow configuration as needed. A default model serves
   agents that do not choose one; discovery lists assist selection without
   proving every listed model supports the intended workload.

Save/test failure leaves a correction path. Check plugin enablement, required
fields, credentials, endpoint reachability and provider errors; do not silently
switch models after a failed call. Configuring a model grants no external tools.

## Configure prices and inspect usage

1. In Model prices, review the current configuration revision and currency.
2. Add provider/plugin and model-specific or wildcard price rows. Rates are per
   million tokens, with input/output and optional cache rates. Specificity
   determines the selected row; Weft does not ship vendor prices.
3. Choose the unpriced-model policy, review and save explicitly. Missing prices
   can block model admission when budget policy requires priced usage.
4. In Usage & costs, inspect recorded spend and breakdowns by provider/model,
   user, project, workflow and task run. Counts and calculated cost follow
   reported usage and configured rates, not a vendor invoice.

## Change a budget

1. Open Budgets and distinguish **current-period usage** from the recurring
   policy and the default limit for new task runs.
2. Inspect amount spent, remaining amount, thresholds and applicable scope.
   Monthly periods use UTC calendar months.
3. Edit the recurring budget settings or use the current-budget adjustment
   offered for the applicable ledger. Review the effect before saving. Raising
   the current period and changing a recurring definition are different actions.
4. Save explicitly as administrator. Existing spend remains recorded; changing
   a limit does not reset the ledger. Existing task limits are not silently
   replaced just because the new-task default changes.
5. If admission/work paused at a limit, inspect the tightest applicable budget
   and raise the appropriate limit when authorized. Inspect the returned resume
   outcome: pending resumes can be retried automatically by Weft. A saved budget
   change is not proof that work restarted; follow the affected run's state.

The task page previews applicable limits before starting. Provider tests and
interpretation may also use model calls; a task not having started is not proof
of zero installation spend. Exact budget/estimate semantics belong to the
[cost-control contract](../../cost-control.md).

## Implementation and verification map

- [ModelsPage](../../../web/src/components/ModelsPage.tsx),
  [ProvidersSection](../../../web/src/components/ProvidersSection.tsx),
  [CostsSection](../../../web/src/components/CostsSection.tsx) and
  [model navigation](../../../web/src/components/modelsNavigation.ts).
- [Provider tests](../../../web/src/components/ProvidersSection.test.tsx),
  [cost UI tests](../../../web/src/components/CostsSection.test.tsx),
  [API routes](../../../server/internal/api/server.go).
- [Configuration guide](../../configuration.md), [cost control](../../cost-control.md).
