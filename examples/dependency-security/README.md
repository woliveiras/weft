# Manual dependency security

`dependency-security` is an opt-in task workflow. It reads current GitHub
Dependabot alerts through `scm.security.alerts`, groups alerts by manifest,
ecosystem and package, and prepares at most one compatible security update per
manual task. Saving the workflow does not execute it or create a schedule.

## Install and review

1. Update the Weft server to a version exposing `scm.security.alerts` in the tool
   catalog. The existing GitHub SCM connection must be named `weft-github`, scoped
   to `woliveiras/weft`, with its existing server-held credential. For another
   repository, review and update the example repository, connection grants and
   prompt together. GitHub requires repository **Dependabot alerts: read** in
   addition to the existing contents/PR delivery permissions. Do not paste tokens
   into a prompt, task or YAML. A 401/403/404 is a discovery failure, never zero
   alerts. Other SCM adapters explicitly report this capability unavailable.
2. Review `installation.yaml`. It adds `registry.npmjs.org` to the existing three
   Go egress hosts, retains the existing network modes and registers a separate
   `weft-security` repository snapshot at `main`. Merge with any additional hosts
   in your installation rather than overwriting them. It does not alter secrets,
   budgets, connections, existing projects, reviewer routes or PR approval.
3. Inspect the plan:

   ```sh
   weft apply -f examples/dependency-security/ --dry-run
   ```

   Apply only after reviewing/authorizing the concrete plan. Resolve any ownership
   conflict through the normal reviewed `--force NetworkPolicy/installation`
   adoption; do not force unrelated objects. No `--record-consent` is needed:
   this example contains no issue trigger or scheduled dispatch.
4. The inline agent becomes reusable through the normal graph save/catalog
   behavior. Later published agent versions are adopted explicitly per workflow.
   The graph stays stopped. Do not use **Run all** on its canvas to launch a
   standalone source-graph run; use a task so consent, budgets and worktree
   evidence belong to the manual request.

## Run manually

1. In **Tasks → New task**, choose workflow **Dependency security**, entry/result
   agent **Dependency Security Fixer**, and project **weft-security** with the
   workflow environment. Review the pinned base commit; if it is stale, import
   and review a fresh `main` snapshot before starting. Existing task pins never
   advance silently.
2. Use a request such as:

   > Investigate the current dependency security alerts. Resolve at most one
   > compatible group, run the required verification and prepare a PR. Report
   > existing PRs, unfixable alerts and remaining work.

3. Leave **Source control hand-off** at **None — review and push manually** for
   this workflow: its reviewed agent already has exact connection delivery
   grants and uses the stable security-group branch. Selecting task hand-off
   would add instructions to use a different per-task branch. This selector's
   generic label describes the default task behavior, not this workflow's
   explicitly granted delivery tools.
4. Save/review the task, confirm its budget and authorization mode, then click
   **Run task**. Full mode may run the granted tools automatically, but does not
   bypass mandatory PR approval. Approve the task environment if requested. The
   environment uses Node 22, pinned pnpm 10.33.4 and Go 1.26 on the dedicated
   sandbox daemon. No host Docker socket is exposed.
5. Follow **Agent plans**, actual tool/check evidence and the proposed PR. Approve
   PR creation only after reviewing the verification. Existing configured
   `scm.pull_request_opened` review routes still apply; the workflow neither
   changes those routes nor grants merge. Inspect the review outcome separately.

The agent must inspect parent dependency and runtime compatibility before using
a patched transitive dependency. A first patched version is advisory data, not
permission for an incompatible override or major upgrade. Installs disable
lifecycle scripts by default. Missing checks, audit findings or incompatible
patches are reported explicitly, not converted into successful remediation.

## Duplicates and recovery

The read tool returns complete bounded observations of open alerts and open PRs
on the explicit base (up to ten pages of 100 records each, 2 MiB per response and
32 KiB of final output (the default gateway model-result ceiling)). It fails on incomplete/malformed pages or permission,
transport and rate-limit errors. It uses server-held credentials; remote text
cannot grant permissions. Gateway invocation events retain normal outcome and
latency diagnostics.

Each group receives a stable branch under the SCM connection prefix. Matching
same-repository open PRs are shown in `existingPullRequests`; forks and other
bases do not match. The agent skips these groups and checks again immediately
before delivery. This is not an atomic snapshot or distributed lock. Another
actor can race the observation; never force a push or promise global deduplication.
PRs created outside this branch convention require operator reconciliation.

If a prior PR was closed/merged but its branch remains, do not reset or overwrite
it. Stop on conflicting branch/push state; the operator reviews the retained
branch and chooses recovery. For an uncertain PR creation, reconcile the existing
PR before retrying. Installing does not start a recovery or remediation task.

After merge, GitHub later observes the updated dependency graph and may close
alerts. An opened PR, successful agent plan or local audit is not evidence that
the remote alert was closed. This workflow never dismisses alerts. Start another
manual task for another group only after reviewing existing results.

## Verification boundary

Unit tests and encrypted-store/gateway/HTTP integration tests cover grouping,
pagination, malformed input, credential reflection, denied access and PR matching.
The API/runtime example test proves that saving/drafting does not call the model
or provider, and that explicit task dispatch reads through the real gateway,
including a denied-provider failure. Its deterministic model does not establish
that a live model will fix arbitrary dependencies. Sandbox build, live credential
permissions, actual remediation, PR creation and reviewer behavior require their
own authorized live evidence.

Reference: [GitHub Dependabot alerts API](https://docs.github.com/en/rest/dependabot/alerts).
