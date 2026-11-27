# Bundled Go agents with Eino

The `summarize` agent reads one document through the execution's Weft tool gateway. It calls the selected node model and returns a `summary.proposed` message carrying the original input ID. The example workflow holds that message for an operator decision. [Example workflow](../../examples/graphs/eino-summary.yaml) · [Runtime Protocol v1](../../docs/runtime-protocol.md)

The bundled service also dispatches the software factory’s planner, researcher, developer, reviewer and QA roles. Compose exposes this service to the controller at `http://agents:8000`. Kubernetes uses `http://weft-agents:8000`.

Run `make build` to build the Linux binaries using Docker. For a host binary, run `go build -o weft-eino-agent ./cmd/weft-eino-agent` from this module. A Weft process-isolated node can launch that binary by absolute path in `runtime.command` with `runtime.endpoint` omitted. The controller supplies its callback origin, a listening port and the configured runtime secret.

For a separately managed service, build the image with `docker build -t weft/eino-agent:pilot agents/eino` from the repository root. Set `WEFT_URL` to the internal HTTP(S) origin of the controller reachable from that service. For multiple controllers, supply comma-separated origins. If the installation uses a shared runtime secret, supply the matching `WEFT_RUNTIME_SECRET` through its existing secret mechanism. The example expects the private service name `eino-agent` and port 8000. A host process binds to loopback by default. `GET /v1/health` lists compiled agents.

Save the example graph, place a text file in its workspace at `docs/input.md`, and start it. Inject a `summary.requested` message on `requests` with `{"path":"docs/input.md"}`. Inspect the pending summary, then reject it or approve it and inspect the message history of `accepted`. Model calls and file reads are accounted by Weft. The agent needs network access to the originating controller and receives its callback capability per turn.

Run `go test -race ./...` from this module. From the repository root, `./scripts/go.sh test -count=1 ./internal/api -run TestEinoSummaryThroughController` exercises the process binary, SQLite, filesystem broker, gateway and approval decisions together.

The same binary serves `reviewer` for `code.ready` batches. Its `analyse` node reads the listed files and requests a model verdict. Its `decide` node routes the original task payload to `review.approved` or `review.changes_requested`. Empty, missing or incomplete files and TODOs require changes. A failed provider request fails the turn. A completed model response without a usable verdict uses the file heuristic.
