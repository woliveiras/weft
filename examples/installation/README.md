# Complete installation example

This eleven-document example describes the operational rehearsal using Gemini,
GitHub MCP and SCM, a URL-imported project and the native `weft-deps` workflow.
It deliberately contains no credentials or installation-specific revisions.

1. Provision the three dedicated server references described in
   [the configuration guide](../../docs/declarative-configuration.md#compose).
   Missing references are reported without revealing values.
2. Run `weft apply -f examples/installation/ --dry-run` and review every diff.
   On a populated installation, use a fresh export as your baseline or explicitly
   adopt individual conflicts with `--force Kind/name` after review.
3. Run `weft apply -f examples/installation/` and confirm the plan. This includes
   downloading the public project. Add `--record-consent` only if you approve the
   displayed trigger scope and its explicit `full` mode. Without it the trigger
   is paused with `consent required`. `--yes` alone never records consent.
4. Build the graph environment using the existing console action before running
   tasks. Apply saves the manual Dockerfile but never builds it.
5. Keep the generated sibling `installation.weft-state.json` with the input,
   outside version control. Use `weft export -o installation-export.yaml` to
   capture a new deterministic baseline after console edits.

Prices reproduce the rehearsal catalogue; they are example values, not a current
vendor quotation. The budget is USD 10 per month, with interruption at 110%; the
per-task limit is USD 2, with an 80% warning. Existing unmentioned objects,
catalogue history and spend are retained.

The graph retains the rehearsal's filesystem, Git, shell (`go`, `gofmt`, `git`)
and environment-status grants. Both installation and graph egress include
`storage.googleapis.com`, in addition to the Go proxy and checksum service.
The trigger has no extra query, polls every five minutes, enables readiness and
uses the `weft-github` hand-off with pull-request approval required. Review this
scope before granting consent; no apply is needed to inspect the example.
