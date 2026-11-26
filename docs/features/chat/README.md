# Chat

Open Chat from the left navigation or the console's home URL. Start with
**New chat** above **Recents** on the right. On a narrow screen, open that
panel from **Sessions**.

## Choose the model

An administrator selects a configured provider and model in
**Models → Chat model**. The console opens stored conversations and manual
pages without a usable model. If interpretation becomes unavailable, use
**Retry saved request** after restoring the model or budget. Supply provider
credentials only in the existing Models configuration form.

When a saved conversation stays **Untitled chat**, use **Retry title** to
generate a title from the first request. The transcript and any open reviews
remain available while the title is generated.

## Follow a task

Ask Chat to show the current state of a task or follow it until it settles.
Use a task ID or a unique exact title. For example, “Let me know when my
release notes review finishes.” If Weft cannot resolve a unique task, supply
the ID from Tasks. Use the manual pages to start tasks, decide their approval
requests or change workflow configuration.

Start another chat or close the browser. Weft keeps observing active tasks.
Recents shows the running indicator and signals an unread result, with a
green dot for success. Failure, cancellation and a required decision have
their own accessible status. Open the original conversation and follow the
task result link to its evidence. If the provider is down, the monitor
continues to read task state without calling that provider.

## Create a reusable agent

As an administrator, describe the agent's name and what it should do. Chat
shows the proposed ID, instructions, selected model, read-only workspace and
empty tool grants. Check the definition and choose **Create agent** or
**Decline**. If there is exactly one active proposal, type
“Confirm this proposal” or “Decline this proposal” to decide it. Open **View
agent** after the catalog confirms version creation. Adding the definition
to a workflow and running it remain separate steps in Workflows.

A stale or expired review requires a new proposal. If a response is lost,
return to the conversation to check the catalog receipt before trying again.

## Archive and access

Use **Archive conversation** to move a conversation out of Recents. In
**Archived**, open it to view new results or restore it. Archiving keeps the
watch and read state. Active and archived conversations are retained without
automatic expiration. The Archived tab shows an unread count even while you
are browsing Recents. Use **Older conversations** to load the next page.

In an authenticated installation the owner can access their conversations.
Auth-disabled local installations show **Shared local history**. People using
that local installation can access the same conversations. Private history is
scoped to the authenticated static or OIDC principal.
