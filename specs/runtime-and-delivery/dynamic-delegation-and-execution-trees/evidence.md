# RUN-002 historical evidence

These results were recorded at the time of the run. The documentation migration did not rerun them.

- 2026-10-18. Foundation/profile/token checkpoints recorded 257, 263 and 274 frontend tests and targeted race checks. Initial workspace-review and graph-drag failures passed reruns. Disposable tests exercised concurrent reservations, interruption, recovery and expiry.
- 2026-10-18. Local Ollama and PostgreSQL activation exercised tree admission and cancellation. The recovery rollout (historical label acafb69) preserved four volumes, five graph versions and schema versions 1–3; two settled ledgers retained 904 and 4,966 tokens with zero holds and stable post-recovery revisions.
- 2026-10-18. The installed recovery check qualified closure and idempotency for settled ledgers. Induced outstanding-reservation recovery and the 30-day compaction boundary were covered in automated disposable fixtures; the installed data had no eligible compaction record.
- 2026-10-19. LOCAL-03b used the production native launcher and a restored SQLite profile. Reconciliation survived a second restart and preserved another worker's reservations. The run used no inference or billing.
