# Declarative installation configuration

An administrator can configure Weft from YAML, review a field diff, apply the
reviewed changes and export the installation. The console remains editable.
This is application configuration, not bootstrap authentication or deployment.

## Review and apply

```sh
weft apply -f examples/installation/ --dry-run
weft apply -f examples/installation/                 # review, then answer yes
weft apply -f examples/installation/ --yes            # non-interactive confirmation
weft export -o installation.yaml                     # refuses to overwrite an existing file
weft apply -f installation.yaml --dry-run
```

`WEFT_URL` selects the server (default `http://localhost:8080`); `WEFT_TOKEN`
provides an existing admin credential. Viewer and operator credentials cannot
plan, apply or export installation configuration. The CLI reads YAML locally and
sends its text to the server. It never opens referenced secret files or resolves
secret environment variables on the client.

A file may contain several `---`-separated documents. Directories are traversed
recursively, sorted by path, and include `.yaml` and `.yml` files. Symlinks,
YAML aliases, duplicate identities and unknown fields are rejected. The request
is bounded to 4 MiB and 500 objects. Errors identify fields without echoing YAML
snippets. Existing graph documents remain supported by `weft apply -f graph.yaml`;
apply now reviews and confirms writes. Graph `x-` extensions are preserved.

```yaml
apiVersion: weft.io/v1
kind: ModelProvider
metadata:
  name: gemini
spec:
  type: gemini
  defaultModel: gemini-3.6-flash
  concurrency: 2
  contextWindow: 1048576
  secretRef: {env: WEFT_SECRET_GEMINI}
```

The plan reports `create`, `update`, `unchanged`, `conflict` or `error`, with
before/after field differences. Secret changes are only `set`, `unchanged` or
`changed`. Missing references are errors (`secret reference unresolved`);
planning never discovers tools, tests connections, downloads repositories,
builds environments or records consent. It also never writes a revision sidecar.
A plan containing errors or conflicts cannot be applied.

## Object kinds

All documents use `apiVersion: weft.io/v1`, `kind`, `metadata.name`, optional
`metadata.revision`, and `spec`. Spec properties use existing API field names.

| Kind | Identity and configuration |
| --- | --- |
| `InferencePlugin` | Plugin ID as name; `enabled` boolean. Built-in plugins only. |
| `ModelProvider` | Provider instance name; provider input fields (`type`, `baseUrl`, `defaultModel`, `timeoutSec`, `maxRetries`, `concurrency`, `contextWindow`, `modelGuidance`, `project`, `region`), optional explicit `default` selection (`false` relinquishes an explicit selection; automatic fallback still applies), and `secretRef` instead of `apiKey`. |
| `PriceCatalog` | Name `installation`; `currency`, `unpriced`, `prices` using the existing catalogue input. |
| `CostPolicy` | Name `installation`; `taskRunBudgetMicros`, `warningPercent`. |
| `Budget` | Name `scope/id`, e.g. `installation/*`; `scope`, `id`, `limitMicros`, `warningPercent`, `interruptPercent`. Project scopes use registered project IDs. Unmentioned budgets are preserved. |
| `NetworkPolicy` | Name `installation`; `setupNetworks`, `runNetworks`, `egressHosts`. Existing installation restrictions remain in force. |
| `MCPConnection` | Connection ID as name; connection input including display `name`, `service`, `endpoint`, `enabled`, quota, transport, template, argument policy and OAuth configuration. Use references for credentials. |
| `SCMConnection` | Connection name; `kind`, `repository`, `apiUrl`, `gitUrl`, `branchPrefix`, `requirePullRequestApproval`, `secretRef`. |
| `Project` | An alias or exported registered project ID; `url`, optional `ref` (default `HEAD`), `checks`, `executionMode`. Existing local registrations export `path` and `displayName`. |
| `Graph` | Existing graph metadata and spec, including manual environments. `metadata.id` remains the graph identity; absent IDs derive from the display name. |
| `IssueTrigger` | Exported trigger ID, or an unambiguous display name for a hand-authored document; existing trigger input including optional display `name`, with mandatory `authorizationMode`. `projectId` can reference a Project alias in the same request. |

Money uses integer micro-units: USD 2 is `2000000`, USD 10 is `10000000`.
Prices use micro-units per **million tokens**. Weft does not fetch or update
vendor prices automatically. Review example rates against your own agreement.

Provider activation precedes instances. Cost and network policy, connections and
projects precede graphs; graphs precede triggers. Within a kind, names are sorted; explicit default providers follow other providers.
A failure stops remaining writes and reports each object as `applied`,
`unchanged`, `failed` or `not-applied`. Earlier writes remain; this is not a
transaction across services. Administrator writes on the same server wait while
a batch is applying, including during imports; viewer reads and operator runtime
controls remain available. Native store revision checks also fence writes across
server instances. Retry uses the same identities and retains completed
work. Omitted installation objects are never deleted.

## Revisions and console edits

Export embeds an opaque per-object revision. Successful apply writes revisions
atomically to a sibling `<input>.weft-state.json`, including objects completed
before a later failure. Keep this file with the local operational input. It holds
server identity and revisions only, never credentials. It is ignored by Git.

The sidecar records the last apply known to that input. Without it, an existing
object with differing desired configuration requires an exported revision or
explicit adoption. A console edit after the baseline produces a conflict.
If a known object was removed or renamed, apply conflicts instead of silently creating a replacement. Export again to use a trigger's stable ID after a rename.
Discovery health, polling counters and spend do not change declarative revisions. Budget revisions advance on edits even when a later edit restores the old amount; changing another budget does not invalidate this one.
The server checks the reviewed plan again before writing, including when forced.

```sh
weft apply -f installation.yaml --dry-run
weft apply -f installation.yaml --force ModelProvider/gemini --yes
```

`--force Kind/name` is repeatable and overrides only that object's baseline
conflict. It cannot bypass admin authorization, validation, confinement or a
change made after the reviewed plan. Export again when the source of a revision
is uncertain. No-op apply does not save, renew consent or run remote checks.

## Server-side secret references

Never place `apiKey`, `token`, `credential`, `oauthClientSecret`,
`processEnvironment` or inline import authentication in `spec`. They are rejected
locally and on the server. Use one of:

```yaml
secretRef: {env: WEFT_SECRET_GEMINI}
# or
secretRef: {file: gemini.key}
# generated by export, to retain an already stored credential:
secretRef: {preserve: true}
```

Environment references must match `WEFT_SECRET_[A-Z0-9_]+`. Installation encryption
key variables, key controls and the configured key environment names are always
forbidden, even if they match the prefix. `WEFT_ADMIN_TOKEN`,
`WEFT_RUNTIME_SECRET` and `POSTGRES_PASSWORD` are never resolvable references.

File references are relative to `secrets.dir` (or `WEFT_SECRETS_DIR`). Empty
configuration disables file references. Absolute paths, `..` components,
nonregular files and symlink escapes are refused. Root-constrained opens prevent
symlink traversal races. Maximum size is 16 KiB; component credential limits may
be smaller. Empty values fail. References are re-resolved at apply, and existing
managers encrypt the value using the installation key. Omission preserves the
stored credential; it never means clear. A preserve placeholder requires an
existing credential, so fill the commented references before using an export on
a new installation.

MCP also accepts `oauthClientSecretRef` and `processEnvironmentRefs`, a map from
process environment key to a reference. Omitted process keys remain stored.
Basic MCP credentials use `credentialUsernameRef` with `secretRef` for the
password. Browser OAuth authorization remains interactive. Export preserves
existing credentials without exporting browser access or refresh tokens.
Private project imports may use `username` and `secretRef` for an explicit token
attempt; import authentication remains transient under the existing import
contract and is not exported or stored as a project credential.

### Compose

The default Compose file forwards these optional variables without requiring
new values for existing installations:

```dotenv
# Populate locally; never commit actual values.
WEFT_SECRET_GEMINI=<provider key>
WEFT_SECRET_GITHUB_MCP=<scoped MCP token>
WEFT_SECRET_GITHUB_SCM=<scoped SCM token>
```

Put them in your existing local `.env` and recreate `weftd` when ready; Compose
does not forward arbitrary variables automatically. Add an explicit `environment`
entry for any additional dedicated reference. Preserve bootstrap credentials and
the installation encryption key.

Alternatively configure `secrets: {dir: /run/weft-secrets}` in the server YAML
(or `WEFT_SECRETS_DIR=/run/weft-secrets`) and add a read-only Compose mount:

```yaml
services:
  weftd:
    environment:
      WEFT_SECRETS_DIR: /run/weft-secrets
    volumes:
      - ./installation-secrets:/run/weft-secrets:ro
```

Create that directory yourself, keep it outside version control, and make only
the intended files readable by container UID 10001. Do not mount the installation
key directory as the reference directory. No default secret-directory mount is
added, so an existing stack starts without extra files or credentials.

## Triggers, imports and environments

Trigger authorization is always read from YAML: `ask`, `workspace` or `full`.
The plan describes the scope, polling interval, selected mode and hand-off.
`--yes` confirms writes and repository downloads; it **does not grant trigger
consent**.

```sh
weft apply -f examples/installation/ --yes --record-consent
```

Without `--record-consent`, a new or changed trigger is saved atomically paused
with `consent required`, without invented consent. With the flag, the existing
scope validator records actor, timestamp and the requested mode. The flag can
activate an otherwise unchanged trigger waiting specifically for consent.
No-op exports preserve paused state and existing consent. Console creation and
scope edits record the reviewing admin's consent; editing a paused trigger keeps
it paused. Runner checks and tool grants still apply in every authorization mode.

A Project downloads through the same reviewed import manager as the console.
The CLI's confirmation covers this download. URL/ref pairs reuse the pinned
snapshot, including `HEAD`; they do not silently refresh branches. Existing project source URL/ref/path is immutable; use a new identity for another source. Apply waits
for the import before dependent objects. Failed imports keep durable progress and
use the existing revision-fenced retry. Configuration does not start workflows,
run tasks, push branches or write tracker issues.

Manual graph environments are saved without building. The report warns when a
changed environment needs a build. Use the existing environment build action
after review. An unavailable MCP endpoint is retained and reported with sanitized
discovery errors; dependent grants still require successfully discovered tools.
SCM saves report the result of the existing read-only connection test.

## HTTP API

See [API documentation](api.md#declarative-installation-configuration) and
[OpenAPI](../api/openapi.yaml). Plan and apply share `files`, `revisions`, `force`,
`recordConsent`, `expected` and `confirmed`; plan ignores write confirmation.
Apply requires `confirmed: true` and `expected` keyed by object identity with the
revisions returned by the immediately reviewed plan. Reports use HTTP 200 with
`ok: false` for object errors/partial results; malformed requests use HTTP 400 and
role failures use 401/403. Export returns `application/yaml`.

Implicit deletion, a console YAML editor, automatic builds and last-applied
console badges are not included. Runtime task copies, spend history and transient
runtime state are excluded from export.
