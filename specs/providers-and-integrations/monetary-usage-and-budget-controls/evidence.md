# INT-004 historical evidence

These results were recorded at the time of the run. The documentation migration did not rerun them.

- 2026-11-11. The optional native Ollama journey recorded plugin setup, provider selection, a tool-bearing task and local cost. Production pricing and external billing accuracy remain unqualified.


## 2026-11-19 — Current monthly limit visibility (AC-003)

- A real browser task exposed that healthy monthly ledger views retained their
  stored limit after an administrator saved a new definition, until another
  reservation. Current individual/list views now use the same definition and
  Raise-floor precedence as admission. The change is read-only: spend entries,
  persisted ledger revisions, reservation/settlement logic and audit contracts
  remain unchanged. Closed periods keep historical limits. Definition read or
  decoding failures propagate instead of presenting a stale successful balance.
- `./scripts/go.sh test ./internal/cost ./internal/api ./internal/engine` passed.
  Unit cases cover current/closed/run views, explicit and legacy Raise floors,
  definition corruption and storage failure. The SQLite API integration case
  reserves and settles real spend, changes the definition up/down, reads both
  endpoints and asserts unchanged persisted ledger bytes/revision.
  Log: `/tmp/weft-budget-go.log`.
- The browser integration creates a task using the deterministic in-process
  model, changes its monthly definition via the inline form and confirms that
  the displayed/new persisted limit changes while charged spend stays constant.
  Task defaults are not written. Failure/retry, cancellation, roles and narrow
  layout are covered in `/tmp/weft-budget-browser-final.log` (nine scenarios).
  Component/helper coverage passed with the 524-test web suite, followed by a
  final 19-test focused pass. `make lint` passed.
- The standard local image build succeeded and the service is healthy. Browser
  inspection opened/cancelled the real editor without changing live budgets;
  no paid model was used. This is local functional evidence, not production
  financial qualification. Wider Go/Python suites were not rerun.
