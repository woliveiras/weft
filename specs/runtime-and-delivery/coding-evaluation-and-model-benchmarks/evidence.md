# RUN-004 historical evidence

These results were recorded at the time of the run. The documentation migration did not rerun them.

- 2026-11-10 baseline. llama3.2 solved 0/4 in 40 s, with 45 tool calls and 36 errors. qwen3.6:27b-q4_K_M solved 3/4 in 1 h 03 min, with 23 calls and five errors. A later 27B attempt froze the 24 GiB development machine; local comparisons resumed with the 3B model.
- 2026-11-11 local progress. llama3.2 remained 0/4 after context/edit/plan changes and bounded output (13 calls, eight errors), then argument coercion (seven calls, two errors). Small-model guidance stayed at 0/4 and remains opt-in. Factory v2 solved 4/4 scripted and 0/4 with llama3.2 (six calls, five errors).
- 2026-11-11 hosted four-case Gemini baseline. Single agent 4/4, 49 calls, zero errors, 315,085 input / 6,985 output tokens, 2 min 04 s. Factory 4/4, 105 calls, zero errors, 855,075 input / 30,519 output tokens, 6 min 02 s.
- 2026-11-11 hosted nine-case Gemini benchmark, two repetitions. Single agent 17/18 observed, 296 calls, one error, 5,359,690 input / 121,618 output tokens, 17 min 59 s. Factory 18/18, 683 calls, three errors, 11,425,054 input / 319,205 output tokens, 45 min 29 s. The single-agent miss was attributed to a harness helper-name collision. [Per-case data](../../../docs/native-coding-runtime.md#harder-cases-repeated-2026-11-11-gemini-35-flash).
