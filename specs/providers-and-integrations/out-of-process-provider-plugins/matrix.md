# INT-003 behavior and oracle matrix

Verification paths identify existing checks unless marked pending. Historical runs are scoped in the evidence record.

| Criterion | Scenario | Invariant | Observable oracle | Oracle provenance | Verification | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| [AC-001](./spec.md#ac-001--reviewed-package-and-activation) | Invalid package or incompatible protocol | Reviewed package and activation | A future plugin host validates package identity, checksum, manifest and conformance before explicit administrator activation. Failed validation leaves the plugin unavailable. | spec-derived (Provider Protocol v1, Package and manifest and Enabling a plugin) | Pending plugin host implementation and integration, [server/internal/llm/conformance/builtin_test.go](../../../server/internal/llm/conformance/builtin_test.go) | Design only |
| [AC-002](./spec.md#ac-002--process-and-credential-boundary) | Plugin start, timeout or requested access beyond its scope | Process and credential boundary | A future plugin receives only its reviewed configuration and own credential through the protocol. Access to installation data and other credentials is excluded; inference remains gateway-mediated. | spec-derived (Provider Protocol v1, Trust model) | Pending process boundary and failure-recovery verification | Design only |
