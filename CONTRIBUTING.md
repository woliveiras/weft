# Contributing to Weft

Thanks for helping. Weft is a vendor-neutral platform; contributions that add
coupling to a specific cloud, model vendor or proprietary runtime will not be
merged into core — put them behind one of the extension interfaces instead
(see [docs/extensibility.md](docs/extensibility.md)).

## Development setup

Install Git and Make. Docker with Compose is required for the container workflow:
Go runs in a container through `scripts/go.sh`, and Compose starts the full stack.
Console tests use the Dockerfile's Node.js/pnpm toolchain. These targets
do not skip when their required tools are missing.

### Local toolchain versions

Use the following version lines to match the project's existing containers:

| Tool | Development version | Source |
| --- | --- | --- |
| Go | Latest patch in 1.26 | [server/go.mod](server/go.mod) requires 1.26.0; [Dockerfile](Dockerfile) and [scripts/go.sh](scripts/go.sh) use `golang:1.26-alpine`. |
| Node.js | Latest patch in 22, at least 22.22.2 | [Dockerfile](Dockerfile) uses `node:22-alpine`; dependencies in [web/pnpm-lock.yaml](web/pnpm-lock.yaml) require 22.22.2 or newer within Node 22. |
| pnpm | 10.33.4 (pinned in `packageManager`) | Use `pnpm install --frozen-lockfile` with the committed web lockfile. |

Choose either asdf or mise below, or install these tools directly. A version
manager is optional. Run the setup commands from the repository root in Bash or
Zsh. They select versions for this checkout, not global defaults. The repository
does not currently commit a version-manager configuration; these commands create
one locally and resolve exact patch versions at setup time. Keep personal
`.tool-versions` or `mise.toml` files out of unrelated commits. The source files
above remain authoritative when updating versions.

### Option A: asdf

Follow the [asdf installation and shell setup guide](https://asdf-vm.com/guide/getting-started)
first, including adding its shims directory to `PATH`. These commands use the
current `asdf set` syntax (asdf 0.16+).

Add each plugin once; omit any already listed by `asdf plugin list`:

```bash
asdf plugin add golang https://github.com/asdf-community/asdf-golang.git
asdf plugin add nodejs https://github.com/asdf-vm/asdf-nodejs.git
```

```bash
asdf set golang latest:1.26
asdf set nodejs latest:22
asdf install
asdf current
```

[`asdf set`](https://asdf-vm.com/manage/versions.html) records the resolved
versions in `.tool-versions`. Subsequent `asdf install` calls reuse those versions.

### Option B: mise

Follow the [mise installation guide](https://mise.jdx.dev/installing-mise.html)
and [shell setup](https://mise.jdx.dev/shells.html) first. For example, add
`eval "$(mise activate zsh)"` to `~/.zshrc`, or `eval "$(mise activate bash)"`
to `~/.bashrc`, then open a new shell.

```bash
mise use --pin go@1.26 node@22
mise current
```

[`mise use --pin`](https://mise.jdx.dev/cli/use.html) installs the tools and
records exact versions in `mise.toml`. Subsequent `mise install` calls reuse
those versions. If you have not activated mise in your shell, run commands with
`mise exec --`, for example `mise exec -- make test`.

Install the pinned package manager with `corepack enable && corepack install --global pnpm@10.33.4`
(or `mise use --pin pnpm@10.33.4`).

### Verify and develop

From the repository root, check that your shell resolves the selected tools:

```bash
go version
node --version
pnpm --version
git --version
make --version
docker --version
docker compose version
```

Start Docker before running the container-backed targets:

```bash
make build        # weftd + weft into server/bin
make test         # Go controller and agent tests, web type-check and Vitest tests
make lint         # gofmt + go vet
make check        # lint, Go/web tests and the pre-push hook regression
make up           # full stack via docker compose
```

`make build`, `make test-go` and `make lint` still use Docker even when Go is
installed locally. Go tests build a cached toolchain image from
`scripts/Dockerfile.go-test`, adding Git for real repository/worktree integration
tests. The first build needs access to Alpine packages; `GO_IMAGE` overrides
must remain Alpine-based. Git is installed inside that development image, not
on the host or in a project sandbox. For native Go development, run `go test ./...`, `go vet ./...`
and `go build ./...` from `server/`; see
[running without Docker](docs/getting-started.md#5-run-without-docker) for native
binaries and startup instructions. `make test-web` builds and runs the `web-source` Docker image. For local
console checks, use the commands in the [web console guide](web/README.md).
Run `go test -race ./...` in `agents/eino/` for the agent and SDK suites.

The shared SQLite API integration fixture validates its entire persisted log
against `schemas/event.schema.json` at teardown, including dynamically named
events. Other fixtures validate their logs when they use `eventtest.Capture`
or `eventtest.ValidateStored`. Store conformance tests may use synthetic event
types. The Go suite also checks event names against the schema and the
documentation. For an
optional capture to inspect or validate separately, use the native Go toolchain. From the repository root:

```sh
capture=$(mktemp -d)
(cd server && WEFT_TEST_EVENT_CAPTURE="$capture" go test -count=1 ./internal/api ./internal/cost ./internal/engine)
(cd server && go run ./cmd/check-event-contract "$capture")
```

For the console development server:

```bash
cd web
pnpm install --frozen-lockfile
pnpm run dev:mock  # fixture-backed UI, no backend required
# or: pnpm run dev  # proxies API requests to the backend on port 8080
```

Live broker/database suites are skipped unless their environment variables are
set: `WEFT_TEST_NATS_URL`, `WEFT_TEST_REDIS_URL`,
`WEFT_TEST_RABBITMQ_URL`, `WEFT_TEST_KAFKA_BROKERS`,
`WEFT_TEST_POSTGRES_DSN`.

### Direct pushes to main

Enable the versioned hook once in each clone with the following command.

```bash
git config --local core.hooksPath .githooks
```

If the clone already has custom hooks, integrate
[`pre-push`](.githooks/pre-push) with that setup. For an outgoing update to
`main`, the hook runs `make check` in a temporary copy of the outgoing commit
stored under the clone's Git directory so Docker can mount it.
Commit the intended changes first. Uncommitted and untracked files stay in
your checkout. A failed check stops the push, and you can run `make check`
directly while developing. The check defaults to serial Go packages, and
the containerized web tests use two Vitest workers.

GitHub Actions runs `make check`, `make build` and `make test-e2e-smoke` after
each push to `main`. It reports against the commit that arrived and keeps
the Playwright report, failure screenshots and traces for seven days when
produced. Both local and GitHub checks run without a minimum coverage
percentage. The [web guide](web/README.md#browser-smoke) shows how to run
the browser smoke locally.

## Ground rules

- **Contracts first.** [OpenAPI](api/openapi.yaml), [JSON Schemas](schemas/),
  [Runtime Protocol](docs/runtime-protocol.md), and the JSON tags in
  `server/internal/domain`, `runtime`, `llm`, `tools` define the wire contracts.
  Additive changes only; anything else needs an ADR in `docs/adr/`.
- **Adapters pass conformance.** A new broker must pass
  `queue/conformance`; a new store must pass `storage/conformance`.
- **Metric names are contracts.** Add metrics, never rename them
  (`server/internal/observability/metrics.go`).
- **Security defaults stay closed.** Tool grants are default-deny; new tools
  declare a capability and enforce their own constraints (paths, commands,
  hosts) through the helpers in `server/internal/tools/policy.go`.
- **Tests** accompany every behaviour change. Engine behaviour is tested
  end-to-end in `server/internal/engine/engine_test.go`.
- Keep the local hook enabled for pushes to `main`. Keep commits focused and
  describe the *why* in the message.

## Architecture decisions

Significant decisions are recorded as ADRs in `docs/adr/`. Propose a new one
(next number, status "Proposed") in the same pull request as the change.

## License

By contributing you agree that your contributions are licensed under the
Apache License 2.0.
