# RUN-001 historical evidence

These results were recorded at the time of the run. The documentation migration did not rerun them.

- 2026-10-18. A real engine/SQLite/instruction manager/filesystem gateway test used two graphs on the same worker. Scoped markers and escape denials were checked without exercising arbitrary host processes or Docker/external-runtime acceptance.
- 2026-10-18. Concurrent scope acceptance passed five race-detector repetitions and the affected instruction, tool and engine suites.


## Console refinement — 2026-11-19

The approved catalogue/editor/association/inspection redesign preserves the
RUN-001 instruction contract. Docker Chromium exercised a real deterministic
execution, updated its managed skill afterward, and inspected the unchanged
historic revision and source hashes. The progressive repository-guidance journey
also passed. Existing viewer restrictions and administrator-only CAS mutations
remain covered by component and real API assertions. The bounded Markdown
preview renders HTML/images as inert text and performs no content fetches.

Validation: `make test-web` (TypeScript, 534 tests), nine instruction/navigation
browser scenarios and two light/dark visual comparisons. Logs:
`/tmp/weft-skills-web-final.log`, `/tmp/weft-skills-browser-final.log`,
`/tmp/weft-skills-visual-final.log`. Backend instruction code and Go/Python suites
were not changed or rerun in this refinement. See the console evidence for scope
and browser/draft/preview limitations.


## Editable built-in copies — 2026-11-19

- The approved refinement adds Built-in / Read-only catalogue labels and a
  prominent explanation in the detail view. Administrators can open Create
  editable copy; it prepares an independent draft with a non-reserved available
  ID. Existing IDs, including tombstones, are avoided. Save creates a new custom
  revision through the existing POST endpoint. The original remains immutable;
  history, workflow references and installation-default state are not copied.
- `make test-web` passed TypeScript and 537 tests in 99 files. Focused route,
  helper and component coverage includes ID collisions, missing sources,
  administrator/viewer behavior and retention after a create conflict.
  Logs: `/tmp/weft-skill-copy-web-final.log` and
  `/tmp/weft-skill-copy-focused.log`.
- Ten Chromium instruction/navigation scenarios passed against the real
  disposable backend. The new journey verifies that opening a draft writes
  nothing, Save persists revision 1, the built-in is byte-for-byte unchanged,
  no workflow uses the new skill, the next draft gets a different ID and
  cancelling writes nothing. Log: `/tmp/weft-skill-copy-browser.log`.
- Standard Compose rebuilt and started weftd; `/healthz` returned `ok`. Live
  browser inspection confirmed the read-only notice/action and the editable
  prefilled draft. No live save was submitted. Screenshots:
  `/tmp/weft-skill-copy-built-in.png`, `/tmp/weft-skill-copy-draft.png`.
  Build log: `/tmp/weft-skill-copy-compose.log`.
- Backend contracts/security controls were unchanged; Go/Python suites and Go
  lint were not rerun. Browser verification is limited to Chromium and does not
  establish assistive-technology conformance.

- Light/dark catalogue snapshots were regenerated, inspected and passed a fresh
  comparison at the existing tolerance. Log:
  `/tmp/weft-skill-copy-visual-final.log`.
