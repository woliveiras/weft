# RUN-003 historical evidence

These results were recorded at the time of the run. The documentation migration did not rerun them.

- 2026-11-11. Scripted Factory v2 solved the original four cases. The local 3B run stopped after agents failed to emit the next workflow message. [Benchmark record](../coding-evaluation-and-model-benchmarks/evidence.md).

- 2026-11-16, AB1 / AC-003. `./scripts/go.sh test ./internal/tools ./internal/runtime/native ./internal/eval -run 'TestRestrictedShell|TestScriptedAgentRecovers' -count=1` passed in Docker. Real gateway/process execution recovered after one denial; scripted model evidence does not qualify Gemini recovery.
