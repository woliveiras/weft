---
id: INT-002
title: "Model Provider Adapters and Conformance"
summary: "Apply a shared provider contract and distinguish adapter delivery from live qualification."
status: in-progress
scope: ["model provider adapters and conformance"]
navigation: ["specs/README.md","specs/providers-and-integrations/model-provider-adapters-and-conformance/matrix.md"]
documentation: ["docs/extensibility.md","docs/provider-langgraph-validation.md","docs/cost-control.md"]
dependencies: ["INT-001"]
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Model Provider Adapters and Conformance

Apply a shared provider contract and distinguish adapter delivery from live qualification.

## Behavior and invariants

- Built-in providers support the applicable chat, model listing, structured response and tool exchanges. Cancellation, timeouts and errors retain the shared gateway contract.

## Acceptance criteria

### AC-001 — Shared provider boundary

Built-in providers support the applicable chat, model listing, structured response and tool exchanges. Cancellation, timeouts and errors retain the shared gateway contract.

### AC-002 — Usage and provider-specific fields

Optional cache-read/write usage preserves earlier fields. The adapter reports provider usage where available and preserves round-trip metadata required for the corresponding tool result.

### AC-003 — Qualification scope

Fixture conformance remains repeatable without real credentials. Explicit live opt-in records the specific endpoint, model and exercised behavior separately.

## Explicit exclusions

- An adapter or compatible hosted endpoint result does not qualify Vertex ADC or a selected Bedrock profile. Streaming presentation belongs to RUN-003.

## Open decisions

- The local-first external list still places Gemini in future work. PROV-03 and cea5042 record the delivered adapter; live Vertex remains pending. Bedrock PROV-02 is open.

## History

- 2026-10-30. The local-first plan selected opt-in Ollama and repeatable fixtures.
- 2026-11-11. Ollama conformance had passed with llama3.2; Gemini native and Vertex configuration were delivered in the built-in adapter.

## Reconciliation

- [extensibility.md](../../../docs/extensibility.md), [provider-langgraph-validation.md](../../../docs/provider-langgraph-validation.md), [cost-control.md](../../../docs/cost-control.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
