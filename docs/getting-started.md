# Getting started

This guide takes you from a clone to a running agent factory, then to real
models, your own graph, and running without Docker.

## Console navigation

Use the [console navigation guide](interface-navigation.md) to find Tasks,
Projects, Workflows, Models, Connections and Administration. Configure agents
and their explicit grants, save the workflow, then start it and inspect Runs.

## 1. Start the platform

Requirements: Docker Engine (or Docker Desktop / Colima / Rancher Desktop /
Podman with the compose plugin) and about 3 GB of free memory for the full
stack. On macOS, see [Colima setup](#macos-setup-with-colima).

For a **new installation only**, create private bootstrap credentials. The
no-clobber subshell refuses to overwrite an existing `.env`; reuse the existing
secret set for upgrades. Never regenerate installation credentials as a repair
step. Review the [initial identity contract](operations.md#initial-identity-and-credentials)
before exposing the installation remotely.

```bash
(
  set -C
  umask 077
  for name in WEFT_ADMIN_TOKEN WEFT_RUNTIME_SECRET POSTGRES_PASSWORD GRAFANA_ADMIN_PASSWORD; do
    value=$(openssl rand -hex 32) || exit 1
    printf '%s=%s\n' "$name" "$value"
  done > .env
) && docker compose up -d --build
docker compose ps              # weftd, agents, nats, postgres, sandbox-daemon, otel-collector, prometheus, tempo, loki, grafana
```

| URL | What |
|---|---|
| <http://localhost:8080> | Web console and API (use `WEFT_ADMIN_TOKEN` from `.env`) |
| <http://localhost:3000> | Grafana (anonymous viewer; admin password is in `.env`) |
| <http://localhost:9090> | Prometheus |

The compose stack runs **PostgreSQL** for storage and **NATS JetStream** for
queues and loads every graph under `examples/graphs/` without starting them
(start some with `WEFT_BOOTSTRAP_START` once a model provider is usable;
see [Use a real model](#3-use-a-real-model)). The four generated
credentials are required. Keep `.env` private and never commit it. Optional
settings can be added to the same file:

```dotenv
WEFT_DEV_MOCK=true
WEFT_BOOTSTRAP_START=hello-echo
```

To run the lighter stack without observability: `docker compose up -d weftd`
(it pulls in `nats`, `postgres` and `agents`). Add `sandbox-daemon sandbox-image`
to that command when agents should run commands in containers.

`sandbox-daemon` is a Docker daemon dedicated to Weft: agents with docker
isolation run their commands in containers there, never on your machine's
Docker. `sandbox-image` builds the default sandbox image inside it on every
`up` and then exits (status `Exited (0)` is expected). The console shows its
state under **Administration → Health**. See
[sandbox runtime](sandbox-runtime.md).

### Prepare a project environment

In **Graph settings → Environment**, choose **Discover environment**. Weft
checks its optional YAML definition, devcontainer, Dockerfiles and Compose in
that order. Select and review the source (and Compose development service),
accept it into the draft, save and build. Invalid higher-priority definitions
must be corrected or explicitly replaced; they do not silently fall through.
When nothing is found, **Analyze repository** creates an initial proposal for
administrator review in **Approvals**. Approval permits build; commands only run
when ready. No `.weft/environment.yaml` is required.

Compatible tasks reuse prepared images and cache seeds while retaining separate
workspaces and writable caches. Setup runs per task. The agent panel explains
readiness, reuse, build failures and capacity waiting. Use **Administration** for
network policy, resource cleanup preview and confirmation. Reset affects a stopped
graph's environment and preserves its workspace and shared resources needed by
other tasks. See [environments and troubleshooting](environments.md), the
[mise example](../examples/projects/mise-node/README.md), and the
[validated scope](sandbox-acceptance.md).

## 2. Watch work move

Configure a [real model or the explicit development mock](#3-use-a-real-model)
before starting Software Factory. Bootstrap imports workflows in a stopped state.
The echo workflow needs no model.

```bash
set -a; . ./.env; set +a
alias weft='docker compose exec -e WEFT_TOKEN="$WEFT_ADMIN_TOKEN" weftd weft'

weft graphs
weft start hello-echo
weft status hello-echo
weft start software-factory
weft inject software-factory requests feature.request \
  '{"title":"slugify","description":"Add a slugify(text) helper. It lowercases text and joins words with dashes."}'
weft events software-factory --follow        # Ctrl-C to stop
weft approvals
weft approve <approval-id> --comment "ship it"
weft messages software-factory --channel release
```

In the console, the canvas shows replica states, queue depth on each edge,
and approval badges; click a node for its prompt, executions, live output and
operator chat; click an edge for its message history and the inject form.

The software factory's agents write real files into the graph workspace and
commit them:

```bash
docker compose exec weftd sh -c 'cd /var/lib/weft/workspaces/software-factory && git log --oneline && ls src tests plans'
```

## 3. Use a real model

Model providers are **inference plugins**. Every plugin (Ollama, OpenAI,
Anthropic, OpenRouter, vLLM, LM Studio, Text Generation Inference) is installed
with Weft but starts **disabled**; an administrator enables the ones the
installation may use and then configures one or more providers from each.
There are no provider environment variables or configuration-file entries.
Until a provider is usable, **New task** explains how to enable one.

For development without any model, start the server with
`WEFT_DEV_MOCK=true` (or `llm.devMock: true`): it adds the deterministic,
offline `mock` plugin with a read-only `mock` provider. Every agent handles it
(the Python agents fall back to heuristics).

### From the browser

1. Open **Models** (admin token required). The
   **Inference plugins** list shows each plugin, whether it is local or
   hosted, what it supports (tool calls, JSON mode, usage, model listing) and
   which providers use it. **Enable** the plugin you want.
2. Choose **Add provider**, pick the enabled plugin, enter the base URL if it
   is not the default, the default model and the API key when the plugin
   needs one, then **Test settings**: the test lists models and sends one
   short prompt through Weft, so a green result means agents can actually
   use it.
3. Save, optionally **Make default**, and pick the provider per agent in the
   graph editor (agent panel → *Provider* / *Model*). Nodes can also pin one
   with `model: {provider: <name>, name: <model>}`.

The key is encrypted on the server and never shown again; leave the field
empty when editing to keep it. See
[installation key](configuration.md#installation-key) for backups and
rotation. **Disabling** a plugin keeps its providers, but they report
unavailable (and agents using them fail with that reason) until it is enabled
again.

The URL must be reachable from the `weftd` process: with Compose a model
server on your machine is `http://host.docker.internal:<port>` (see
[Ollama installed on the host](#ollama-installed-on-the-host) for Linux and
listen-address notes), and the bundled Ollama service is `http://ollama:11434`;
a native installation (section 5) reaches `http://localhost:11434` directly.

### Push and open a pull request

Add a source control connection under **Connections → Source control**
(GitHub: repository `owner/name` and a fine-grained token with *Contents* and
*Pull requests* write access; the token stays encrypted on the server). When
you create a task, choose it as the **Source control hand-off**. After the
workflow's human gate, the result agent (in the Software Factory, *Review
handoff*) commits on the branch `weft/task-<id>`, pushes it and opens a pull
request into the repository's default branch, which waits for a person's
approval unless the connection relaxes it. The task shows the pushed branch,
the pull request link and any failure (a rejected push, a missing permission,
a refused approval). Agents never run an SCM CLI or see the token.

### Start tasks from labelled issues

In a saved workflow, open **Graph settings → Issue triggers**. Select the
tracker connection, agents and delivery connection. The default selection
label is `weft:todo`. Saving records your consent. **Preview matching
issues** reads the queue. Readiness checks happen before work starts.
Incomplete issues move to `weft:needs-attention` with a question on the
issue, and the hold appears in Activity and the trigger. A claim applies
`weft:wip`. An open PR changes it to `weft:done` for human review.
[Issue-trigger setup and lifecycle](task-workflows.md#issue-trigger-readiness-and-delivery)

### Prices and budgets

**Models → Costs** shows this month's spend by provider and model, user,
project, workflow and task run. Administrators enter prices per million tokens
(Weft ships none; local plugins such as Ollama cost nothing unless priced),
a budget for every task run, and monthly budgets for the installation, a user,
a project or a workflow. New tasks show their applicable budgets. A reached
monthly budget can block a task from starting. During a run, a call that cannot
reserve enough budget is refused before dispatch and the run pauses. The task
shows the reason. An administrator can raise the blocking budget in **Models →
Costs**. See [budget enforcement](cost-control.md#enforcement) for admission,
pause and resume behavior.

### What a model needs to run agents

Agents with tools (shell, git, files) need two things from a model. **Test
connection** in **Integrations** checks both:

1. **Tool calling.** The model must return tool calls in the provider's
   structured format. Some models only write the call as JSON text (for
   example code-tuned Qwen 2.5 variants on Ollama). The test then says
   *"Inference works, but not tool calling"*. Weft runs a call written as
   text when it names an offered tool, but such models are unreliable for long
   tasks; prefer models whose test passes.
2. **A large enough context window.** A turn sends the whole conversation
   (task, tool results, file contents) on every model call. Weft asks
   Ollama for 32768 tokens (`num_ctx`) by default — Ollama's own default is
   4096 and it silently drops the beginning of longer conversations, task
   included. Change it per provider (**Context window** under *Limits*). Larger
   windows need more memory on the machine running the model.

When something goes wrong, the agent's turn fails with the reason, visible in
**Events** (`execution.failed`) and the agent's **Recent turns**:

| Turn error | Meaning | What to do |
|---|---|---|
| `prompt exceeds the model context window` | The conversation no longer fits the provider's context window | Raise the provider's context window, or give the agent a smaller task |
| `the model wrote a tool call as text instead of calling the tool` | The model answered with JSON that is not a usable call | Use a model whose connection test passes tool calling |
| `the model ended the turn without an answer or a tool call` | Empty answer, nothing done | Check the model and the prompt; this often follows a truncated context |
| `the model's answer was cut off by its output limit` | The answer hit the output token limit | Use a model or provider with a larger output limit |
| `model call timed out: … gave no answer within 5m0s` | The model server did not answer within the provider's timeout — typical for a large local model that is still loading or a machine short of memory (heavy swapping) | Use a smaller model, close memory-hungry apps, or raise the provider's timeout (**Timeout** under *Limits*) |
| `the turn used every model round without finishing` | The agent kept calling tools until its budget ran out | Raise **Max tool calls** in the agent's *Limits per turn*, or split the task |

### Ollama (local, offline after the first pull)

```bash
docker compose --profile ollama up -d ollama
docker compose exec ollama ollama pull llama3.2
```

Enable the **Ollama** plugin and add a provider with base URL
`http://ollama:11434` and default model `llama3.2`. Pick a model with
tool-calling support for native agents (e.g. `llama3.1`, `qwen2.5`).

### Ollama installed on the host

If Ollama already runs natively on your machine, use it instead of the
container: it can use the host's GPU and is not limited by the memory given to
Docker.

1. Make Ollama listen on an address containers can reach. By default it only
   accepts connections from `127.0.0.1`; set `OLLAMA_HOST=0.0.0.0` where Ollama
   is started (for `ollama serve`, the systemd unit, or the desktop app's
   environment):

   ```bash
   OLLAMA_HOST=0.0.0.0 ollama serve
   ```

   This exposes port 11434 to your network while it runs, without
   authentication. Restrict it with a firewall on shared or public networks.

2. Pull a model:

   ```bash
   ollama pull llama3.2
   ```

3. Enable the **Ollama** plugin and add a provider with base URL
   `http://host.docker.internal:11434` (Compose) or `http://localhost:11434`
   (native installation).

`host.docker.internal` resolves out of the box on Docker Desktop and Colima.
With Docker Engine on Linux, map it to the host gateway in a
`docker-compose.override.yml` next to `docker-compose.yml` (Compose merges it
automatically):

```yaml
services:
  weftd:
    extra_hosts: ["host.docker.internal:host-gateway"]
```

Weft sends the context window (`num_ctx`, default 32768) with every
request, so `OLLAMA_CONTEXT_LENGTH` on the Ollama side is not needed; change it
with the provider's **Context window**.

Check the connection with **Test connection** on the Ollama provider in
**Integrations**: it sends a real prompt, so success means agents can use the
model. A `no such host` error means the URL still points at the bundled
service (`http://ollama:11434`); `connection refused` usually means Ollama is
still listening only on `127.0.0.1`.

### HuggingFace models (Text Generation Inference)

```bash
HF_MODEL_ID=Qwen/Qwen2.5-0.5B-Instruct docker compose --profile hf up -d tgi
```

Enable the **Hugging Face TGI** plugin and add a provider with base URL
`http://tgi:80/v1`. Weights are cached in the `hf-models` volume; set
`HF_HUB_OFFLINE=1` once they are present.

### vLLM, LM Studio, OpenAI, Anthropic, OpenRouter

Enable the matching plugin and add a provider with its base URL, default model
and (for hosted APIs) API key. Keys are read by the platform only; agents
never see them.

## 4. Write your first graph

```yaml
# my-first-graph.yaml
apiVersion: weft.io/v1
kind: Graph
metadata: {name: Summariser}
spec:
  parameters:
    - {name: style, value: "three bullet points"}
  nodes:
    - id: summariser
      prompt: Summarise each document from @input as @style and hand the summary to @output.
      tools: [filesystem.read]
    - id: archivist
      prompt: Append every summary from @input to summaries.md in @workspace.
      tools: [{name: filesystem.write, paths: ["summaries.md"]}, filesystem.read]
  channels:
    - {id: docs, to: [summariser]}              # ingress: inject work here
    - {id: summaries, from: summariser, to: [archivist]}
```

Defaults fill in the rest: nodes with inputs wait for work (`loop.mode: event`),
nodes without inputs run once, runtime is `native`, one replica, process
isolation, shared workspace. Then:

```bash
weft validate -f my-first-graph.yaml     # errors and warnings, locally and on the server
weft apply -f my-first-graph.yaml
weft start summariser
weft inject summariser docs document '{"text":"..."}'
```

(With the compose alias, copy the file into the container first or use
`curl -X POST -H 'Content-Type: application/yaml' --data-binary @my-first-graph.yaml`.)

### Build and configure a graph in the browser

Everything above can be done from the console instead of YAML:

1. **Graphs → New graph**, keep or adjust the template, **Create**. The graph
   opens on the **Graph** tab.
2. **Graph settings** (toolbar) edits graph-wide values:
   - **Name** and **Description**;
   - **Repository URL**: a remote repository (`https://…`, `ssh://…` or
     `user@host:path`) cloned into the graph workspace the first time the
     graph starts, while the workspace is empty. Changing it later does not
     replace an existing workspace. Local paths and `file://` are refused, so a
     graph cannot copy another graph's workspace or the server's files.
     Private repositories need credentials, which are not supported yet;
   - **Parameters**: values used in prompts as `@name`;
   - **Max hops**: how long a chain of agent-to-agent messages may get.
3. **Add agent**, then select it to configure it in **Agent configuration**:
   name, runtime, provider and model, prompt (**Edit → Apply to draft**), loop
   and stop policy, replicas, **Command sandbox** (see below), **Limits per
   turn**, approval gates, and tools in **Tools**.
4. Drag from one agent's right handle to another's left handle to add a
   channel, then select it to set its type, message types, route condition
   and **Require approval before delivery**. A channel with approval and no consumer is a
   convenient "notify me" endpoint: its messages wait in **Approvals**.
5. **Save graph**. Problems are listed above the canvas and next to the field
   that caused them; fix them and they clear on their own.
6. **Run all** (or **Start** on one agent) and follow **Live output**,
   **Messages**, **Events** and **Approvals**.

**Limits per turn** bound one agent turn: **Turn timeout** (default `30m`),
**Max tool calls** (default 50) and **Token budget** (unlimited by default). A
turn that exceeds one fails and its input is redelivered, so raise them for
long tasks such as dependency upgrades (for example `2h` and 300 calls).

### Dry-run a topology for free

Set `runtime: {kind: echo}` (per node or in `spec.defaults`) to run the whole
machinery — queues, routing, clusters, approvals, persistence, tracing —
without calling a model. Echo agents forward each input to their outputs, and
source nodes emit a `tick` per turn. `config: {fail: true}` makes a node fail
(to test retries and dead-lettering); `config: {stopAfter: N}` exercises the
auto-stop handshake. See `examples/graphs/hello-echo.yaml`.

### Run agent commands in containers

Agents that hold `shell.exec` or the git tools should run their commands in a
container: select the agent, open **Agent configuration → Command sandbox**,
set **Isolation** to `docker`, and choose **Network** `none` (default) or
`bridge` when it must download dependencies. Commands then run on
`sandbox-daemon`, can reach the internet only when bridged, and never reach
Weft's services or your network. Details, reset and troubleshooting:
[sandbox runtime](sandbox-runtime.md).

### Prompt tokens

| Token | Meaning |
|---|---|
| `@input` | the messages claimed this turn (listed in the system preamble) |
| `@output` | the node's outgoing channels; hand work on with the `emit` tool |
| `@workspace` / `@project` | the graph workspace (a git repository when `spec.workspace.git`) |
| `@loop` | the replica's private scratch folder (`.weft/<node>/r<n>`) |
| `@<parameter>` | substituted with the parameter's value each turn |

## 5. Run without Docker

You need Go 1.26+, Python 3.10+, git and (for the console) Node 22.22.2+
within the Node 22 line. For development aligned with the container versions,
use Go 1.26, Python 3.12 and Node 22; see the
[asdf or mise setup instructions](../CONTRIBUTING.md#local-toolchain-versions).

```bash
# console (optional)
(cd web && pnpm install --frozen-lockfile && pnpm run build)

# control plane: SQLite + filesystem queues + mock provider, no services needed
cd server
go build -o bin/weftd ./cmd/weftd && go build -o bin/weft ./cmd/weft
WEFT_ADMIN_TOKEN="$(openssl rand -hex 32)" \
WEFT_WEB_DIR=../web/dist \
WEFT_BOOTSTRAP_GRAPHS=../examples/graphs \
WEFT_BOOTSTRAP_START=hello-echo \
WEFT_LOG_FORMAT=text \
./bin/weftd
```

Data goes to `server/data/` (`weft.db`, `queues/`, `workspaces/`).
For local-only experiments `WEFT_AUTH_DISABLED=true` turns authentication off
(never on a shared host).

Python agents run as a separate process:

```bash
PYTHONPATH=sdk/python:agents PORT=8000 python3 -m weft_agents
```

The example graph addresses them as `http://agents:8000`; when running locally,
change `spec.defaults.runtime.endpoint` to `http://127.0.0.1:8000` (or add
`127.0.0.1 agents` to `/etc/hosts`).

`scripts/go.sh` runs the Go toolchain in a container if you prefer not to
install Go (it produces Linux binaries, suitable for containers and CI).

## macOS setup with Colima

If you already have Docker with Compose working, skip to the
[platform startup](#1-start-the-platform). Otherwise, Colima can run the Docker
engine in a Linux VM on your Mac, without Docker Desktop.

With [Homebrew](https://brew.sh/) installed, install Colima and the Docker
client and plugins:

```bash
brew install colima docker docker-compose docker-buildx

mkdir -p ~/.docker/cli-plugins
ln -s "$(brew --prefix)/opt/docker-compose/bin/docker-compose" \
  ~/.docker/cli-plugins/docker-compose
ln -s "$(brew --prefix)/opt/docker-buildx/bin/docker-buildx" \
  ~/.docker/cli-plugins/docker-buildx
```

If a plugin link already exists, check it before replacing it.

On Apple Silicon with macOS 13 or newer, start the VM with Apple's
virtualization framework and VirtioFS file sharing:

```bash
colima start --runtime docker --vm-type vz --mount-type virtiofs \
  --cpus 4 --memory 8 --disk 100
```

The 4 CPUs, 8 GiB of RAM and 100 GiB virtual disk are suggested starting
settings for Weft, not minimum requirements. Adjust them to your machine;
local models need additional resources. On other supported Macs, use
`colima start --runtime docker --cpus 4 --memory 8 --disk 100`.

Verify the engine, plugins and a test container:

```bash
colima status
docker info
docker compose version
docker buildx version
docker run --rm hello-world
```

Initial setup and image downloads need internet access. Use `colima stop`
to stop the VM and `colima start` to restart it with its saved settings.
Then follow [platform startup](#1-start-the-platform) to create Weft's `.env`
and start the platform.
See the official [installation](https://colima.run/docs/installation/) and
[configuration](https://colima.run/docs/configuration/) guides for details.

## Next steps

- [Architecture](architecture.md) to understand the engine, queues and runtimes.
- [Extensibility](extensibility.md) to write an agent in any language or framework.
- [Sandbox runtime](sandbox-runtime.md) to run agent commands in containers and reset them.
- [Project environments](environments.md) to give agents your project's toolchain (Go, Node, Python, ...).
- [Deployment](deployment.md) and [production hardening](production-hardening.md) before going live.
