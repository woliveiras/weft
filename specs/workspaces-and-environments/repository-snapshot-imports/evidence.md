# ENV-001 historical evidence

These results were recorded at the time of the run. The documentation migration did not rerun them.

- 2026-10-19. PROMPT-03 recorded 343 Vitest cases and 18 import/projects/drafts Chromium journeys. PROMPT-04 recorded 345 Vitest cases, four import Chromium journeys, nine disposable native journeys and passing targeted race checks. The fixtures used real Git/subprocess boundaries and synthetic credentials.
- 2026-11-11. The local-first check notes known repository-import and API failures in the recorded container environment. No current result was established in this migration.

## PROD-11 local verification

On 2026-11-11, the package and `TestRepositoryImportAPIPermissionsAndPersistence`
failed with a cached Alpine Git image missing `git-http-backend`. Commit
`f6dab53` installed its owning `git-daemon` package in the default test image.
On main, the complete package passed 10 uncached repetitions and the API test
passed 20 uncached repetitions. Both also passed in the uncached full Go suite.
[Commands and output](../../production-readiness/prod-11/evidence.md#local-verification-on-2026-11-11).
