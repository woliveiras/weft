# ENV-003 historical evidence

## Historical rollout

These results were recorded at the time of the run. The documentation migration did not rerun them.

- 2026-11-11. SQLite replay and missing-archive recovery, Linux fanotify and Chromium journal checks were recorded passing. Unresolved paths were coalesced in 10-second windows; queue overflows remained individual records.
- 2026-11-11. Local upgrade preserved the 67,108,856-byte legacy segment with inode and SHA-256 and advanced the PostgreSQL cursor across the rotation link. Disposable tests exercised verified pruning and stale restored anchors.
- 2026-11-11. Cached rollout c0902f5 and ddc8737 recreated weftd and sandbox-daemon with existing volumes. SHA-256 matched the archive and the 12,471 previously written active bytes. Both services remained healthy with zero restarts for two minutes. Readiness passed for storage, queue and sandbox.
- 2026-11-11. Prometheus evaluated all five journal rules and scraped 38.6 GiB available, an advancing collector heartbeat and zero write/rotation failure timestamps. Rules were inactive after rollout. Historical promtool tests recorded firing/clearing and healthy rotation with no importer samples.

## Independent review correction

- 2026-11-11. The concurrent prune integration initially failed with a stale
  retention checkpoint. After the correction, a SQLite importer reloaded the
  cursor after the concurrent prune, preserved the existing observations and
  imported a new record exactly once.
- 2026-11-11. The concurrent prune, inconsistent JSON anchor and same-size
  archive replacement tests passed 20 repetitions with the cached Go runner.
  The journal, app and weftd package suites passed.
- 2026-11-11. Cached Prometheus 3.1.0 passed `promtool check rules alerts.yaml`
  and `promtool test rules alerts_test.yaml`. The latter distinguished a zero
  or stale heartbeat from scrape and filesystem monitoring failures.
