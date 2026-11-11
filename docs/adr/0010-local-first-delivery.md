# 0010 — Local-first delivery and external qualification

**Status:** Accepted

## Context

Weft needs repeatable regression checks and a local delivery path before
production integrations can be selected, funded and authorized. Local model
performance varies with hardware and workload.

## Decision

Deliver task-oriented UX first, then the standalone profile, then the
issue-to-PR flow. Keep CI deterministic with fixtures. Exercise the shared
model and tool contracts through opt-in local Ollama and fixture SCM/MCP
boundaries. Qualify real providers, tracker write-back and live vendor
authorization in a separately authorized phase.

## Consequences

- Each historical run records its actual model, service and isolation
  boundaries. Fixture success qualifies the exercised boundary.
- Final clean-install and developer acceptance follow the remaining Release 1
  work. Downloads, external accounts and writes require separate authorization.
- Production provider terms and the coding evaluation threshold are tracked in
  [PROD-03](../../specs/production-readiness/prod-03/spec.md) and
  [PROD-13](../../specs/production-readiness/prod-13/spec.md).

## Alternatives

- **Require hosted services for all checks:** increases setup, cost and
  variability in the default regression suite.
- **Use fixtures exclusively:** leaves real transport and operational
  boundaries unqualified.
