# Provider selection through an Eino agent

Run `make test-e2e-providers` from the checkout with the host Go toolchain, pnpm, the console dependencies and Playwright Chromium installed. Use `WEFT_PROVIDER_E2E_PORT` to change the fixture controller's loopback port (default 18102). The tests create a disposable installation and use artificial credentials.

The browser configures and tests an OpenAI-compatible provider, selects it for a graph node and starts the graph. Another provider remains the installation default. Weft sends the execution to a separate Go process running a compiled Eino graph. The agent sends its prompt to the model callback without selecting a model or receiving a provider key. The controller resolves the selected provider, makes the authenticated request to the fixture HTTP server and charges the execution once. The agent returns the model's answer as its summary. The fixture also checks refusal of a missing provider before execution.

On a simulated upstream HTTP 503 the agent receives a generic model callback failure. The controller records the upstream status and retryability in the execution's events. The tests check the model selection on the wire, failure propagation, usage and credential redaction in API responses, persisted settings, events, runtime input, output and logs. The loopback harness omits callback tokens from its disposable turn audit. The deterministic provider exercises protocol behavior with no external vendor account.

The [original LangGraph qualification](provider-langgraph-validation.md) records the earlier Python implementation. The current fixtures live in the [Eino agent module](../agents/eino/cmd/provider-fixture), the [controller harness](../server/cmd/weft-provider-e2e) and the [browser suite](../web/e2e-providers).
