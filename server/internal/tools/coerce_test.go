package tools

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
)

const coerceSchema = `{"type":"object","properties":{
	"path":{"type":"string"},"replaceAll":{"type":"boolean"},"limit":{"type":"integer"},"ratio":{"type":"number"},
	"argv":{"type":"array","items":{"type":"string"}},"env":{"type":"object","properties":{"n":{"type":"integer"}}},
	"edits":{"type":"array","items":{"type":"object","properties":{"replaceAll":{"type":"boolean"}}}}}}`

func TestCoerceArgsConvertsUnambiguousValues(t *testing.T) {
	in := `{"path":["main.go"],"replaceAll":"False","limit":"30","ratio":"0.5","argv":"go test './a b'","env":"{\"n\":\"2\"}","edits":[{"replaceAll":"true"}],"extra":"kept"}`
	out, notes := CoerceArgs(json.RawMessage(coerceSchema), json.RawMessage(in))
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	want := `{"argv":["go","test","./a b"],"edits":[{"replaceAll":true}],"env":{"n":2},"extra":"kept","limit":30,"path":"main.go","ratio":0.5,"replaceAll":false}`
	if b, _ := json.Marshal(got); string(b) != want {
		t.Fatalf("%s\nwant %s", b, want)
	}
	if len(notes) != 8 || !strings.Contains(strings.Join(notes, "|"), `replaceAll: "False" → false`) {
		t.Fatalf("%v", notes)
	}
}

func TestCoerceArgsLeavesAmbiguousOrCorrectValuesAlone(t *testing.T) {
	for _, in := range []string{
		`{"replaceAll":"maybe","limit":"3.5","path":["a","b"],"argv":"go 'unterminated"}`,
		`{"replaceAll":true,"limit":3,"path":"a","argv":["go"]}`,
		`not json`,
	} {
		out, notes := CoerceArgs(json.RawMessage(coerceSchema), json.RawMessage(in))
		if string(out) != in || len(notes) != 0 {
			t.Fatalf("%s → %s %v", in, out, notes)
		}
	}
	if out, notes := CoerceArgs(nil, json.RawMessage(`{"a":"1"}`)); string(out) != `{"a":"1"}` || notes != nil {
		t.Fatal("no schema must mean no change")
	}
	// Large integers keep their exact value.
	if out, _ := CoerceArgs(json.RawMessage(coerceSchema), json.RawMessage(`{"limit":9007199254740993,"replaceAll":"true"}`)); !strings.Contains(string(out), "9007199254740993") {
		t.Fatalf("%s", out)
	}
}

func TestArgumentErrorsSayWhatTypeIsExpected(t *testing.T) {
	var v struct {
		Offset int      `json:"offset"`
		Argv   []string `json:"argv"`
		Flag   bool     `json:"flag"`
	}
	for args, want := range map[string]string{
		`{"offset":"x"}`: "offset must be an integer (without quotes), got a JSON string",
		`{"argv":"go"}`:  `argv must be an array of strings, e.g. ["go","test","./..."]`,
		`{"flag":1}`:     "flag must be a boolean (true or false, without quotes), got a JSON number",
		`{"flag":`:       "not valid JSON",
	} {
		err := decodeArgs(&Invocation{Args: json.RawMessage(args)}, &v)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: %v", args, err)
		}
	}
}

// Through the gateway: the tool runs with converted values, the model is
// told, and the audit records what ran.
func TestGatewayCoercesArgumentsBeforeRunning(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"a.txt": "x x\n"})
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, nil)
	p := principal(dir, domain.ToolGrant{Name: "filesystem.*"}, domain.ToolGrant{Name: "shell.exec"})
	r := run(t, gw, p, "filesystem.edit", map[string]any{"path": "a.txt", "old": "x", "new": "y", "replaceAll": "true"})
	if readFile(t, dir, "a.txt") != "y y\n" || !strings.Contains(r.Content, `replaceAll: "true" → true`) || !strings.Contains(r.Content, "Send these JSON types next time") {
		t.Fatalf("%q", r.Content)
	}
	r = run(t, gw, p, "shell.exec", map[string]any{"argv": "cat a.txt"})
	if !strings.Contains(r.Content, "y y") {
		t.Fatalf("argv string: %q", r.Content)
	}
	r, err := call(t, gw, p, "filesystem.read", map[string]any{"path": "a.txt", "offset": "first"})
	if err != nil && !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if !r.IsError || !strings.Contains(r.Content, "offset must be an integer") {
		t.Fatalf("%q", r.Content)
	}
}
