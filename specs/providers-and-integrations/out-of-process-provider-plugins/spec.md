---
id: INT-003
title: "Out-of-process Provider Plugins"
summary: "Preserve the Provider Protocol v1 design while external plugin execution remains deferred."
status: deferred
scope: ["out of process provider plugins"]
navigation: ["specs/README.md","specs/providers-and-integrations/out-of-process-provider-plugins/matrix.md"]
documentation: ["docs/provider-protocol.md","docs/extensibility.md"]
dependencies: ["INT-001","INT-002"]
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Out-of-process Provider Plugins

Preserve the Provider Protocol v1 design while external plugin execution remains deferred.

## Behavior and invariants

- A future plugin host validates package identity, checksum, manifest and conformance before explicit administrator activation. Failed validation leaves the plugin unavailable.

## Acceptance criteria

### AC-001 — Reviewed package and activation

A future plugin host validates package identity, checksum, manifest and conformance before explicit administrator activation. Failed validation leaves the plugin unavailable.

### AC-002 — Process and credential boundary

A future plugin receives only its reviewed configuration and own credential through the protocol. Access to installation data and other credentials is excluded; inference remains gateway-mediated.

## Explicit exclusions

- The built-in plugin manager does not implement a third-party process host. No installation or activation of external provider code is authorized.

## Open decisions

- Revisit the proposed Bedrock dependency and reference adapter only if the maintainer reopens PROV-06.

## History

- 2026-10-30. The two-layer model separated built-in manifest/activation from out-of-process plugins.
- 2026-11-11. PROV-05 design was delivered. The maintainer deferred PROV-06 due to third-party code risk in d101200; earlier progress still listed design as pending.

## Reconciliation

- [provider-protocol.md](../../../docs/provider-protocol.md), [extensibility.md](../../../docs/extensibility.md).
- [Behavior and oracle matrix](./matrix.md).
