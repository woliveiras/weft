# Evidence

- 2026-11-23: read-only GitHub CLI observation of `woliveiras/weft` found five
  open Dependabot alerts (2, 3, 5, 6, 8), all for `undici` in
  `web/pnpm-lock.yaml`. This is discovery evidence, not proof of remediation.
- Implemented `scm.security.alerts` as an optional read-only SCM adapter
  capability, with exact connection grants, grouping, stable branch identity,
  open-PR matching, bounded HTTP pages, explicit incomplete-read errors and
  decoded-string credential redaction. Both pre- and post-redaction output are
  limited to the gateway's default 32 KiB model result ceiling.
- Fail-first: the new grouping unit tests failed to compile before the new
  types/helpers/tool existed. The specification/matrix and initial tool tests
  were reviewed before implementation was used as an oracle. The reviewer
  requested additional PR-pagination failures, Unicode-escaped reflection and
  GitHub invalid-base cases; these were added. The API/runtime collaboration
  tests were authored after implementation and remain implementation-aware
  tests of specification-derived observable invariants.
- Unit coverage: `TestSecurityGroupingIdentityAndOrder`,
  `TestSecurityGroupingRejectsIncompleteData`.
- Real encrypted-store/gateway/HTTP fixture coverage:
  `TestSecurityAlertsGatewayHTTP`,
  `TestSecurityAlertsUnsupportedAndInvalidArguments`,
  `TestSecurityAlertsFinalRedactedOutputBound`. No external writes are made.
- API/catalog/engine/task-consent/gateway/HTTP integration:
  `TestDependencySecurityExampleRequiresManualTaskDispatch` covers saving and
  drafting without execution, explicit start, an empty inventory and a denied
  provider. Only the deterministic model and sandbox/build/checkout outside the
  read-only discovery seam are substituted. This does not prove live model
  compliance for arbitrary fixes.
- This integration exposed an existing `task.authorization_decision` event
  absent from the event schema. The schema and event catalog now recognize the
  emitted event with a strict argument-free payload. A schema unit test rejects
  invalid payloads; the task integration validates emitted stored events. The
  first full Go run found the missing event-catalog entry; it was corrected.
- Final automated results: `./scripts/go.sh test ./...` passed;
  `make lint` (Docker gofmt check and Go vet) passed; `make build` passed.
  Focused SCM/API/schema tests also passed. No web source changed; the standard
  Docker build reused the existing successful web typecheck/Vite build layers.
- Independent subagent reviews covered specification, test oracles and the final
  implementation. Two findings were corrected: Debian's virtual `git-daemon`
  package was removed, and the final output bound is rechecked after redaction.
  The reviewer confirmed the fixes and reported no remaining blocking findings.
- The example environment Dockerfile was built on the local Docker daemon:
  Go 1.26.11, Node 22.23.4, pnpm 10.33.4 and `git-http-backend` were confirmed.
  This validates the image contents, not a task admission on the dedicated daemon.
- The standard `docker compose up -d --build --no-deps weftd` succeeded; the
  service is healthy and the live tool catalog exposes `scm.security.alerts`.
- Final dry-run against the local server exited 0: update the network ceiling
  with `registry.npmjs.org`, create `Project/weft-security` at
  `refs/heads/main`, and create `Graph/dependency-security`. No issue trigger,
  schedule, credential, budget or review-route change is included. Applying this
  plan remains pending the operator's explicit approval under the rehearsal rule.
- Live SCM credential permission for Dependabot reading, the dedicated task
  environment admission, live model remediation and actual PR delivery remain
  unqualified. No remediation task, remote branch, PR or alert mutation was
  dispatched by this installation work.
