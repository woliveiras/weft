# Projects

Projects register repositories and pin a commit for later task worktrees.
Open **Configuration → Projects** (`#/projects`). The table provides search,
execution-environment filtering, repository/ref/commit facts, optional server
path disclosure and a **Create task** link. That link opens
`#/tasks/new?project=<id>` and selects this project after resource discovery,
including its concrete registered base commit and environment preference.
The task still needs a workflow, requirements review and explicit execution
authorization. Unknown or unavailable project IDs remain visible for correction.

A task without a selected project can still inherit a repository configured in
the workflow workspace. The task form's **No repository (managed workspace)**
label does not disable that workflow initialization. Selecting a project when
the workflow also configures a repository is rejected to avoid two owners.

## Register an existing server repository

1. Select **Add project** and the registration path (`#/projects/new`).
2. Supply a project name and absolute repository path on the machine running
   Weft. For containerized Weft this is its container filesystem, not an
   arbitrary path on the browser user's computer.
3. Review the Git metadata access explanation and select its authorization
   checkbox. Registration is administrative and reads repository metadata;
   it does not execute agents.
4. **Register project** persists the project and registered commit. Invalid
   paths/repositories and access failures leave an actionable form error.
5. Select the project in a task's **Workflow & repositories** section and review
   its base commit. Task worktrees start from the reviewed commit; uncommitted
   source changes remain in the original checkout.

Starting from **Create task** avoids selecting the repository again. This
project context wins over a workflow's suggested project. Changing the project
in the task deliberately updates its base/preference; late discovery does not
replace an explicit choice or an existing saved task. **Review task** saves
the selected version/base and checks that saved revision without starting work.

## Import from a remote URL

1. Open the import path (`#/projects/import`), also available from task setup.
2. Enter an HTTPS clone URL and base reference (`HEAD`, branch/tag reference or
   full commit hash). Select public access or a supported explicit Git
   authentication source. Credentials configured in the host CLI or MCP are
   not implicitly copied into the import process.
3. Review and authorize the server-side download, then import. The UI states
   transfer/time limits and reports progress/failure. Import does not start
   agents or expand their grants.
4. Choose a successful saved import and register/use its repository as offered
   by the calling form. A saved snapshot pins its commit; selecting it again
   does not silently fetch a new branch tip.

Import cancellation stops the attempt through its own API operation; it is not
permission to delete an existing checkout. Authentication/network/size/time
failures require correcting the import inputs or connection and retrying.

## Execution preferences and checks

A registered project's **Configure preference** controls the environment
suggested for new tasks. Save explicitly. Existing saved task drafts remain
unchanged; every task still needs compatible workflow isolation and review.
Local worktrees require native Weft and isolate files rather than OS access.

Projects can also have declared checks through the project checks API. Do not
infer tests are declared merely because a repository contains test files, or
promise a complete console check editor where only the API contract exists.
Task evidence distinguishes declared checks from arbitrary shell commands.

## Implementation and verification map

- [ProjectsPage](../../../web/src/components/ProjectsPage.tsx),
  [ProjectPreference](../../../web/src/components/ProjectPreference.tsx),
  [RepositoryImport](../../../web/src/components/RepositoryImport.tsx).
- [Project API](../../../server/internal/api/projects.go),
  [project configuration](../../../server/internal/projectconfig) and
  [repository import](../../../server/internal/repositoryimport).
- [Project UI tests](../../../web/src/components/ProjectsPage.test.tsx).

Continue with [tasks](../tasks/README.md), [connections](../connections/README.md)
and [setup and readiness](../setup-and-readiness/README.md). The detailed
[task workflow guide](../../task-workflows.md) describes admission boundaries.
