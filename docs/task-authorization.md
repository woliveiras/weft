# Task authorization

Task consent and execution isolation are independent. Modes do not grant tools,
change credentials, expand child permissions or override installation policy.

Precedence is: installation restrictions and cancellation; exact agent grants
(including exact MCP names); reviewed task scope; mandatory tool/workflow gates;
then task-mode approval. Tool path, command, host and rate constraints still apply.

- **Ask for approval** adds a human decision before each granted tool call.
- **Workspace access** preauthorizes jailed filesystem tools, Git reads and sandbox
  shell calls. Other tools require a decision. Native shell is refused in this
  mode: a working directory or Git worktree is not OS confinement.
- **Full access** preauthorizes granted operations within the reviewed scope.
  Mandatory grant approvals, human questions, delegation and environment gates
  remain mandatory. This is not a risk classifier or a permission wildcard.

Consent must bind the human actor, task, workflow version, environment, repository
and base commit, capability snapshot and policy version. Full access confirmation
covers server execution location, files reachable by native commands, network,
external effects and Git commit/push where existing grants allow them. The model
cannot supply consent. Configuration changes invalidate prior consent; subsequent
calls fail closed. Children inherit the task ceiling in addition to their own
narrower grants. Native shell may reach anything the OS user can access; sandbox
restrictions remain enforced in Full access.

Preferences are suggestions requiring confirmation for each new task. Reduction
applies to subsequent admission and calls returning from approval. Already running
operations may finish; stopping requests cancellation and does not roll back
external effects. Auditing records actor, revisions and decisions, not prompts or
credentials. No automatic mode expansion or replay is permitted.

AUTH-01/02 are implemented. New tasks default to Ask at admission. Save or resolve
a draft to review capabilities and explicitly confirm another mode before starting.
Existing execution copies without consent retain their existing grant behavior.
The server seals the normalized execution copy before agents start, checks its
version and specification on each gateway call, and rechecks consent after approval
and environment preparation. Stale scope is denied, never silently refreshed.
AUTH-03 adds optional project suggestions, explicitly confirmed with the consent
save. Suggestions never preauthorize another task. Live changes only reduce automatic
execution (Full → Workspace/Ask, Workspace → Ask); concurrent stale changes fail.
Waiting calls refuse a decision made under an earlier consent revision. Stop task
uses the existing execution-copy cancellation flow. In-flight effects are not rolled
back. Preference-save failures are reported separately from an already saved task
consent. Integrated qualification remains PROMPT-07.


## Qualification and limits

Local qualification covers the prompt interpreter fixture, real Git smart-HTTP
import and worktrees, native tool gateway, all three modes, explicit approvals,
rejection, denied tools, revision changes, cancellation, and persisted consent after
store reopen. The native browser suite also exercises the preinstalled Software
Factory and its human handoff. The small prompt qualification workflow uses a
scripted model; it does not claim live model quality or live provider access.

The policy version identifies the server's task-authorization contract. Installation
restrictions are independently enforced at their existing boundaries on every call;
task consent does not replace or edit those policies. Existing task copies without
a consent record are labeled legacy and retain workflow approvals. They cannot be
silently promoted into a new mode while running.

Live private repositories, OS credential helpers/keychains, corporate providers,
real Docker sandbox execution and final user acceptance remain external validation.
This delivery does not activate or migrate the user's running installation.
