# Chat conversation evals

Run `make eval-chat` from the repository root. Each case starts its own Weft API with temporary SQLite storage and an in-memory queue, sends requests to the authenticated Chat endpoints and inspects their effects. The default model is scripted and works offline with the normal cached Go test dependencies. The deployed Chat installation is never a fixture.

| Case | Passing evidence |
| --- | --- |
| TaskFollowup | Two successful tasks followed by five failed tasks. Both turns read the requested status. The replies report each total and show the requested titles and IDs in last-update order, capped at three. |
| PullRequest | Two loopback repositories both contain PR 418. “o do mobile” prepares a review of the chosen head. No diff is read and no comments are published. |
| AgentCreation | The definition covers the requested release-review work. The catalog changes only on confirmation. Replaying confirmation leaves exactly one published version. |
| UnavailableRead | An unreadable SQLite task record makes the running-task query unavailable. The session retains a retryable diagnostic without reporting zero matches. |
| TaskLink | A viewer asks for a task link by ID and then asks for it again by reference. Both replies identify the same verified task. A missing ID returns no link, even when another task has that ID as its title. |

## Run the same cases with your Codex subscription

On macOS or Linux, install the Codex CLI, sign in using ChatGPT and check `codex login status`. The CLI must support `exec --ephemeral --ignore-user-config --output-schema`. Git and either Go (the version in `server/go.mod`) or Docker are required. Run from the repository root:

```sh
make eval-chat-codex
CHAT_EVAL_CASE=TaskFollowup make eval-chat-codex
```

Set `CHAT_EVAL_MODEL` to a model available to your plan to pin it across runs. The default uses the CLI's built-in model selection. Each run consumes Codex plan usage. The offline target and normal test suite have no inference cost. Neither target uses Gemini or reads the deployed model settings.

The test binary runs on the host even when Docker compiles it. Each inference round starts an ephemeral CLI process in an empty temporary working directory, with shell tools, web search, plugins and subagents disabled. The CLI receives the same Weft system instructions, conversation messages, tool descriptions and tool results. Codex returns text or proposed tool calls via structured output. Weft validates arguments, queries local services and owns every effect. This measures conversational decisions through the CLI adapter. Provider-specific function-call transport and token accounting need separate provider conformance tests.

Use verbose output to distinguish a bad tool choice, a wrong final answer and an unavailable CLI. A model change can regress an individual case. Keep the failing run when comparing versions and record the CLI version, model, case and pass count across repeats. The CLI must identify its login as ChatGPT; a missing login or model error fails the run. [Official OpenAI documentation for CLI authentication](https://learn.chatgpt.com/docs/auth)

## Add a case

1. Add a `TestChatEval<Name>` function in `server/internal/api/chat_eval_<area>_test.go` using the `api_test` package. Start it with `newChatEval(t, scripted)`. That callback describes an offline provider response. Use `evalTool` for tool calls and build any final answer from the observation the API actually sent back.
2. Seed deterministic records in the case's temporary installation. For remote collaborators use a loopback HTTP fixture. Keep credentials synthetic. Give rows fixed timestamps when order matters.
3. Submit each operator turn with `e.ask`, reusing the returned session ID for follow-ups. Check the actual reply, the new entries in `e.calls()`, and independently known state. For writes, compare before confirmation, after confirmation, and after replay. For lists, include a distractor and more matches than the display limit.
4. Keep the assertions outside the scripted callback so they apply to real inference as well. For custom grading logic include a negative example that represents the regression. Run a single case with `./scripts/go.sh test -v -count=1 ./internal/api -run '^TestChatEvalTaskFollowup$'`. Verbose output identifies the operator turn, tool names and arguments, and the saved reply. Standard Go test exit status signals failure.
