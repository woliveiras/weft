# TASK-007 historical evidence

These results were recorded at the time of the run. The documentation migration did not rerun them.

- 2026-10-19. Admission passed backend, frontend, lint/build and 83 main browser cases. Structured ingress passed unit/engine/API and five browser cases. FLOW-08 later recorded 332 frontend tests plus Go/Python and 93 main Chromium cases, one hosted CIMD case and one isolated provider case.

- 2026-11-16, AB2/AB4 / AC-004, AC-005. Unit tests cover outcome classification, historical-copy preservation, invalid ownership and storage failure. API integration covers blocked execution, successful execution without required delivery, persisted terminal task state, stopped graph and existing rejection of graph mutations. The existing engine cycle settles newly marked task copies; no new reconciler or historical migration was introduced.
- Console verification: Node 22 Bookworm with the existing `leforge-web-node-modules` volume passed `pnpm typecheck`, E2E TypeScript compilation and all 466 Vitest tests (88 files). Playwright passed 29 selected cases in `task-outcome`, `task-journey`, `task-drafts`, `task-authorization`, `triggers` and `ux-feedback`. The cached E2E runner supplied Chromium and libraries, with the Node 22 Bookworm binary mounted; no browser/base image was downloaded. The authorization fixture now tests narrowing while genuinely active, before approving completion. Workflow lists/selectors hide execution copies and direct graph navigation links to the task.
