# Provider selection through the real Python LangGraph adapter

`make test-e2e-providers` verifies the managed-provider journey without a Docker
daemon, production installation, paid inference, or real provider credentials.
It uses the production web console, Go API/engine, SQLite, encrypted credential
storage, provider registry and HTTP client, runtime callback API, Python SDK,
`LangGraphAgent`, `weft_chat_model`, LangChain `BaseChatModel` and compiled
LangGraph `StateGraph`. Only the external model server is deterministic.

## Reproduce

From the repository root, with Go, Node, Git and Python 3.10+ available:

```sh
python3 -m venv /tmp/weft-provider-venv
/tmp/weft-provider-venv/bin/pip install './sdk/python[langgraph]'
cd web
pnpm install --frozen-lockfile
pnpm exec playwright install chromium
cd ..
export WEFT_E2E_PYTHON=/tmp/weft-provider-venv/bin/python
make test-e2e-providers
```

Dependencies and Chromium must be installed before offline execution. The
validation run used Python 3.15.0, LangGraph 1.2.15, langchain-core 1.6.11 and
Chromium supplied by Playwright 1.64.1. This is evidence for that combination,
not a compatibility claim for every version permitted by the SDK extras.

The controller/browser listens on loopback port **18102**. Set
`WEFT_PROVIDER_E2E_PORT` to another free port when coordinating concurrent
work. Model and Python runtime ports are allocated dynamically. Every harness
instance owns its temporary SQLite database, encryption key, workspace, runtime
process and audit files; shutdown removes only those temporary resources.
The dedicated Go command is compiled only with `-tags e2e`. It does not modify
production configuration. Its `/fixture` metadata endpoint requires its
artificial admin token and returns only the local model URL.

## Verified path

1. In Integrations, the browser creates an OpenAI-compatible provider with an
   artificial API key and the local model URL, tests the draft, saves it, and
   tests the saved provider (models, inference and structured tool calling).
2. The browser selects that provider in the agent editor, saves the graph and
   runs it. An unused mock provider remains the installation default, proving
   the explicit selection overrides it. The fixture supplies an external LangGraph runtime and once loop;
   provider selection and execution are browser actions.
3. The engine sends Runtime Protocol v1 `TurnInput`, including provider/model
   references and an execution-scoped callback capability, to the Python SDK.
   The runtime process environment is allowlisted; no provider key, provider
   URL or provider SDK is supplied to it.
4. A real compiled LangGraph node invokes `weft_chat_model(ctx)`. Its
   LangChain model converts messages and calls `ctx.llm.chat`, which uses the
   SDK `CallbackClient` and the execution's `/llm/chat` callback.
5. The gateway resolves the selected managed provider and model. The production
   OpenAI HTTP provider attaches the stored credential to its upstream request.
   The deterministic server requires that credential, parses the wire request,
   and returns `LANGGRAPH_GATEWAY_OK` for the runtime prompt.
6. That answer passes back through the callback, LangChain and LangGraph to the
   execution summary, which the browser observes.

There is no mocked browser request interception and no fake runtime callback.
The server's generated answer proves routing and protocol interoperability;
it does **not** prove real model quality or connectivity to any hosted vendor.
The tool-calling connection test proves the provider protocol check, not a
LangGraph tool-execution loop.

## Failure and credential evidence

- Focused Python tests use the real adapter with an isolated SDK backend to
  check message conversion, returned summary/usage and propagated failures.
- Go unit tests cover the deterministic model protocol and credential redaction.
  Real HTTP tests retain `APIError` status, retryability and `Retry-After`, and
  cover a credential crossing the error-body truncation boundary.
- The Go integration starts real Python and Go servers, configures/tests the
  managed provider, exercises success and an explicit failing model override,
  and verifies that the requested models reached the model server. A missing
  provider is rejected before execution.
- The deterministic server deliberately echoes artificial keys in 401/503
  errors. Assertions check API responses, persisted provider settings, runtime
  input/results/environment/logs and stored events for those keys. Error
  messages are redacted at the HTTP provider boundary before truncation, so
  failures remain failures without carrying credentials into the runtime.
- Chromium covers invalid URL validation, rejected credentials, failed provider
  inference and failed execution, and missing-provider preflight. The saved
  key field is empty when reopened; provider reads, execution/event reads,
  rendered content and local storage are checked for credential exposure.

The browser necessarily sends the operator-entered artificial key in create/test
requests. This is distinct from exposing a stored key back to the browser or
runtime. Failure traces can therefore contain the artificial input; never
substitute real credentials in this suite. Raw runtime audit files include
execution callback capabilities, remain inside the private temporary directory,
and are not returned by `/fixture` or committed.

These tests provide evidence for section 5's real LangGraph path and browser
configuration → provider selection → execution checklist items. They do not
cover MCPs, editor-wide behavior, Docker isolation, or external vendor inference.
