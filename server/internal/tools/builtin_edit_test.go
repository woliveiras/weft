package tools

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
)

const goFile = "package calc\n\nfunc Add(a, b int) int {\n\treturn a - b\n}\n\nfunc Sub(a, b int) int {\n\treturn a - b\n}\n"

func TestSnippetNotFoundExplainsTheClosestText(t *testing.T) {
	for name, tc := range map[string]struct{ old, want string }{
		"indentation": {"func Add(a, b int) int {\n    return a - b\n}", "indentation differs (tabs in the file)"},
		"trailing":    {"func Add(a, b int) int {  \n\treturn a - b\n}", "trailing whitespace differs"},
		"changed":     {"func Add(a, b int) int {\n\treturn a + c\n}", `snippet line 2 differs: file has "return a - b"`},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := applySnippet(goFile, snippetEdit{Old: tc.old, New: "x"})
			if err == nil || !strings.Contains(err.Error(), "Closest text is lines 3-5") || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "4\t\treturn a - b") {
				t.Fatalf("%v", err)
			}
		})
	}
	_, _, err := applySnippet(goFile, snippetEdit{Old: "completely unrelated words here", New: "x"})
	if err == nil || !strings.Contains(err.Error(), "no similar text") {
		t.Fatalf("%v", err)
	}
	_, _, err = applySnippet(goFile, snippetEdit{Old: "\treturn a - b", New: "x"})
	if err == nil || !strings.Contains(err.Error(), "occurs 2 times (first at line 4)") {
		t.Fatalf("%v", err)
	}
	if _, _, err := applySnippet(goFile, snippetEdit{New: "x"}); err == nil {
		t.Fatal("empty old must fail")
	}
}

func TestSnippetMatchesCRLFFiles(t *testing.T) {
	crlf := strings.ReplaceAll(goFile, "\n", "\r\n")
	out, n, err := applySnippet(crlf, snippetEdit{Old: "func Add(a, b int) int {\n\treturn a - b", New: "func Add(a, b int) int {\n\treturn a + b"})
	if err != nil || n != 1 || !strings.Contains(out, "return a + b\r\n") || strings.Count(out, "\r\n") != strings.Count(crlf, "\r\n") {
		t.Fatalf("%v %q", err, out)
	}
}

func TestParsePatchRefusesWhatItCannotApply(t *testing.T) {
	for name, tc := range map[string]struct{ patch, want string }{
		"delete":  {"--- a/x.go\n+++ /dev/null\n@@ -1 +0,0 @@\n-x\n", "use filesystem.delete"},
		"rename":  {"--- a/x.go\n+++ b/y.go\n@@ -1 +1 @@\n-x\n+y\n", "use filesystem.move"},
		"prose":   {"Here is the fix.", "no file headers"},
		"trailer": {"--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-x\n+y\nThis fixes it.\n", "not part of a unified diff"},
		"nohunks": {"--- a/x.go\n+++ b/x.go\n", "has no @@ hunks"},
		"none":    {"", "no file headers"},
		"orphan":  {"@@ -1 +1 @@\n-x\n+y\n", "hunk before any"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parsePatch(tc.patch); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%v", err)
			}
		})
	}
}

func TestApplyHunksIgnoresWrongCountsAndFindsShiftedContext(t *testing.T) {
	// Header line numbers and counts are wrong; the context decides.
	patch := "--- a/calc.go\n+++ b/calc.go\n@@ -1,99 +1,99 @@\n func Add(a, b int) int {\n-\treturn a - b\n+\treturn a + b\n }\n@@ -40 +40 @@\n func Sub(a, b int) int {\n-\treturn a - b\n+\treturn a - b // ok\n }\n"
	files, err := parsePatch(patch)
	if err != nil {
		t.Fatal(err)
	}
	out, add, del, err := applyHunks(goFile, files[0])
	if err != nil || add != 2 || del != 2 {
		t.Fatalf("%v +%d -%d", err, add, del)
	}
	if !strings.Contains(out, "func Add(a, b int) int {\n\treturn a + b\n}") || !strings.Contains(out, "return a - b // ok\n}\n") {
		t.Fatalf("%q", out)
	}
}

func TestApplyHunksKeepsTheEndOfFile(t *testing.T) {
	patch := "--- a/f.txt\n+++ b/f.txt\n@@ -1,2 +1,2 @@\n a\n-b\n+c\n\\ No newline at end of file\n"
	files, err := parsePatch(patch)
	if err != nil {
		t.Fatal(err)
	}
	if out, _, _, err := applyHunks("a\nb\n", files[0]); err != nil || out != "a\nc" {
		t.Fatalf("%v %q", err, out)
	}
	files, _ = parsePatch("--- a/f.txt\n+++ b/f.txt\n@@ -1,2 +1,2 @@\n a\n-b\n+c\n")
	if out, _, _, err := applyHunks("a\r\nb\r\n", files[0]); err != nil || out != "a\r\nc\r\n" {
		t.Fatalf("crlf: %v %q", err, out)
	}
	files, _ = parsePatch("--- a/f.txt\n+++ b/f.txt\n@@ -1,2 +1,2 @@\n a\n-b\n+c\n")
	if _, _, _, err := applyHunks("a\nzzz\n", files[0]); err == nil || !strings.Contains(err.Error(), "hunk 1") {
		t.Fatalf("mismatch: %v", err)
	}
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for p, c := range files {
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readFile(t *testing.T, dir, p string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, p))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The tests below run through the real gateway, grants and filesystem.

func TestEditToolsThroughTheGateway(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"src/calc.go": goFile, "src/util.go": "package calc\n\nconst X = 1\n"})
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, nil)
	p := principal(dir, domain.ToolGrant{Name: "filesystem.write", Paths: []string{"src/**"}})

	r, _ := call(t, gw, p, "filesystem.edit", map[string]any{"path": "src/calc.go", "old": "func Add(a, b int) int {\n    return a - b\n}", "new": "x"})
	if !r.IsError || !strings.Contains(r.Content, "indentation differs") || readFile(t, dir, "src/calc.go") != goFile {
		t.Fatalf("edit miss: %q", r.Content)
	}

	run(t, gw, p, "filesystem.multi_edit", map[string]any{"path": "src/calc.go", "edits": []map[string]any{
		{"old": "func Add(a, b int) int {\n\treturn a - b", "new": "func Add(a, b int) int {\n\treturn a + b"},
		{"old": "package calc", "new": "// Package calc adds.\npackage calc"},
	}})
	edited := readFile(t, dir, "src/calc.go")
	if !strings.Contains(edited, "return a + b") || !strings.HasPrefix(edited, "// Package calc adds.") {
		t.Fatalf("%q", edited)
	}
	r, _ = call(t, gw, p, "filesystem.multi_edit", map[string]any{"path": "src/calc.go", "edits": []map[string]any{
		{"old": "return a + b", "new": "return b + a"},
		{"old": "not there at all, nothing similar", "new": "y"},
	}})
	if !r.IsError || !strings.Contains(r.Content, "edit 2 of 2") || !strings.Contains(r.Content, "no edit was applied") || readFile(t, dir, "src/calc.go") != edited {
		t.Fatalf("multi_edit must be atomic: %q", r.Content)
	}

	res := run(t, gw, p, "filesystem.apply_patch", map[string]any{"patch": "diff --git a/src/util.go b/src/util.go\nindex 1..2 100644\n--- a/src/util.go\n+++ b/src/util.go\n@@ -3 +3 @@\n-const X = 1\n+const X = 2\n--- /dev/null\n+++ b/src/new/extra.go\n@@ -0,0 +1,2 @@\n+package new\n+\n"})
	if !strings.Contains(res.Content, "patched 2 file(s): src/util.go (+1 -1), src/new/extra.go (new, +2 -0)") {
		t.Fatalf("%q", res.Content)
	}
	if readFile(t, dir, "src/util.go") != "package calc\n\nconst X = 2\n" || readFile(t, dir, "src/new/extra.go") != "package new\n\n" {
		t.Fatal("patch not applied")
	}
}

func TestApplyPatchIsAllOrNothing(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"src/a.txt": "one\ntwo\n", "src/b.txt": "three\n", "docs/c.txt": "c\n"})
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, nil)
	p := principal(dir, domain.ToolGrant{Name: "filesystem.write", Paths: []string{"src/**"}})
	changeA := "--- a/src/a.txt\n+++ b/src/a.txt\n@@ -1,2 +1,2 @@\n one\n-two\n+TWO\n"

	for name, tc := range map[string]struct{ patch, want string }{
		"second hunk fails": {changeA + "--- a/src/b.txt\n+++ b/src/b.txt\n@@ -1 +1 @@\n-four\n+FOUR\n", "does not match src/b.txt"},
		"outside the grant": {changeA + "--- a/docs/c.txt\n+++ b/docs/c.txt\n@@ -1 +1 @@\n-c\n+C\n", "docs/c.txt"},
		"inside .git":       {changeA + "--- /dev/null\n+++ b/src/.git/hooks/pre-commit\n@@ -0,0 +1 @@\n+boom\n", ".git directory"},
		"creates existing":  {changeA + "--- /dev/null\n+++ b/src/b.txt\n@@ -0,0 +1 @@\n+x\n", "already exists"},
		"duplicate file":    {changeA + changeA, "appears twice"},
		// The write of the new file fails (its parent is a file): the
		// already written src/a.txt is restored.
		"write fails": {changeA + "--- /dev/null\n+++ b/src/b.txt/inner.txt\n@@ -0,0 +1 @@\n+x\n", "src/b.txt"},
	} {
		t.Run(name, func(t *testing.T) {
			r, err := call(t, gw, p, "filesystem.apply_patch", map[string]any{"patch": tc.patch})
			msg := ""
			switch {
			case err != nil:
				msg = err.Error()
			case r.IsError:
				msg = r.Content
			default:
				t.Fatalf("expected a failure, got %q", r.Content)
			}
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("%q does not mention %q", msg, tc.want)
			}
			if readFile(t, dir, "src/a.txt") != "one\ntwo\n" || readFile(t, dir, "src/b.txt") != "three\n" || readFile(t, dir, "docs/c.txt") != "c\n" {
				t.Fatal("a failed patch changed files")
			}
		})
	}
	if _, err := os.Stat(filepath.Join(dir, "src/.git")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused path was created: %v", err)
	}
}

func TestEditToolsNeedTheWriteGrant(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"a.txt": "a\n"})
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, nil)
	p := principal(dir, domain.ToolGrant{Name: "filesystem.read"})
	for _, tool := range []string{"filesystem.multi_edit", "filesystem.apply_patch"} {
		args := map[string]any{"path": "a.txt", "edits": []map[string]any{{"old": "a", "new": "b"}}}
		if tool == "filesystem.apply_patch" {
			args = map[string]any{"patch": "--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-a\n+b\n"}
		}
		if _, err := call(t, gw, p, tool, args); !errors.Is(err, ErrDenied) {
			t.Fatalf("%s without filesystem.write: %v", tool, err)
		}
	}
	if readFile(t, dir, "a.txt") != "a\n" {
		t.Fatal("changed without a grant")
	}
}
