# Provider Protocol v1 (out-of-process inference plugins)

Status: **design, not implemented** (PROV-05). Implementation is PROV-06.

Built-in inference plugins are Go adapters compiled into Weft
([extensibility](extensibility.md#add-a-model-provider)). This protocol lets
an administrator install a provider plugin that runs as a separate process,
so a backend can be added without rebuilding Weft. It is derived from
`llm.Provider` (`Chat`, `Models`), the plugin manifest (`llm.Manifest`) and the
[provider conformance suite](extensibility.md#provider-conformance-suite).

## Trust model

A provider plugin receives prompts, conversation history, tool schemas and
tool results — which include code and data from the user's repositories. It
is therefore trusted with that content, and treated accordingly:

- **Explicit installation.** Only an administrator installs a plugin, from a
  package whose SHA-256 checksum they confirm. Weft never downloads or
  updates a plugin by itself.
- **Separate process, least privilege.** The plugin runs as its own process
  with an empty environment, a private working directory and no access to
  Weft's data directory, installation key or other plugins' credentials.
- **Its own credential only.** A plugin that needs an API key receives that
  one key, decrypted by Weft at start, through the protocol — never through
  arguments, environment variables or files Weft writes.
- **Declared egress.** The manifest lists the hosts the plugin calls. Where the
  installation enforces egress (sandbox network profiles), only those hosts
  are reachable; elsewhere the list is shown to the administrator at install
  and in the console.
- **Marked in the console.** Providers built from a plugin are labelled
  *External plugin* with the plugin name, version and checksum.
- **Gateway unchanged.** Every call still goes through the LLM gateway:
  concurrency limits, metrics, tracing, cost records and budgets apply as for
  built-in providers. Agents never talk to a plugin directly.

## Package and manifest

A plugin package is a directory (or `.tar.gz`) with an executable and
`weft-plugin.json`:

```json
{
  "protocol": "weft.provider/v1",
  "id": "acme",
  "name": "Acme models",
  "version": "1.4.0",
  "executable": "bin/acme-provider",
  "checksum": "sha256:…",
  "description": "Acme hosted models",
  "local": false,
  "defaultBaseUrl": "https://api.acme.example",
  "key": "required",
  "capabilities": {"tools": true, "json": "native", "usage": true, "modelListing": true, "streaming": false},
  "settings": ["contextWindow"],
  "egress": ["api.acme.example"]
}
```

The fields after `executable`/`checksum` are those of `llm.Manifest`, so a
plugin appears in **Inference plugins** exactly like a built-in one and is
enabled, disabled and configured the same way. `id` must not collide with an
installed plugin. `checksum` covers the executable; Weft verifies it at
install and again before every start.

## Transport

**stdio, newline-delimited JSON-RPC 2.0.** Weft starts the executable and
exchanges one JSON object per line on stdin/stdout; stderr is captured as
bounded, redacted log lines. Stdio avoids listening sockets, needs no port
allocation and ties the plugin's lifetime to Weft's. (An HTTP transport was
considered and rejected for v1: it needs a port, loopback authentication and
more ways to be reached by other local processes.)

Limits: one message at most 8 MiB; at most `concurrency` requests in flight
per provider instance (the gateway's limit); a request without an answer
within the instance's timeout is cancelled.

## Lifecycle

One process per configured provider instance, started lazily on the first
call and stopped after 10 minutes idle or when the instance or plugin is
disabled, removed or reconfigured.

1. `initialize` → `{protocol, id, version}` must match the manifest.
   Params: `{instance, baseUrl, defaultModel, contextWindow, timeoutSec, credential?}`.
   The credential is the only secret the plugin ever receives.
2. `chat`, `models` — see below.
3. `$/cancel` (notification) `{id}` — the gateway's context was cancelled.
4. `shutdown` → `{}`; then stdin is closed. A process that has not exited 5
   seconds later is killed (process group).

A crashed process fails its in-flight calls with `provider_unavailable` and is
restarted on the next call, with exponential backoff (1 s to 60 s) after
repeated crashes.

## Methods

`chat` params are `llm.ChatRequest` and the result is `llm.ChatResponse`, in
their existing JSON shapes (camelCase, as on the LLM callback endpoint):
`model, system, messages[{role, content, toolCalls[{id, name, arguments}],
toolCallId, name}], tools[{name, description, parameters}], temperature,
maxTokens, json, stop` → `message, stopReason, model, usage{inputTokens,
outputTokens, cacheReadTokens?, cacheWriteTokens?}`. Tool names are platform
names (`filesystem.read`); mapping them to a vendor's character set is the
plugin's job. `metadata` is never sent.

`models` params are `{}` and the result is `[{id, contextSize?}]`.

## Errors

JSON-RPC errors carry `data: {kind, status?, retryAfterMs?, message}`:

| kind | Meaning | Gateway behaviour |
|---|---|---|
| `invalid_request` | The request cannot be served as sent | Not retried; not charged |
| `unauthorized` | The credential was refused | Not retried; not charged; provider shown failing |
| `rate_limited` | Backend throttled | Retried with `retryAfterMs`; charged at estimate if it had been dispatched |
| `unavailable` | Backend down or 5xx | Retried; charged at estimate if dispatched |
| `context_exceeded` | Prompt does not fit | Surfaces as `llm.ErrContextExceeded` |
| `cancelled` | Answer to `$/cancel` | Context error |

`message` must not contain the credential; Weft redacts it again before
logging or showing it.

## Enabling a plugin

Installing registers the plugin **disabled**. Before an administrator can
enable it, Weft runs the conformance suite's wire-independent checks
against a plugin process pointed at a scripted backend when the plugin
supports `baseUrl` overrides, and otherwise the live subset
(`conformance.RunLive`) against the administrator's test instance. Results
are stored with the plugin version; a new version must pass again.

## Out of scope for v1

Streaming responses, embeddings, multimodal inputs, plugin-to-plugin calls
and automatic updates.
