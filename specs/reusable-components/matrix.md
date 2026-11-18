# Release 2 behavior and oracle matrix

| Criterion | Scenario | Invariant | Observable oracle | Provenance | Verification |
| --- | --- | --- | --- | --- | --- |
| AC-001 | Create, publish and browse agent versions with different roles | Versions are immutable and installation permissions apply | Admin can publish a new revision; stale writes and viewer writes leave the earlier revisions accessible | spec-derived | Unit manager and storage/API integration |
| AC-001 | Create an agent in a workflow and browse existing agents | Every saved agent can be reused; task copies are excluded | Catalog finds the new agent and preexisting inline agents without a separate save; creation can select either from a searchable picker | maintainer-confirmed | Catalog unit, API integration and browser walkthrough |
| AC-002 | Bind the same version twice with different values | Parameter resolution is local and grants stay bounded | Each node uses its own input; missing/unknown values or revision fail without altering saved graph; graph defaults grant no extra tools | spec-derived | Unit resolver and real graph API |
| AC-003 | Publish a version, inspect the diff and explicitly adopt it | Old bindings and history remain pinned | Saved and historical nodes retain the old configuration until a reviewed graph save | spec-derived | Real storage/API and browser journey |
| AC-003 | Open an agent and change instructions in one workflow | The local text is composed only for the selected node | Workflows tab links directly to configuration; two instances of one version receive different local text | maintainer-confirmed | Runtime integration and browser walkthrough |
