# Configuration reference

`weftd` reads configuration in three layers; later layers win:

1. **Built-in defaults** — a laptop setup with no services: SQLite, filesystem
   queues, the offline `mock` model provider (`config.Default()`).
2. **Config file** — YAML, given by `--config <path>` or `WEFT_CONFIG`.
   `${VAR}` references in the file are expanded from the environment before
   parsing. The container image ships `deploy/config/weft.yaml` at
   `/etc/weft/weft.yaml`.
3. **Environment variables** — the overrides listed below.

Secrets are never written in the file: the file names the environment variable
(`tokenEnv`, `apiKeyEnv`) or mounted file (`tokenFile`, `apiKeyFile`) that
holds them. Startup fails fast on an invalid configuration (unknown default
provider, duplicate provider names, invalid roles, no API credentials).

## Reference

| Key | Env var | Default | Description |
|---|---|---|---|
| `instance` | `WEFT_INSTANCE` | hostname | Unique instance name; holder of replica leases and recorded on executions. Must differ per instance. |
| `server.addr` | `WEFT_ADDR` | `:8080` | Listen address (API, console, `/metrics`, `/internal`). |
| `server.internalUrl` | `WEFT_INTERNAL_URL` | `http://127.0.0.1:8080` | URL external agent runtimes use to call back **this** instance. Per instance (pod IP), never a load balancer. |
| `server.webDir` | `WEFT_WEB_DIR` | — (image: `/app/web`) | Serve the built console from this directory. |
| `server.corsOrigins` | — | `[]` | Allowed CORS origins (`*` allowed; avoid in production). |
| `server.maxBodyBytes` | — | `8388608` | Request body limit. |
| `server.internalAddr` | `WEFT_INTERNAL_ADDR` | — | Serve the callback API (`/internal/v1`) on its own listener (e.g. `:8081`) and remove it from `server.addr`; point `server.internalUrl` at that port. |
| `server.protectMetrics` | `WEFT_PROTECT_METRICS` | `false` | Require a viewer token on `/metrics` (configure Prometheus with a bearer token). |
| `auth.disabled` | `WEFT_AUTH_DISABLED` | `false` | Disable authentication entirely (local development only). |
| `auth.tokens[]` | `WEFT_ADMIN_TOKEN` adds `{name: admin, role: admin}` | `[]` | Static bearer tokens: `name`, `role` (`viewer`/`operator`/`admin`), and one of `tokenEnv`, `tokenFile`, `token` (inline, dev only). Minimum 12 characters. |
| `auth.oidc` | — | unset | `issuer`, `audience`, `roleClaim` (default `weft_role`). Accepts RS256/ES256 JWTs verified against the issuer's JWKS. |
| `storage.type` | `WEFT_STORAGE_TYPE` | `sqlite` | `sqlite`, `postgres`, `fs`. |
| `storage.dsn` | `WEFT_STORAGE_DSN` | `file:data/weft.db` | SQLite path/URI or PostgreSQL URL. |
| `storage.dir` | `WEFT_STORAGE_DIR` | `data` | Data directory for `fs` and default SQLite location. |
| `queue.type` | `WEFT_QUEUE_TYPE` | `fs` | `nats`, `redis`, `rabbitmq`, `kafka`, `fs`, `memory`. |
| `queue.url` | `WEFT_QUEUE_URL` | — | Broker URL (see [brokers](#brokers)). |
| `queue.dir` | `WEFT_QUEUE_DIR` | `data/queues` | Directory for the `fs` broker. |
| `queue.options` | — | `{}` | Adapter options (map of strings). |
| `workspaces.root` | `WEFT_WORKSPACES` | `data/workspaces` | One directory per graph; worktrees under `.worktrees/`. |
| `workspaces.hostRoot` | `WEFT_WORKSPACES_HOST` | = root | The same directory as the Docker daemon sees it (bind mounts for docker sandboxes when weftd runs in a container). Not needed with Compose: `sandbox-daemon` mounts the workspaces volume at the same path. |
| `workspaces.allowAbsolute` | — | `false` | Allow graphs to set an absolute `spec.workspace.path`. |
| `llm.devMock` | `WEFT_DEV_MOCK` | `false` | Offer the development `mock` plugin with a read-only `mock` provider (the default until an administrator picks another). Real providers are [inference plugins](#inference-plugins) configured in the console; `llm.providers` and `llm.default` were removed and are rejected. |
| `secrets.keyEnv` | `WEFT_SECRET_KEY` (the variable it names) | `WEFT_SECRET_KEY` | Variable holding the installation key (32 bytes, base64 or hex) that encrypts credentials entered in the console. Wins over `keyFile`. |
| `secrets.keyFile` | `WEFT_SECRET_KEY_FILE` | `data/secret.key` (image: `/var/lib/weft/data/secret.key`) | File holding the key when the variable is empty. |
| `secrets.generateKeyFile` | `WEFT_SECRET_KEY_GENERATE` | `true` | Create `keyFile` (mode 0600) with a random key on first start when no key exists. Turn off when several instances run (they must share one key). |
| `secrets.previousKeysEnv` | `WEFT_SECRET_KEY_PREVIOUS` (the variable it names) | `WEFT_SECRET_KEY_PREVIOUS` | Comma-separated older keys that still decrypt. Model-provider values are re-sealed on load; MCP credentials may still require previous keys. See the rotation runbook. |
| `runtimes.sharedSecretEnv` | — (image: `WEFT_RUNTIME_SECRET`) | — | Env var whose value is sent to external runtimes as `X-Weft-Runtime-Secret`. |
| — | `WEFT_RUNTIME_NETWORK` | — | Docker network for runtimes launched from `runtime.image` with docker isolation. |
| `isolation.docker.bin` | — | `docker` | Docker CLI used by docker sandboxes and launched runtimes. |
| `isolation.docker.image` | `WEFT_SANDBOX_IMAGE` | `weft/sandbox:latest` | Default sandbox image. |
| `isolation.docker.network` | — | `none` | Default sandbox network. |
| `isolation.docker.host` | `WEFT_SANDBOX_DOCKER_HOST` | — | Docker daemon for sandboxes (`DOCKER_HOST` syntax). Compose sets `tcp://sandbox-daemon:2376` with `DOCKER_TLS_VERIFY`/`DOCKER_CERT_PATH`. Setting it enables the `sandbox` readiness check. See [sandbox-runtime.md](sandbox-runtime.md). |
| `tools.allowPrivateNetworks` | — | `false` | Let `http.fetch` reach loopback/private/link-local addresses (SSRF guard off). |
| `observability.serviceName` | `OTEL_SERVICE_NAME` | `weft` | OTel service name. |
| `observability.otlpEndpoint` | `OTEL_EXPORTER_OTLP_ENDPOINT` | — | OTLP/HTTP base URL; empty disables trace/log export. |
| `observability.traceSampleRatio` | — | `1` | Parent-based ratio sampler. |
| `observability.logLevel` | `WEFT_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. |
| `observability.logFormat` | `WEFT_LOG_FORMAT` | `json` | `json` or `text`. |
| `observability.grafanaUrl` | `WEFT_GRAFANA_URL` | — | Console links to dashboards and traces. |
| `engine.leaseTTL` | — | `20s` | Replica lease lifetime (min 3s). Failover time ≈ TTL. |
| `engine.reconcileInterval` | — | `2s` | Reconcile/renew period; clamped to ≤ TTL/2. |
| `engine.retryBackoff` | — | `2s` | Base delay for nacked inputs (doubles per attempt, max 5m). |
| `engine.maxConcurrentTurns` | — | `0` (unlimited) | Cap on simultaneous turns per instance. |
| `bootstrap.graphs` | `WEFT_BOOTSTRAP_GRAPHS` (comma list) | `[]` | Graph files or directories loaded at startup (saved only when changed). |
| `bootstrap.start` | `WEFT_BOOTSTRAP_START` (comma list) | `[]` | Graph ids started after bootstrapping. |

### Inference plugins

Model providers are inference plugins. Each built-in plugin (`ollama`,
`openai`, `vllm`, `lmstudio`, `anthropic`, `openrouter`, `huggingface`/`tgi`)
publishes a manifest at `GET /api/v1/provider-plugins`: label, local or hosted,
default base URL, whether an API key is required, capabilities (tool calls,
JSON mode, usage, model listing, streaming) and optional settings. Every plugin
starts disabled; administrators enable or disable it in **Integrations → Model
providers** (`PUT /api/v1/provider-plugins/{id}`), and only enabled plugins
accept new providers. Disabling a plugin keeps its providers, which then report
unavailable until it is enabled again.

Provider fields: `name`, `type` (the plugin id), `baseUrl`, API key (stored
encrypted), `defaultModel`, `timeoutSec` (default 300), `maxRetries` (default
3; 429/408/5xx/network errors with backoff and `Retry-After`), `concurrency`
(per-provider in-flight cap, 0 = unlimited) and `contextWindow` (tokens per
request, prompt plus answer). Ollama receives it as `num_ctx` and defaults to
32768 instead of Ollama's own 4096. The other built-in types (OpenAI and its
compatibles — vLLM, LM Studio, OpenRouter, Hugging Face TGI, Gemini's
OpenAI-compatible endpoint — and Anthropic) cannot send it, so set it to the
model's real context size: native agents budget their conversation against
it, and when it is unset they assume 32768 tokens. Prompts that do not fit
fail the turn with "prompt exceeds the model context window" before they are
sent (Ollama would otherwise truncate them, silently dropping the task from
the conversation). The check uses a rough estimate of about 4 bytes per token,
so the provider may still reject a prompt slightly under the window.
`modelGuidance` is `""` (standard) or `"small"`: native agents on that
provider get compact tool schemas, call shapes in the system prompt and one
tool call per reply. It is off by default (see the measurements in
[native-coding-runtime.md](native-coding-runtime.md)).

Nodes select models with `model: {provider, name, temperature, maxTokens}`;
an empty provider means the default provider, an empty name the provider's
`defaultModel`.

### Providers managed in the console

Administrators add, edit, test and remove providers of enabled plugins under
**Integrations → Model providers**, and choose the default provider there. Those providers are stored in the database with their API
key encrypted by the installation key (`secrets.*`); keys are never returned
by the API after saving. Names cannot clash with the development `mock`
provider, and `default` is reserved. Changes apply immediately on the instance that
made them and within 5 seconds on other instances.

A node that selects a provider that does not exist or cannot be used (for
example, its credential cannot be decrypted) is reported as a warning when
the graph is saved, and the graph or node is refused with `422` when started.

### Installation key

Credentials entered in the console are encrypted (AES-256-GCM) with the
installation key before they reach storage. Without a key, the console
refuses to store credentials (`409 secret_key_missing`) and keyless
providers (Ollama, vLLM, ...) still work.

* **Single instance (Docker Compose):** by default a key is generated on
  first start in the `weftd-data` volume. Back up that volume (or set
  `WEFT_SECRET_KEY` yourself with `openssl rand -base64 32`): without the
  key, stored credentials must be entered again.
* **Several instances (Kubernetes):** set `WEFT_SECRET_KEY` from a Secret
  on every instance and set `secrets.generateKeyFile: false`.
* **Rotation:** set the new key as `WEFT_SECRET_KEY`, move the old one to
  `WEFT_SECRET_KEY_PREVIOUS`, restart (values are re-encrypted and the log
  reports model-provider re-sealing). Retain previous keys for MCP credentials
  and historical backups until separately verified; see the
  [rotation runbook](operations.md#encryption-key-rotation).
* **Lost key:** affected providers show *Unavailable* with the reason; agents
  selecting them fail with that reason instead of using another provider.
  Re-enter the credential in the console to repair them.

### Brokers

| Type | URL | Options |
|---|---|---|
| `nats` (default when type is empty) | `nats://host:4222` | `replicas` (stream replicas), `creds` (credentials file), `name`, `advisories=false` |
| `redis` | `redis://host:6379/0` (Redis ≥ 7) | — |
| `rabbitmq` | `amqp://user:pass@host:5672/` | `ackWait`, `maxDeliver` |
| `kafka` | `kafka://h1:9092,h2:9092` or host list | `partitions` (default 12), `replicationFactor`, `ackWait`, `maxDeliver`, `clientId` |
| `fs` | `file:///path` or `queue.dir` | — |
| `memory` | — | — (not durable; tests) |

### Storage

| Type | DSN | Notes |
|---|---|---|
| `sqlite` (default) | path or `file:` URI | WAL, single writer connection; single instance only. |
| `postgres` | `postgres://user:pass@host:5432/db?sslmode=require` | Required for multiple instances. Migrations run at startup under an advisory lock. |
| `fs` | `storage.dir` | JSON files + JSONL event log; one process per directory (lock file). |

## Example: production-style file

```yaml
instance: ${HOSTNAME}
server:
  addr: ":8080"
  internalUrl: "http://${POD_IP}:8080"
storage: {type: postgres}                      # DSN from WEFT_STORAGE_DSN
queue: {type: nats, url: "nats://nats:4222", options: {replicas: "3"}}
workspaces: {root: /var/lib/weft/workspaces}
auth:
  tokens:
    - {name: ci, role: operator, tokenEnv: WEFT_CI_TOKEN}
    - {name: grafana, role: viewer, tokenFile: /run/secrets/viewer-token}
  oidc: {issuer: "https://sso.example.com/realms/eng", audience: weft, roleClaim: weft_role}
runtimes: {sharedSecretEnv: WEFT_RUNTIME_SECRET}
observability: {otlpEndpoint: "http://otel-collector.observability:4318", traceSampleRatio: 0.2}
engine: {leaseTTL: 30s, reconcileInterval: 5s, maxConcurrentTurns: 64}
```

## Built-in preview gateway

```yaml
preview:
  addr: 127.0.0.1:8082
  baseUrl: http://localhost:8082
  consoleOrigins: [http://localhost:8080]
  relayBinary: bin/weft-preview-linux
  containerLocal: false
  tlsCert: ""
  tlsKey: ""
```

An empty `addr` disables managed review tools and the listener. `baseUrl` is the
address reachable by the human, with a review-specific subdomain added by Weft.
`consoleOrigins` must include the exact browser origin (scheme, host and port).
Remote URLs require HTTPS and `tlsCert`/`tlsKey`. `containerLocal: true` permits a
container listener on all interfaces only with a localhost base URL; the operator
must publish its host port on loopback. Bundled Compose does this explicitly.
Environment overrides: `WEFT_PREVIEW_ADDR`, `WEFT_PREVIEW_BASE_URL`,
`WEFT_PREVIEW_RELAY`, `WEFT_PREVIEW_CONTAINER_LOCAL`.
See [preview operations](sandbox-runtime.md#human-review-and-application-previews).
