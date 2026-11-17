package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Edit tools that rarely fail (NAT-03). Models get snippets slightly wrong
// all the time (indentation, trailing spaces, a changed line); a bare "not
// found" makes small models retry blindly. Every failure here says where
// the closest text is and how it differs, and multi-file changes are
// all-or-nothing.

// editTools are registered with the filesystem tools; they need the
// filesystem.write capability like filesystem.edit.
func editTools() []Tool {
	return []Tool{
		&funcTool{name: "filesystem.multi_edit", capability: capFSWrite,
			description: "Apply several exact-snippet edits to one file, in order, atomically: if any edit fails, the file is left unchanged.",
			schema:      schema(`{"type":"object","properties":{"path":{"type":"string"},"edits":{"type":"array","minItems":1,"maxItems":50,"items":{"type":"object","properties":{"old":{"type":"string","description":"Exact text to replace"},"new":{"type":"string"},"replaceAll":{"type":"boolean"}},"required":["old","new"],"additionalProperties":false}}},"required":["path","edits"],"additionalProperties":false}`),
			fn:          fsMultiEdit},
		&funcTool{name: "filesystem.apply_patch", capability: capFSWrite,
			description: "Apply a unified diff (---/+++ headers, @@ hunks) to one or more files atomically. Creates files from /dev/null; does not delete or rename files.",
			schema:      schema(`{"type":"object","properties":{"patch":{"type":"string","description":"Unified diff"}},"required":["patch"],"additionalProperties":false}`),
			fn:          fsApplyPatch},
	}
}

// maxEditFile bounds the files the edit tools rewrite in memory.
const maxEditFile = 8 << 20

type snippetEdit struct {
	Old        string `json:"old"`
	New        string `json:"new"`
	ReplaceAll bool   `json:"replaceAll"`
}

// applySnippet replaces old in s. It returns the new content and the number
// of replacements, or an error that locates the closest text.
func applySnippet(s string, e snippetEdit) (string, int, error) {
	if e.Old == "" {
		return "", 0, errors.New("'old' must not be empty")
	}
	old := e.Old
	n := strings.Count(s, old)
	if n == 0 && strings.Contains(s, "\r\n") && !strings.Contains(old, "\r\n") {
		// The file uses CRLF; the model wrote LF. Same text otherwise.
		old = strings.ReplaceAll(old, "\n", "\r\n")
		e.New = strings.ReplaceAll(e.New, "\n", "\r\n")
		n = strings.Count(s, old)
	}
	switch {
	case n == 0:
		return "", 0, fmt.Errorf("the text to replace was not found%s", closestRegion(s, e.Old))
	case n > 1 && !e.ReplaceAll:
		return "", 0, fmt.Errorf("the text to replace occurs %d times (first at line %d); add surrounding lines to make it unique or set replaceAll", n, lineOf(s, strings.Index(s, old)))
	}
	if e.ReplaceAll {
		return strings.ReplaceAll(s, old, e.New), n, nil
	}
	return strings.Replace(s, old, e.New, 1), 1, nil
}

func lineOf(s string, off int) int { return strings.Count(s[:off], "\n") + 1 }

// closestRegion describes the lines of s most similar to snippet, so the
// model can copy the exact text. It returns "" when nothing is similar.
func closestRegion(s, snippet string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	want := strings.Split(strings.TrimRight(strings.ReplaceAll(snippet, "\r\n", "\n"), "\n"), "\n")
	if len(want) == 0 || len(lines) == 0 || len(lines)*len(want) > 250_000 {
		return ""
	}
	n := min(len(want), len(lines))
	best, bestScore := -1, 0.0
	for i := 0; i+n <= len(lines); i++ {
		score := 0.0
		for j := 0; j < n; j++ {
			score += lineSimilarity(lines[i+j], want[j])
		}
		score /= float64(len(want))
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	if best < 0 || bestScore < 0.5 {
		return "; no similar text either - read the file again before editing"
	}
	region := lines[best : best+n]
	var b strings.Builder
	fmt.Fprintf(&b, ". Closest text is lines %d-%d (%d%% similar)", best+1, best+n, int(bestScore*100))
	if why := explainDifference(region, want); why != "" {
		b.WriteString("; " + why)
	}
	b.WriteString(". Copy it exactly:\n")
	for j, l := range region {
		if j == 30 {
			fmt.Fprintf(&b, "…(%d more lines)\n", len(region)-j)
			break
		}
		fmt.Fprintf(&b, "%d\t%s\n", best+j+1, l)
	}
	return strings.TrimRight(b.String(), "\n")
}

// explainDifference names the kind of mismatch between the file's region
// and the snippet.
func explainDifference(region, want []string) string {
	if len(region) != len(want) {
		return fmt.Sprintf("the snippet has %d lines, the file region %d", len(want), len(region))
	}
	indent, trailing, first := false, false, -1
	for i := range region {
		got, w := region[i], want[i]
		if got == w {
			continue
		}
		if strings.TrimSpace(got) != strings.TrimSpace(w) {
			if first < 0 {
				first = i
			}
			continue
		}
		if leading(got) != leading(w) {
			indent = true
		}
		if strings.TrimLeft(got, " \t") != strings.TrimLeft(w, " \t") {
			trailing = true
		}
	}
	var parts []string
	if indent {
		parts = append(parts, "indentation differs ("+indentKind(region)+" in the file)")
	}
	if trailing {
		parts = append(parts, "trailing whitespace differs")
	}
	if first >= 0 {
		parts = append(parts, fmt.Sprintf("snippet line %d differs: file has %q", first+1, truncateRunes(strings.TrimSpace(region[first]), 120)))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ")
}

func leading(s string) string { return s[:len(s)-len(strings.TrimLeft(s, " \t"))] }

func indentKind(lines []string) string {
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "\t"):
			return "tabs"
		case strings.HasPrefix(l, " "):
			return "spaces"
		}
	}
	return "no indentation"
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// lineSimilarity is 1 for equal lines, 0.9 for lines equal up to
// whitespace, else the Dice coefficient of their character bigrams.
func lineSimilarity(a, b string) float64 {
	if a == b {
		return 1
	}
	ta, tb := strings.Join(strings.Fields(a), " "), strings.Join(strings.Fields(b), " ")
	if ta == tb {
		return 0.9
	}
	if len(ta) < 2 || len(tb) < 2 {
		return 0
	}
	grams := map[string]int{}
	for i := 0; i+1 < len(ta); i++ {
		grams[ta[i:i+2]]++
	}
	common := 0
	for i := 0; i+1 < len(tb); i++ {
		g := tb[i : i+2]
		if grams[g] > 0 {
			grams[g]--
			common++
		}
	}
	return 0.8 * 2 * float64(common) / float64(len(ta)-1+len(tb)-1)
}

func readEditable(abs, rel string) (string, error) {
	st, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("%s: %w", rel, errors.Unwrap(err))
	}
	if st.IsDir() {
		return "", fmt.Errorf("%s is a directory", rel)
	}
	if st.Size() > maxEditFile {
		return "", fmt.Errorf("%s is %d bytes; the edit tools handle files up to %d bytes", rel, st.Size(), maxEditFile)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("%s: %w", rel, errors.Unwrap(err))
	}
	return string(b), nil
}

func fsMultiEdit(_ context.Context, inv *Invocation) (*Result, error) {
	var a struct {
		Path  string        `json:"path"`
		Edits []snippetEdit `json:"edits"`
	}
	if err := decodeArgs(inv, &a); err != nil {
		return nil, err
	}
	if len(a.Edits) == 0 {
		return nil, errors.New("'edits' must not be empty")
	}
	abs, rel, err := jailWrite(inv, a.Path)
	if err != nil {
		return nil, err
	}
	s, err := readEditable(abs, rel)
	if err != nil {
		return nil, err
	}
	total := 0
	for i, e := range a.Edits {
		next, n, err := applySnippet(s, e)
		if err != nil {
			return nil, fmt.Errorf("edit %d of %d in %s: %w (no edit was applied)", i+1, len(a.Edits), rel, err)
		}
		s, total = next, total+n
	}
	if err := atomicWrite(abs, []byte(s)); err != nil {
		return nil, err
	}
	return jsonResult(fmt.Sprintf("applied %d edit(s) (%d replacement(s)) to %s", len(a.Edits), total, rel), map[string]any{"path": rel, "edits": len(a.Edits), "replacements": total}), nil
}

// filePatch is one file's part of a unified diff.
type filePatch struct {
	path  string
	isNew bool
	hunks []hunk
}

type hunk struct {
	oldStart int // 1-based, 0 when unknown
	lines    []string
	header   string
	// noNewline marks "\ No newline at end of file" after the new side.
	noNewline bool
}

// parsePatch reads a unified diff. Hunk line counts in the @@ headers are
// ignored (models often get them wrong); a hunk runs until the next header.
func parsePatch(patch string) ([]filePatch, error) {
	lines := strings.Split(strings.ReplaceAll(patch, "\r\n", "\n"), "\n")
	var files []filePatch
	var cur *filePatch
	var h *hunk
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		switch {
		case strings.HasPrefix(l, "--- ") && i+1 < len(lines) && strings.HasPrefix(lines[i+1], "+++ "):
			oldPath, newPath := patchPath(l[4:]), patchPath(lines[i+1][4:])
			i++
			if newPath == "" {
				return nil, fmt.Errorf("the patch deletes %s; use filesystem.delete to delete files", oldPath)
			}
			if oldPath != "" && oldPath != newPath {
				return nil, fmt.Errorf("the patch renames %s to %s; use filesystem.move to rename files", oldPath, newPath)
			}
			files = append(files, filePatch{path: newPath, isNew: oldPath == ""})
			cur, h = &files[len(files)-1], nil
		case strings.HasPrefix(l, "@@"):
			if cur == nil {
				return nil, fmt.Errorf("patch line %d: hunk before any ---/+++ file header", i+1)
			}
			cur.hunks = append(cur.hunks, hunk{oldStart: hunkStart(l), header: l})
			h = &cur.hunks[len(cur.hunks)-1]
		case h != nil && strings.HasPrefix(l, `\ No newline at end of file`):
			if n := len(h.lines); n > 0 && h.lines[n-1][0] != '-' {
				h.noNewline = true
			}
		case h != nil && l != "" && (l[0] == ' ' || l[0] == '+' || l[0] == '-'):
			h.lines = append(h.lines, l)
		case h != nil && l == "" && i < len(lines)-1:
			// An empty context line whose leading space was stripped.
			h.lines = append(h.lines, " ")
		case strings.HasPrefix(l, "diff ") || strings.HasPrefix(l, "index ") || strings.HasPrefix(l, "new file mode") || l == "" || (h == nil && cur == nil):
			h = nil
		default:
			return nil, fmt.Errorf("patch line %d is not part of a unified diff: %q", i+1, truncateRunes(l, 80))
		}
	}
	if len(files) == 0 {
		return nil, errors.New("no file headers (--- a/path, +++ b/path) found in the patch")
	}
	for _, f := range files {
		if len(f.hunks) == 0 {
			return nil, fmt.Errorf("%s has no @@ hunks", f.path)
		}
	}
	return files, nil
}

func patchPath(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\t'); i >= 0 {
		s = s[:i] // timestamps
	}
	if s == "/dev/null" {
		return ""
	}
	if strings.HasPrefix(s, "a/") || strings.HasPrefix(s, "b/") {
		s = s[2:]
	}
	return s
}

func hunkStart(header string) int {
	f := strings.Fields(header)
	if len(f) < 2 || !strings.HasPrefix(f[1], "-") {
		return 0
	}
	n, _ := strconv.Atoi(strings.SplitN(f[1][1:], ",", 2)[0])
	return n
}

// applyHunks applies a file's hunks to content, matching each hunk's old
// side exactly (then ignoring trailing whitespace) nearest to its stated line.
func applyHunks(content string, f filePatch) (string, int, int, error) {
	crlf := strings.Contains(content, "\r\n")
	text := strings.ReplaceAll(content, "\r\n", "\n")
	endsNL := strings.HasSuffix(text, "\n") || text == ""
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if text == "" {
		lines = nil
	}
	added, removed, offset := 0, 0, 0
	for hi, h := range f.hunks {
		var old, repl []string
		for _, l := range h.lines {
			switch l[0] {
			case ' ':
				old, repl = append(old, l[1:]), append(repl, l[1:])
			case '-':
				old = append(old, l[1:])
				removed++
			case '+':
				repl = append(repl, l[1:])
				added++
			}
		}
		at := findLines(lines, old, h.oldStart-1+offset)
		if at < 0 {
			return "", 0, 0, fmt.Errorf("hunk %d (%s) does not match %s%s", hi+1, h.header, f.path, closestRegion(strings.Join(lines, "\n"), strings.Join(old, "\n")))
		}
		lines = append(lines[:at], append(repl, lines[at+len(old):]...)...)
		offset += len(repl) - len(old)
		if h.noNewline && at+len(repl) == len(lines) {
			endsNL = false
		}
	}
	out := strings.Join(lines, "\n")
	if endsNL && len(lines) > 0 {
		out += "\n"
	}
	if crlf {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	return out, added, removed, nil
}

// findLines finds old in lines, preferring the position nearest to hint.
func findLines(lines, old []string, hint int) int {
	if len(old) == 0 {
		return max(0, min(hint, len(lines)))
	}
	match := func(eq func(a, b string) bool) int {
		best := -1
		for i := 0; i+len(old) <= len(lines); i++ {
			ok := true
			for j := range old {
				if !eq(lines[i+j], old[j]) {
					ok = false
					break
				}
			}
			if ok && (best < 0 || abs(i-hint) < abs(best-hint)) {
				best = i
			}
		}
		return best
	}
	if at := match(func(a, b string) bool { return a == b }); at >= 0 {
		return at
	}
	return match(func(a, b string) bool { return strings.TrimRight(a, " \t") == strings.TrimRight(b, " \t") })
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func fsApplyPatch(_ context.Context, inv *Invocation) (*Result, error) {
	var a struct {
		Patch string `json:"patch"`
	}
	if err := decodeArgs(inv, &a); err != nil {
		return nil, err
	}
	files, err := parsePatch(a.Patch)
	if err != nil {
		return nil, err
	}
	var plan []patchedFile
	seen := map[string]bool{}
	// Check every path and compute every result before writing anything.
	for _, f := range files {
		abs, rel, err := jailWrite(inv, f.path)
		if err != nil {
			return nil, err
		}
		if rel == "." {
			return nil, errors.New("a patch path must name a file")
		}
		if seen[rel] {
			return nil, fmt.Errorf("%s appears twice in the patch; put all its hunks under one header", rel)
		}
		seen[rel] = true
		p := patchedFile{abs: abs, rel: rel}
		content := ""
		if f.isNew {
			if _, err := os.Lstat(abs); err == nil {
				return nil, fmt.Errorf("the patch creates %s, which already exists", rel)
			}
		} else {
			s, err := readEditable(abs, rel)
			if err != nil {
				return nil, err
			}
			content, p.before = s, []byte(s)
		}
		out, add, del, err := applyHunks(content, f)
		if err != nil {
			return nil, fmt.Errorf("%w (no file was changed)", err)
		}
		p.after, p.add, p.del = []byte(out), add, del
		plan = append(plan, p)
	}
	// Write; on failure restore what was already written.
	for i, p := range plan {
		if p.before == nil {
			if err := os.MkdirAll(filepath.Dir(p.abs), 0o755); err != nil {
				return nil, errors.Join(err, rollback(plan[:i]))
			}
		}
		if err := atomicWrite(p.abs, p.after); err != nil {
			return nil, errors.Join(fmt.Errorf("write %s: %w", p.rel, err), rollback(plan[:i]))
		}
	}
	var summary []string
	changed := make([]map[string]any, 0, len(plan))
	for _, p := range plan {
		kind := ""
		if p.before == nil {
			kind = "new, "
		}
		summary = append(summary, fmt.Sprintf("%s (%s+%d -%d)", p.rel, kind, p.add, p.del))
		changed = append(changed, map[string]any{"path": p.rel, "created": p.before == nil, "added": p.add, "removed": p.del})
	}
	return jsonResult(fmt.Sprintf("patched %d file(s): %s", len(plan), strings.Join(summary, ", ")), map[string]any{"files": changed}), nil
}

// patchedFile is one file of a patch, computed before anything is written.
type patchedFile struct {
	abs, rel string
	before   []byte // nil for new files
	after    []byte
	add, del int
}

// restore puts the file back as it was before the patch.
func (p patchedFile) restore() error {
	if p.before == nil {
		if err := os.Remove(p.abs); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", p.rel, err)
		}
		return nil
	}
	if err := atomicWrite(p.abs, p.before); err != nil {
		return fmt.Errorf("restore %s: %w", p.rel, err)
	}
	return nil
}

// rollback restores files written before a failed write.
func rollback(done []patchedFile) error {
	var errs []error
	for _, p := range done {
		if err := p.restore(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("restoring files after a failed patch: %w", errors.Join(errs...))
	}
	return nil
}
