# Weft developer tasks. Standard Go targets use Docker via scripts/go.sh.
# build-native and test-native-browser use the installed host Go toolchain.
# Python and Node/pnpm run locally for their corresponding suites.

GO        := ./scripts/go.sh
COMPOSE   := docker compose
PYTHON    ?= python3
VERSION   ?= dev
NAMESPACE ?= weft
GRAPH     ?= examples/graphs/software-factory.yaml
# Hosted OpenAI-compatible endpoint for test-llm-cloud and eval-cloud; the
# key is WEFT_CLOUD_API_KEY in the local .env (never committed, never echoed).
CLOUD_URL   ?= https://generativelanguage.googleapis.com/v1beta/openai/
CLOUD_MODEL ?= gemini-3.5-flash

.PHONY: egress-image test-egress test-environment-policy help build test test-compose test-go test-python test-web test-sandbox test-sandbox-capacity test-environment-context lint fmt up down logs images k8s-render clean

help: ## Show targets
	@grep -E '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'

build: ## Build weftd and weft binaries into server/bin
	$(GO) build -o bin/weftd ./cmd/weftd
	$(GO) build -o bin/weft ./cmd/weft
	$(GO) build -o bin/weft-egress ./cmd/weft-egress
	GOOS=linux CGO_ENABLED=0 $(GO) build -o bin/weft-preview-linux ./cmd/weft-preview
	GOOS=linux CGO_ENABLED=0 $(GO) build -o bin/weft-workspace-journal-linux ./cmd/weft-workspace-journal

egress-image: ## Build the managed proxy image inside the dedicated sandbox daemon
	./scripts/build-egress-image.sh

test-egress: ## Real runtime/build/setup HTTP/HTTPS allowlist and isolation checks on the dedicated daemon
	./scripts/test-egress.sh

test-environment-policy: ## Installation policy network checks on the dedicated daemon (cached alpine:3.22 required)
	./scripts/test-environment-policy.sh

test: test-go test-python test-web ## Run all test suites

test-go: ## Go unit + integration tests (memory/fs/sqlite; live brokers via WEFT_TEST_* env)
	$(GO) test ./...

test-python: ## SDK and example agent tests (stdlib unittest)
	cd sdk/python && $(PYTHON) -m unittest discover -s tests -v
	PYTHONPATH=sdk/python:agents $(PYTHON) -m unittest discover -s agents/tests -v
	$(PYTHON) -m unittest discover -s scripts/tests -v

test-web: ## Type-check and run the web console tests (Vitest, jsdom)
	cd web && pnpm install --frozen-lockfile && pnpm exec tsc --noEmit && pnpm test

test-compose: ## Reviewed Compose services on the dedicated daemon (cached alpine:3.22 required)
	./scripts/test-compose-runtime.sh

test-sandbox: ## Sandbox runtime isolation checks against the Compose stack (needs Docker; see docs/sandbox-runtime.md)
	./scripts/test-sandbox-runtime.sh

test-sandbox-capacity: ## Real sandbox capacity/cgroup checks (running dedicated daemon and local Go required)
	./scripts/test-sandbox-capacity.sh

test-environment-context: ## Real COPY/ADD sandbox checks (running dedicated daemon and local Go required)
	./scripts/test-environment-context.sh

lint: ## gofmt check and go vet
	@out=$$(docker run --rm -v "$$(pwd)/server:/src" -w /src golang:1.26-alpine gofmt -l .); \
	  if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	$(GO) vet ./...

fmt: ## Format Go code
	docker run --rm -v "$$(pwd)/server:/src" -w /src golang:1.26-alpine gofmt -w .

up: ## Start the full stack (platform + observability)
	$(COMPOSE) up -d --build

down: ## Stop the stack (volumes are kept; use `docker compose down -v` to wipe)
	$(COMPOSE) down

logs: ## Follow control-plane logs
	$(COMPOSE) logs -f weftd

images: ## Build all container images
	docker build --build-arg VERSION=$(VERSION) -t weft/weftd:$(VERSION) .
	docker build -f agents/Dockerfile -t weft/agents:$(VERSION) .
	docker build -f Dockerfile.sandbox -t weft/sandbox:$(VERSION) .   # outside Compose; Compose builds it inside sandbox-daemon

k8s-render: ## Render platform manifests (kustomize) and agent runtimes for GRAPH
	kubectl kustomize deploy/kubernetes
	@echo "---"
	$(GO) run ./cmd/weft k8s render -f ../$(GRAPH) --namespace $(NAMESPACE) || true

clean: ## Remove build outputs
	rm -rf server/bin web/dist

.PHONY: test-e2e
test-e2e: ## Chromium browser journey against a disposable backend (Go, Node, Git, Playwright Chromium required)
	cd server && go test -tags e2e ./cmd/weft-e2e
	cd web && pnpm run test:e2e

.PHONY: test-e2e-real
test-e2e-real: ## Browser journey with shell.exec on the running dedicated sandbox daemon
	cd server && go test -tags e2e ./cmd/weft-e2e
	cd web && pnpm run test:e2e:real

.PHONY: eval-local
eval-local: ## Measure the native coding runtime with local Ollama models (EVAL_MODELS=llama3.2:latest,... EVAL_GUIDANCE=small EVAL_WORKFLOW=factory)
	WEFT_EVAL_OLLAMA_URL=$${WEFT_EVAL_OLLAMA_URL:-http://host.docker.internal:11434} WEFT_EVAL_MODELS=$${EVAL_MODELS:-llama3.2:latest} WEFT_EVAL_GUIDANCE=$(EVAL_GUIDANCE) WEFT_EVAL_WORKFLOW=$(EVAL_WORKFLOW) \
		WEFT_EVAL_REPORT=/repo/server/eval-report.md $(GO) test ./internal/eval -run TestLocalModels -count=1 -timeout 4h -v

.PHONY: test-llm-cloud
test-llm-cloud: ## Live provider conformance against CLOUD_URL/CLOUD_MODEL (key: WEFT_CLOUD_API_KEY in .env)
	@set -a; . ./.env; set +a; test -n "$$WEFT_CLOUD_API_KEY" || { echo "set WEFT_CLOUD_API_KEY in .env"; exit 2; }; \
		WEFT_TEST_OPENAI_URL="$(CLOUD_URL)" WEFT_TEST_OPENAI_MODEL="$(CLOUD_MODEL)" WEFT_TEST_OPENAI_KEY="$$WEFT_CLOUD_API_KEY" \
		$(GO) test ./internal/llm/conformance -run TestOpenAICompatibleLive -count=1 -v

.PHONY: test-llm-gemini
test-llm-gemini: ## Live conformance of the native Gemini plugin against the Gemini API with CLOUD_MODEL (key: WEFT_CLOUD_API_KEY in .env)
	@set -a; . ./.env; set +a; test -n "$$WEFT_CLOUD_API_KEY" || { echo "set WEFT_CLOUD_API_KEY in .env"; exit 2; }; \
		WEFT_TEST_GEMINI_MODEL="$(CLOUD_MODEL)" WEFT_TEST_GEMINI_KEY="$$WEFT_CLOUD_API_KEY" \
		$(GO) test ./internal/llm/conformance -run TestGeminiLive -count=1 -v

.PHONY: eval-cloud
eval-cloud: ## Measure the native coding runtime with hosted models (CLOUD_MODEL=a,b EVAL_WORKFLOW=factory; key: WEFT_CLOUD_API_KEY in .env)
	@set -a; . ./.env; set +a; test -n "$$WEFT_CLOUD_API_KEY" || { echo "set WEFT_CLOUD_API_KEY in .env"; exit 2; }; \
		WEFT_EVAL_OPENAI_URL="$(CLOUD_URL)" WEFT_EVAL_OPENAI_KEY="$$WEFT_CLOUD_API_KEY" WEFT_EVAL_MODELS="$(CLOUD_MODEL)" \
		WEFT_EVAL_GUIDANCE="$(EVAL_GUIDANCE)" WEFT_EVAL_WORKFLOW="$(EVAL_WORKFLOW)" WEFT_EVAL_REPORT=/repo/server/eval-report.md \
		$(GO) test ./internal/eval -run TestCloudModels -count=1 -timeout 4h -v

.PHONY: test-e2e-providers
test-e2e-providers: ## Real LangGraph + browser provider journey, no Docker (set WEFT_E2E_PYTHON)
	@test -n "$(WEFT_E2E_PYTHON)" || (echo "Set WEFT_E2E_PYTHON to Python with the langgraph extra installed"; exit 1)
	PYTHONPATH=sdk/python "$(WEFT_E2E_PYTHON)" -m unittest discover -s scripts/provider-e2e -v
	cd server && go test -tags e2e ./cmd/weft-provider-e2e
	cd web && pnpm run test:e2e:providers

.PHONY: test-preview
test-preview: ## Real sandbox preview gateway, HTTP client and lifecycle checks
	./scripts/test-preview.sh

.PHONY: test-workspace-journal
test-workspace-journal: ## Kernel process/transient/offline journal tests in the dedicated Linux daemon
	./scripts/test-workspace-journal.sh

.PHONY: build-native
build-native: ## Build the current host's native bundle without Docker; does not initialize or start it
	./scripts/build-native.sh

.PHONY: test-native-browser
test-native-browser: ## Chromium against a disposable production native profile; no Docker required
	./scripts/test-native-browser.sh

.PHONY: test-native-upgrade
test-native-upgrade: ## Disposable format-1 native binary upgrade and safe failure qualification (host Go/Git/Python)
	$(PYTHON) scripts/test-native-upgrade.py
