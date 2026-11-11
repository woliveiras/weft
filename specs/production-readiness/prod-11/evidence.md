# PROD-11 historical verification notes

- 2026-10-17. The UX checkpoint observed an intermittent sandbox journal
  supervision failure in a full run and one isolated repetition. A diagnostic
  repetition and the final full run passed. The cause remained open.
- 2026-11-11. The local-first record used the Docker Go wrapper and reported
  known repository-import and API failures. Browser checks used a local image
  with Playwright 1.64.1, Go 1.26 and Git, and recorded the MCP authentication
  failure on main. The recorded Node 22 setup used a separate dependency volume
  and the public registry after the corporate registry responded with HTTP 403.
- These environment notes describe the historical runs. Current CI
  qualification and any dependency acquisition remain pending.
