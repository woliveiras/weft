# Agent catalog verification

- **AC-001.** The Go API integration exercises catalog publication with real
  storage. Viewer and operator writes are rejected. Publishing without
  `If-Match` is rejected, and stale writers receive a conflict. Unit tests cover
  backfill, stable snapshots and exclusion of task copies. The API and browser
  journeys create an agent as part of a workflow, find it in the catalog, and
  reuse it when creating a second workflow. No registration action is needed.
- **AC-002.** The resolver tests cover missing required values, undeclared
  parameters, oversized values and storage failures. A real graph run gives
  two nodes distinct parameters and workflow instructions while both use
  version 1. The graph receives no extra grants from workflow defaults. An internal task snapshot retains its
  source context and pin.
- **AC-003.** The Chromium journey opens a card, uses the Workflows tab to
  reach the instance configuration, and saves instructions in that workflow.
  The source workflow and its channel are preserved. Publishing version 2
  in the catalog leaves the target on version 1.
- **Pilot.** A read-only export of the active `code-review` workflow
  (`pr-review`, version 3) was resolved against `pr-reviewer` version 1 in
  a disposable catalog. After substituting the workflow's `owner`,
  `repository` and `baseBranch` parameters, the entire effective graph spec
  matched the source apart from the new binding. This preserves the reviewer's
  prompt and grants, the inline publisher, and the `requests` and `decisions`
  channels. The local Compose installation then published `pr-reviewer`
  version 1 and saved `pr-review` version 4 after server validation returned
  no warnings. The graph reported both replicas idle and the console listed
  the version 1 binding under **Used in workflows**.

- **Unified catalog rollout.** The local Compose installation reported 12 of
  12 saved workflow agents in the catalog. The live console rendered 12 cards.
  The Workflows tab linked to the code-review Reviewer configuration, where
  Workflow instructions was editable. The code-review graph reported version 4
  running with both replicas idle.
