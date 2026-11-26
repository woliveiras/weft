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

## Follow a task

Ask Chat to show the current state of a task or follow it until it settles.
Use a task ID or a unique exact title. For example, “Let me know when my
release notes review finishes.” If Weft cannot resolve a unique task, supply
the ID from Tasks. The assistant can inspect and follow tasks in this delivery.
Use the manual pages to start work, decide approval requests or change
configuration.

Start another chat or close the browser. Weft keeps observing active tasks.
Recents shows the running indicator and signals an unread result, with a
green dot for success. Failure, cancellation and a required decision have
their own accessible status. Open the original conversation and follow the
task result link to its evidence. If the provider is down, the monitor
continues to read task state without calling that provider.

Use **Archive conversation** to move a conversation out of Recents. In
**Archived**, open it to view new results or restore it. Archiving keeps the
watch and read state. Active and archived conversations are retained without
automatic expiration.

In an authenticated installation the owner can access their conversations.
Auth-disabled local installations show **Shared local history**. People using
that local installation can access the same conversations. Private history is
scoped to the authenticated static or OIDC principal.
