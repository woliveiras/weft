# Portable code-review adoption example

These files prepare a separate `code-review-portable` workflow. They do not
migrate the running GitHub pilot, change credentials or activate a route.

1. Configure and test the intended SCM connection and model provider. Select the
   actual provider edition and repository. Review its credential permissions and
   TLS trust separately. This example uses `team-scm`; replace that name in the
   agent's explicit repository grants and workflow parameters together.
2. Review `agent.json`, select a configured model, and publish it through
   `POST /api/v1/agents` (or configure the same reusable definition in Agents).
   This creates an immutable agent version. If the ID already exists, inspect
   it and explicitly publish a version with its current If-Match revision;
   do not overwrite another agent or silently pick its latest version.
3. Put that actual agent version in `workflow.yaml`. The workflow pins it
   explicitly and supplies connection, base branch and optional local review
   focus. Review the workflow plan before applying it:

   ```sh
   weft apply -f workflow.yaml --dry-run
   ```

   Applying the reviewed graph is separate from starting it. Agent instructions
   never create tool grants. The example grants read/summary and bounded HTTP
   research; it grants no merge, push, shell or MCP tools. Existing task budgets,
   tool approvals and installation restrictions continue to apply.
4. Copy the saved SCM connection's `updatedAt` value verbatim into the route's
   `connectionRevision`; also copy its exact provider, API instance and repository.
   Select the saved graph version, ingress channel and agent. `route.yaml` starts
   disabled. Apply/save reviewed dependencies before route adoption; changing
   them and activating a route in one YAML batch is refused.
5. Review the route with `POST /api/v1/review-routes/preview`, using
   `{"route": <reviewed input>}`. This reads local state without writing or
   contacting the provider. Start the selected workflow explicitly and verify
   its loaded version and idle event agent before enabling the route.
6. Review the enabled route plan, then apply with `--record-consent`, or save via
   the administrative route API with `{"route": <input>, "confirm": true}`.
   Effective activation records a durable event-sequence barrier. Only later
   Weft PR-opened events qualify. It never replays the backlog or starts a graph.
   Updating/re-enabling a route creates a new barrier. An unchanged save does not.
7. Before switching a live pilot, explicitly retire its previous bridge/routing
   for the selected scope. Do not run both paths and assume they deduplicate each
   other's external comments. This example does not stop that bridge for you.

## On-demand analysis in Chat

For a draft on an existing open PR, use a running native event reviewer with a repository read grant and a dedicated ingress. Open Chat and ask to review the PR. Confirm the pinned source and base commits, then follow the draft to its execution. The fixture journey uses a loopback SCM adapter. Automatic delivery routes and remote comment publication have their own grants and receipts.

## What to observe

Inspect `GET /api/v1/review-routes` and the paginated
`GET /api/v1/review-route-receipts` history. Each receipt includes exact provider,
instance, connection revision, repository, PR and source/base commits, source
fact, destination workflow version/run and stable message ID. Its `published`
state confirms engine dispatch acceptance, possibly held for channel approval,
not a finished review or a comment. Inspect the
workflow execution and `scm.review_summary_published` event for that later result.

Duplicate events/routes reserve one receipt per scoped PR revision. Queue delivery
is at least once; the SCM summary tool separately reconciles its durable comment
receipt to avoid a blind second publication. A changed connection, version/run,
missing grants or paused/stopped graph fails before dispatch; it is never started
as a side effect. Disabling prevents new/queued admissions but does not cancel
an already accepted in-flight dispatch.

A crash or lost publish response leaves `unknown`/`dispatching`, which cannot be
reposted. An operator can reconcile a receipt using its positive If-Match revision
and `{"confirm":true}` at `/review-route-receipts/{id}/reconcile`. Only exact
engine-authored retained message evidence confirms publication; missing/expired
history stays unknown. An admin can explicitly requeue a known pre-dispatch
`failed` receipt at `/retry` after restoring the unchanged route prerequisites.
Neither action creates a task, PR or merge. Stale revisions conflict.

Read [evidence coverage](../../docs/portable-review-evidence.md) before interpreting
checks. A no-findings review still publishes a summary. Missing evidence remains
visible; automatic merge is unavailable until an adapter has a qualified atomic
source-and-destination concurrency contract.

## Real qualification

Local tests use deterministic model responses, API/storage/queue components and
verified TLS provider fixtures. They do not qualify a live model's review quality
or a real Jira/Bitbucket account/version. A live rehearsal still needs approved
test resources, reviewed connections, the account restricted to the selected
project for automatic tracker writes, explicit mapping IDs and authorization to
create the test issue/branch/PR/comment. Record the actual edition/version,
connection pair, milestone times, observed outcome and cost in the INT-009 ledger.
