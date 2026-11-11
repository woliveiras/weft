# PROD-12 behavior and oracle matrix

| Criterion | Scenario | Invariant | Observable oracle | Oracle provenance | Planned verification | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| AC-001 | CI builds a production artifact. | Artifact qualification | It uses pinned inputs, produces an SBOM, scans dependencies and images, and signs the image identified by digest. | independent (backlog and operating contracts, before new implementation) | Inspect build inputs and attestations and verify the signature independently. | Pending |
| AC-002 | An image fails signature verification or meets the agreed blocking scan policy. | Admission failure | It does not qualify for deployment. The failure is visible and any exception follows the approved policy. | independent (backlog and operating contracts, before new implementation) | Authorized disposable pipeline and admission checks for unsigned, changed and rejected artifacts. | Pending |
