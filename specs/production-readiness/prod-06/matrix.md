# PROD-06 behavior and oracle matrix

| Criterion | Scenario | Invariant | Observable oracle | Oracle provenance | Planned verification | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| AC-001 | The production installation starts with the chosen managed key service. | Managed key | Weft can use encrypted credentials with access limited to authorized server components. The local key file is no longer required in production. | independent (backlog and operating contracts, before new implementation) | Unit key-service contract tests; authorized store and key-service integration. | Pending |
| AC-002 | The key is rotated or the key service becomes unavailable. | Rotation and recovery | The documented procedure is tested against existing encrypted data and states how to recover. Failures are visible without exposing key material. | independent (backlog and operating contracts, before new implementation) | Unit error and redaction checks; authorized disposable rotation and restore drill. | Pending |
