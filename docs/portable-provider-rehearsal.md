# Portable provider rehearsal

Use disposable operator-approved resources. Local fixtures do not qualify a real
provider, credential, model, Data Center version or provider combination. Nothing
in this guide authorizes writing to an installed connection or external account.

## Record the selected combination

Record tracker edition, site/cloud ID, numeric project, Git provider edition,
API instance, repository, actual Data Center versions where applicable, test issue,
base branch and model. Test Cloud and Data Center separately; the same issue key
or local connection name is not the same authority on two instances.

There is no presumed Data Center support range. Record the version actually
exercised and the operations that passed. Use a trusted certificate chain or
explicitly reviewed CA configuration; do not bypass TLS to make a test pass.

## Review connections and scope

1. Configure and test the selected source-control connection. Review its exact
   repository, branch prefix and PR approval policy. The account must have the
   intended branch/PR/comment permissions. A successful identity check does not
   prove every write permission or branch rule.
   For Bitbucket Cloud, new connections can select the explicit API-token mode
   with the owner's Atlassian email and a scoped token. Follow the
   [console/token steps](atlassian-bitbucket-setup.md#create-and-enter-a-scoped-api-token).
   Existing Bearer connections are not migrated. Initialize the repository's
   default branch with a commit before importing/registering it for a task.
2. For Jira Data Center, configure its native issue connection with exact site
   URL, numeric project and project-restricted technical account. For Jira Cloud,
   configure a supported first-party MCP connection and its independent Jira REST
   credential (see [setup](atlassian-bitbucket-setup.md#independent-jira-cloud-rest-access)), then a
   credential-free Cloud workflow profile pinning that connection revision,
   cloud ID and numeric project. Review the remote account permissions before
   confirming `projectRestricted`; the flag does not modify Jira permissions.
   MCP OAuth credentials are not reused for REST. Explicit Basic REST credentials
   may coexist with MCP OAuth. Qualify both audiences; MCP Ready and a stored REST
   credential flag do not prove a typed REST request succeeded.
3. Discover transitions from a real sample issue of the intended type in the
   intended source state. Select numeric transition and destination status IDs.
   `Review`, `In Review`, translated names and board columns are presentation,
   never lookup authority. Required transition-field updates remain unsupported.
4. Save each definition, inspect its revision, then explicitly select mappings
   and record trigger consent. Review a YAML dry-run before every apply. Save
   dependencies first; do not change a credential/profile/mapping and adopt its
   future revision silently in the same batch. Existing GitHub/Jira workflows
   remain unchanged until separately reviewed adoption.

## Exercise the issue-to-PR path

Use a small, bounded change with meaningful checks. Review the actual task budget,
installation budget, environment and allowed tool/network grants before starting.
Never place credentials in an issue, prompt, agent parameter or test transcript.

Record UTC times and durable IDs for selection/poll, readiness result, claim,
task creation, required environment approval, actual runtime admission, check
results, commit, PR approval and delivered PR. Confirm that tracker and SCM use
independent reviewed connections. Jira issue links must survive delivery without
claiming that GitHub `Closes #...` syntax closes a Jira issue.

If mappings are selected, observe `workStarted` after runtime admission and
`prDelivered` after the durable PR-opened event. Task creation or waiting for
approval is not work started. Optional synchronization has its own result; its
failure must not erase a delivered PR or silently report success. Inspect its
retained profile/connection/mapping revisions, numeric issue/type/status IDs and
historical transition intent.

On an uncertain write, reconcile by reading. Do not click retry until the system
proves failure before dispatch. An admin may requeue that known preflight failure
only under unchanged reviewed authority. Mapping edits, account revocation,
required fields and a human moving the issue to another state must fail visibly.
Do not rename a remote status merely to match a Weft expectation.

## Exercise portable review

Adopt the [separate portable reviewer](../examples/portable-code-review/README.md)
with its actual published agent version, saved workflow version and scoped grants.
Explicitly start the workflow, review the route preview, then activate it with
recorded consent. Older events are excluded. Retire an old pilot bridge explicitly
before routing the same scope through the new path.

Create a new Weft PR only after activation. Record the source event, route receipt,
execution and summary event. `published` on the routing receipt means engine
acceptance, possibly awaiting channel approval; it does not prove review completion.
Verify the actual comment and its exact reviewed head/base revisions. Duplicate
source events must not cause a blind second summary publication. Response loss
must retain uncertainty and allow read-only reconciliation.

Run a no-findings case and a case with an intentionally seeded, bounded defect.
The review must publish a summary in both cases. Missing tests/checks or incomplete
provider evidence must be stated, not interpreted as passing. This live exercise
must assess the model's findings and reasoning; scripted model fixtures only
prove transport and orchestration.

Automatic portable merge remains unavailable because the adapters do not yet have
qualified atomic protection for both reviewed source and destination revisions.
No alternate Git push/CLI merge bypass is granted. Review/merge confirmation and
any resulting Done transition therefore remain an explicit separate qualification
boundary; do not mark that stage passed after only summary publication.

## Save evidence

Append actual results to INT-009 evidence and the operator rehearsal record:

- Product/edition/version and tracker/SCM combination.
- UTC milestones, durations and task/run/PR/event/receipt IDs.
- Required approvals and actual observed checks/comments/transitions.
- Positive, denied, changed-revision and uncertain-outcome results.
- Model usage/cost from recorded usage and configured prices.
- Unavailable or untested stages, with their concrete cause and next action.

Do not include credentials, raw personal issue content or unredacted authentication
headers. Keep synthetic fixtures, real-account results and inferred conclusions
clearly distinguished. PROD-14 live evidence remains pending until this exercise
is authorized and observed on the selected resources.
