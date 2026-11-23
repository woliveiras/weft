# INT-006 historical evidence

These results were recorded at the time of the run. The documentation migration did not rerun them.

- 2026-10-18. Recorded unit/TLS/browser coverage exercises overlapping OAuth transactions, isolated renewal/revocation and pinned template upgrades. The hosted-CIMD browser check was separate from live vendor qualification.


## Explicit DCR recovery — 2026-11-23

Maintainer approved a deliberate fresh DCR attempt in the disposable personal
Atlassian rehearsal, accepting possible orphaned/duplicate provider clients.
INT-006 AC-003 now distinguishes automatic duplicate prevention from explicit
administrator recovery. AC-004 specifies safe per-field failure diagnosis.

- Specification, test and implementation reviews by an independent subagent found
  no blocking issue. The review's body-limit hardening observation was addressed
  with MaxBytesReader and an oversized trailing-body API case.
- Unit tests cover acknowledgement, revisions, allowed states and history bounds.
  Integration tests exercise real TLS registration, API authorization, fsstore,
  restart/configuration history, write failures and concurrent managers/starts.
- Diagnostic fixtures include incompatible credential values and verify that only
  allowlisted names/categories and HTTP status persist or reach public views.
- Console tests use the real API client boundary and cover confirmation, loading,
  failure, connection/revision changes, historical diagnostics and no auto-start.
- Passed: Docker `./scripts/go.sh test ./...`; `make lint`; `make build`;
  web TypeScript, complete Vitest suite and production build in node:22-bookworm;
  Python SDK (16), agents (14) and script (10) tests; `git diff --check`.
  Final localized changes additionally reran the affected API/manager tests.
- Browser fixture E2E assertions were updated for explicit recovery. Only their
  TypeScript check ran: the existing registration browser file still uses old
  inline Connections routes. Its full fixture browser qualification remains open;
  API/TLS integration and the local real-account exercise are separate evidence.
- Live provider outcome is recorded in INT-009 and the rehearsal notes. These
  automated passes do not establish Atlassian interoperability or a delivered PR.
