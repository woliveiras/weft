# TASK-006 historical evidence

These results were recorded at the time of the run. The documentation migration did not rerun them.

- 2026-10-19. AUTH-02 passed focused Go suites, 21 Vitest cases and one Chromium journey for all modes. AUTH-03 passed focused Go suites, 15 Vitest cases and a Chromium live-reduction journey. The historical record notes a parallel cleanup timeout that passed on focused rerun.

## Go API role verification

- 2026-11-11. The consent, task approval and environment proposal role
  tests passed five repetitions with a real HTTP handler and disposable
  SQLite store. They covered anonymous, Viewer, Operator and Admin access
  to consent, cancellation, task progress, the approval inbox and decision
  routes. Denied requests left consent, approvals and task state intact.
  The task and inbox showed the same pending and final decision identities.

## Legacy execution compatibility

- 2026-11-11. `TestRunningLegacyTaskWithoutConsent` passed 20 repetitions.
  A disposable filesystem store held a running execution copy without a
  consent record. The gateway allowed the ungated grant, waited for approval
  on the gated grant, and denied a gated call in a non-interactive context
  and an ungranted call. The fixture provided the human approval response.
  Saving Ask, Workspace or Full returned a conflict and left consent absent.
