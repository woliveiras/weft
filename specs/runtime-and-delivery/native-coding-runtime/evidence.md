# RUN-003 historical evidence

These results were recorded at the time of the run. The documentation migration did not rerun them.

- 2026-11-11. Scripted Factory v2 solved the original four cases. The local 3B run stopped after agents failed to emit the next workflow message. [Benchmark record](../coding-evaluation-and-model-benchmarks/evidence.md).

- 2026-11-16, AB1 / AC-003. `./scripts/go.sh test ./internal/tools ./internal/runtime/native ./internal/eval -run 'TestRestrictedShell|TestScriptedAgentRecovers' -count=1` passed in Docker. Real gateway/process execution recovered after one denial; scripted model evidence does not qualify Gemini recovery.

- 2026-11-16, AB2 / AC-004. Native `blocked` requires a summary and ends the execution as failed without publishing queued output. Unit tests cover the signal and missing summary; `internal/api/task_outcome_test.go` exercises the actual model/runtime/engine/task/trigger path. The event contract has positive/negative schema tests and validates the emitted integration event. Existing small-context runtime tests remain unchanged and pass. AB1 recovery is scripted, not a live Gemini qualification.
