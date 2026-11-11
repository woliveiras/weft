package engine

import (
	"fmt"
	"strings"

	"github.com/woliveiras/weft/server/internal/llm"
)

// toolExamples are argument examples for small models (NAT-08): one valid
// call per tool, with the JSON types the schema expects.
var toolExamples = map[string]string{
	"filesystem.read":        `{"path":"<file>","offset":1,"limit":80}`,
	"filesystem.list":        `{"path":".","recursive":true}`,
	"filesystem.search":      `{"pattern":"<name>","path":"."}`,
	"filesystem.stat":        `{"path":"<file>"}`,
	"code.map":               `{"path":"."}`,
	"code.outline":           `{"path":"<file>"}`,
	"filesystem.write":       `{"path":"<new file>","content":"<whole file text>"}`,
	"filesystem.edit":        `{"path":"<file>","old":"<exact current text>","new":"<replacement>"}`,
	"filesystem.multi_edit":  `{"path":"<file>","edits":[{"old":"<exact text>","new":"<replacement>"},{"old":"<old name>","new":"<new name>","replaceAll":true}]}`,
	"filesystem.apply_patch": `{"patch":"--- a/<file>\n+++ b/<file>\n@@ -3 +3 @@\n-<old line>\n+<new line>\n"}`,
	"filesystem.move":        `{"from":"<file>","to":"<new path>"}`,
	"shell.exec":             `{"argv":["go","test","./..."]}`,
	"check.run":              `{"name":"test"}`,
	"git.status":             `{}`,
	"git.diff":               `{}`,
	"plan.update":            `{"items":[{"step":"<step 1>","status":"done"},{"step":"<step 2>","status":"in_progress"},{"step":"<step 3>","status":"pending"}]}`,
	"finish":                 `{"summary":"<what changed and which tests pass>"}`,
}

// smallModelGuidance is the system-prompt section for providers with
// ModelGuidance "small": strict tool-call rules and examples for the tools
// this agent was actually given.
func smallModelGuidance(tools []llm.ToolSchema) string {
	var b strings.Builder
	b.WriteString("\n## How to call tools\n")
	b.WriteString("- Call exactly one tool per reply, through the tool-calling interface. Do not write tool calls as text.\n")
	b.WriteString("- Arguments are one JSON object. Write booleans and numbers without quotes and lists as JSON arrays.\n")
	b.WriteString("- Paths are relative to the workspace root. Use only paths you saw in filesystem.list or filesystem.search results; never guess a file name.\n")
	b.WriteString("- Read a file before you edit it, and copy the text to replace exactly.\n")
	b.WriteString("- If a call fails, read the error, fix the arguments and try again. Do not give up after one error.\n")
	b.WriteString("- Keep going until the change is made and the tests pass; then call `finish`.\n")
	names := map[string]bool{"plan.update": true, "finish": true}
	for _, t := range tools {
		names[t.Name] = true
	}
	var lines []string
	for _, name := range []string{"code.map", "code.outline", "filesystem.read", "filesystem.list", "filesystem.search", "filesystem.stat", "filesystem.edit", "filesystem.multi_edit",
		"filesystem.apply_patch", "filesystem.write", "filesystem.move", "check.run", "shell.exec", "git.status", "git.diff", "plan.update", "finish"} {
		if ex, ok := toolExamples[name]; ok && names[name] {
			lines = append(lines, fmt.Sprintf("- %s %s", name, ex))
		}
	}
	if len(lines) > 0 {
		b.WriteString("Call shapes (replace every <...> with real values):\n" + strings.Join(lines, "\n") + "\n")
	}
	return b.String()
}
