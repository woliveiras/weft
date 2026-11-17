package tools

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	capFSRead   = "filesystem.read"
	capFSWrite  = "filesystem.write"
	capFSDelete = "filesystem.delete"
)

// skipDirs are never descended into by list/search (noise, not source).
var skipDirs = map[string]bool{".git": true, "node_modules": true, ".venv": true, "__pycache__": true}

func filesystemTools(cfg BuiltinConfig) []Tool {
	return []Tool{
		&funcTool{name: "filesystem.read", capability: capFSRead,
			description: "Read a text file from the workspace. Paths are relative to the workspace root. Use offset/limit (1-based line numbers) to page through large files.",
			schema:      schema(`{"type":"object","properties":{"path":{"type":"string","description":"File path relative to the workspace"},"offset":{"type":"integer","minimum":1,"description":"First line to return (1-based)"},"limit":{"type":"integer","minimum":1,"description":"Maximum number of lines"}},"required":["path"],"additionalProperties":false}`),
			fn:          func(ctx context.Context, inv *Invocation) (*Result, error) { return fsRead(inv, cfg.MaxReadBytes) }},
		&funcTool{name: "filesystem.list", capability: capFSRead,
			description: "List files and directories in the workspace. Directories end with '/'. Set recursive to walk subdirectories; glob filters names (e.g. '*.go' or 'src/**/*.ts').",
			schema:      schema(`{"type":"object","properties":{"path":{"type":"string","description":"Directory relative to the workspace (default '.')"},"recursive":{"type":"boolean"},"glob":{"type":"string"},"maxEntries":{"type":"integer","minimum":1,"maximum":5000}},"additionalProperties":false}`),
			fn:          fsList},
		&funcTool{name: "filesystem.search", capability: capFSRead,
			description: "Search file contents. pattern is a regular expression (Go RE2) unless literal is true. Returns 'path:line: text' matches; context adds lines around each match. Skips .git, node_modules and vendor unless path is inside them.",
			schema:      schema(`{"type":"object","properties":{"pattern":{"type":"string","description":"Regular expression, or exact text when literal is true"},"literal":{"type":"boolean","description":"Match pattern as exact text"},"path":{"type":"string","description":"Directory or file to search (default '.')"},"glob":{"type":"string","description":"Only search files matching this glob"},"context":{"type":"integer","minimum":0,"maximum":10,"description":"Lines to show before and after each match"},"maxResults":{"type":"integer","minimum":1,"maximum":2000,"description":"Maximum matches (default 200)"}},"required":["pattern"],"additionalProperties":false}`),
			fn:          fsSearch},
		&funcTool{name: "filesystem.stat", capability: capFSRead,
			description: "Return size, type and modification time of a workspace path, or report that it does not exist.",
			schema:      schema(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`),
			fn:          fsStat},
		&funcTool{name: "filesystem.write", capability: capFSWrite,
			description: "Create or overwrite a file (parent directories are created). Set append to add to the end instead. Writes are atomic.",
			schema:      schema(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"},"append":{"type":"boolean"}},"required":["path","content"],"additionalProperties":false}`),
			fn:          fsWrite},
		&funcTool{name: "filesystem.edit", capability: capFSWrite,
			description: "Replace an exact snippet of text in a file. 'old' must occur exactly once unless replaceAll is true. Include enough surrounding context to make it unique.",
			schema:      schema(`{"type":"object","properties":{"path":{"type":"string"},"old":{"type":"string","description":"Exact text to replace"},"new":{"type":"string","description":"Replacement text"},"replaceAll":{"type":"boolean"}},"required":["path","old","new"],"additionalProperties":false}`),
			fn:          fsEdit},
		&funcTool{name: "filesystem.move", capability: capFSWrite,
			description: "Move or rename a file or directory within the workspace. The move is atomic on one filesystem, so it can be used to claim a file.",
			schema:      schema(`{"type":"object","properties":{"from":{"type":"string"},"to":{"type":"string"}},"required":["from","to"],"additionalProperties":false}`),
			fn:          fsMove},
		&funcTool{name: "filesystem.mkdir", capability: capFSWrite,
			description: "Create a directory (and its parents).",
			schema:      schema(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`),
			fn:          fsMkdir},
		&funcTool{name: "filesystem.delete", capability: capFSDelete,
			description: "Delete a file, or a directory when recursive is true. The workspace root itself cannot be deleted.",
			schema:      schema(`{"type":"object","properties":{"path":{"type":"string"},"recursive":{"type":"boolean"}},"required":["path"],"additionalProperties":false}`),
			fn:          fsDelete},
	}
}

func schema(s string) []byte { return []byte(s) }

// jail resolves p and checks it against the grant's Paths.
func jail(inv *Invocation, p string) (abs, rel string, err error) {
	abs, rel, err = ResolvePath(workdirOf(inv), p)
	if err != nil {
		return "", "", err
	}
	if err := CheckPath(inv.Grant, rel); err != nil {
		return "", "", err
	}
	return abs, rel, nil
}

// jailWrite is jail for mutating tools. It additionally refuses anything
// inside a .git directory: writing .git/hooks or .git/config (core.fsmonitor,
// aliases) would turn the git tools into arbitrary code execution for an
// agent that holds filesystem.write but deliberately not shell.exec.
func jailWrite(inv *Invocation, p string) (abs, rel string, err error) {
	abs, rel, err = jail(inv, p)
	if err != nil {
		return "", "", err
	}
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.EqualFold(seg, ".git") {
			return "", "", fmt.Errorf("%w: %s is inside a .git directory; repository internals are only changed through the git tools", ErrDenied, rel)
		}
	}
	return abs, rel, nil
}

func fsRead(inv *Invocation, maxBytes int64) (*Result, error) {
	var a struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := decodeArgs(inv, &a); err != nil {
		return nil, err
	}
	abs, rel, err := jail(inv, a.Path)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, errors.Unwrap(err))
	}
	if st.IsDir() {
		return nil, fmt.Errorf("%s is a directory; use filesystem.list", rel)
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := bytes.NewBuffer(make([]byte, 0, min64(st.Size(), maxBytes)+1))
	n, err := buf.ReadFrom(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	content := buf.Bytes()
	truncated := false
	if n > maxBytes {
		content, truncated = content[:maxBytes], true
	}
	if bytes.IndexByte(content[:min(len(content), 8000)], 0) >= 0 {
		return textResult(fmt.Sprintf("%s is a binary file (%d bytes); not shown.", rel, st.Size())), nil
	}
	text := string(content)
	if a.Offset > 0 || a.Limit > 0 {
		lines := strings.SplitAfter(text, "\n")
		start := a.Offset - 1
		if start < 0 {
			start = 0
		}
		if start > len(lines) {
			start = len(lines)
		}
		end := len(lines)
		if a.Limit > 0 && start+a.Limit < end {
			end = start + a.Limit
		}
		text = fmt.Sprintf("[%s lines %d-%d of %d]\n", rel, start+1, end, len(lines)) + strings.Join(lines[start:end], "")
	}
	res := textResult(text)
	if truncated {
		res.Content += fmt.Sprintf("\n…[file truncated at %d bytes of %d; use offset/limit]", maxBytes, st.Size())
		res.Truncated = true
	}
	return res, nil
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

type fileEntry struct {
	Path    string    `json:"path"`
	Dir     bool      `json:"dir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

func fsList(_ context.Context, inv *Invocation) (*Result, error) {
	var a struct {
		Path       string `json:"path"`
		Recursive  bool   `json:"recursive"`
		Glob       string `json:"glob"`
		MaxEntries int    `json:"maxEntries"`
	}
	if err := decodeArgs(inv, &a); err != nil {
		return nil, err
	}
	if a.MaxEntries <= 0 {
		a.MaxEntries = 1000
	}
	abs, rel, err := ResolvePath(workdirOf(inv), a.Path)
	if err != nil {
		return nil, err
	}
	if CheckPath(inv.Grant, rel) != nil && !pathMayContain(inv.Grant, rel) {
		return nil, CheckPath(inv.Grant, rel)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, errors.Unwrap(err))
	}
	if !st.IsDir() {
		return jsonResult(fmtEntry(rel, st), []fileEntry{{Path: rel, Size: st.Size(), ModTime: st.ModTime()}}), nil
	}
	var entries []fileEntry
	more := false
	walkErr := filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		if p == abs {
			return nil
		}
		r, _ := filepath.Rel(workdirOf(inv), p)
		r = relFrom(inv, p, r)
		if d.IsDir() {
			if skipDirs[d.Name()] || !pathMayContain(inv.Grant, r) {
				return filepath.SkipDir
			}
		} else if CheckPath(inv.Grant, r) != nil {
			return nil
		}
		if a.Glob == "" || globHit(a.Glob, r, d.Name()) {
			if len(entries) >= a.MaxEntries {
				more = true
				return filepath.SkipAll
			}
			info, ierr := d.Info()
			e := fileEntry{Path: r, Dir: d.IsDir()}
			if ierr == nil {
				e.Size, e.ModTime = info.Size(), info.ModTime()
			}
			entries = append(entries, e)
		}
		if d.IsDir() && !a.Recursive {
			return filepath.SkipDir
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	var b strings.Builder
	for _, e := range entries {
		if e.Dir {
			b.WriteString(e.Path + "/\n")
		} else {
			fmt.Fprintf(&b, "%s\t%d\n", e.Path, e.Size)
		}
	}
	if len(entries) == 0 {
		b.WriteString("(empty)\n")
	}
	if more {
		fmt.Fprintf(&b, "…[listing stopped at %d entries]\n", a.MaxEntries)
	}
	res := jsonResult(b.String(), entries)
	res.Truncated = more
	return res, nil
}

// relFrom returns the slash path of p relative to the physical workdir.
func relFrom(inv *Invocation, p, fallback string) string {
	root, err := filepath.EvalSymlinks(workdirOf(inv))
	if err == nil {
		if r, ok := within(root, p); ok {
			return filepath.ToSlash(r)
		}
	}
	return filepath.ToSlash(fallback)
}

func globHit(glob, rel, base string) bool {
	if strings.Contains(glob, "/") {
		return MatchPathGlob(glob, rel)
	}
	ok, _ := filepath.Match(glob, base)
	return ok
}

func fmtEntry(rel string, st os.FileInfo) string {
	if st.IsDir() {
		return rel + "/\n"
	}
	return fmt.Sprintf("%s\t%d\n", rel, st.Size())
}

func fsSearch(ctx context.Context, inv *Invocation) (*Result, error) {
	var a struct {
		Pattern    string `json:"pattern"`
		Literal    bool   `json:"literal"`
		Path       string `json:"path"`
		Glob       string `json:"glob"`
		Context    int    `json:"context"`
		MaxResults int    `json:"maxResults"`
	}
	if err := decodeArgs(inv, &a); err != nil {
		return nil, err
	}
	if a.Pattern == "" {
		return nil, errors.New("pattern must not be empty")
	}
	if a.Context < 0 || a.Context > 10 {
		return nil, fmt.Errorf("context must be between 0 and 10 lines, got %d", a.Context)
	}
	expr := a.Pattern
	if a.Literal {
		expr = regexp.QuoteMeta(expr)
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern: %w (set literal to search for exact text)", err)
	}
	if a.MaxResults <= 0 {
		a.MaxResults = 200
	}
	abs, rel, err := ResolvePath(workdirOf(inv), a.Path)
	if err != nil {
		return nil, err
	}
	if CheckPath(inv.Grant, rel) != nil && !pathMayContain(inv.Grant, rel) {
		return nil, CheckPath(inv.Grant, rel)
	}
	s := &searcher{re: re, context: a.Context, max: a.MaxResults}
	err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r := relFrom(inv, p, p)
		if d.IsDir() {
			if p != abs && !pathMayContain(inv.Grant, r) {
				return filepath.SkipDir // not named: the caller may not know it exists
			}
			if p != abs && ignoredDir(d.Name()) {
				s.ignored = append(s.ignored, r+"/")
				return filepath.SkipDir
			}
			return nil
		}
		if CheckPath(inv.Grant, r) != nil || (a.Glob != "" && !globHit(a.Glob, r, d.Name())) {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil || info.Size() > 2<<20 || !info.Mode().IsRegular() {
			return nil
		}
		f, oerr := os.Open(p)
		if oerr != nil {
			return nil
		}
		defer f.Close()
		if !s.file(r, f) {
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := strings.Join(s.out, "\n")
	if s.matches == 0 {
		out = "no matches"
		if n := len(s.ignored); n > 0 {
			out += fmt.Sprintf(" (not searched: %s", strings.Join(s.ignored[:min(n, 5)], ", "))
			if n > 5 {
				out += fmt.Sprintf(" and %d more", n-5)
			}
			out += "; set path inside one to search it)"
		}
	}
	for _, note := range s.notes {
		out += "\n…[" + note + "]"
	}
	if s.more {
		out += fmt.Sprintf("\n…[stopped at %d matches]", a.MaxResults)
	}
	return &Result{Content: out, Truncated: s.more}, nil
}

// searcher collects grep-style output: "path:line: text" for matches,
// "path-line- text" for context lines and "--" between separate groups.
type searcher struct {
	re           *regexp.Regexp
	context, max int
	matches      int
	more         bool
	out          []string
	// ignored lists skipped dependency/vendored directories; notes report
	// files that could not be read to the end.
	ignored, notes []string
}

// file scans one file; it returns false once the match limit is exceeded.
func (s *searcher) file(rel string, f io.Reader) bool {
	type numbered struct {
		n int
		t string
	}
	var before []numbered
	printed, after := 0, 0 // last line emitted for this file; context lines still due
	emit := func(n int, t, sep string) {
		if s.context > 0 && len(s.out) > 0 && (printed == 0 || n > printed+1) {
			s.out = append(s.out, "--")
		}
		if len(t) > 300 {
			t = truncateUTF8(t, 300) + "…"
		}
		s.out = append(s.out, fmt.Sprintf("%s%s%d%s %s", rel, sep, n, sep, t))
		printed = n
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	line := 0
	for sc.Scan() {
		line++
		t := sc.Text()
		if line == 1 && strings.IndexByte(t, 0) >= 0 {
			return true // binary
		}
		switch {
		case s.re.MatchString(t):
			if s.matches >= s.max {
				s.more = true
				return false
			}
			s.matches++
			for _, b := range before {
				if b.n > printed {
					emit(b.n, b.t, "-")
				}
			}
			emit(line, t, ":")
			after = s.context
		case after > 0:
			emit(line, t, "-")
			after--
		}
		if s.context > 0 {
			before = append(before, numbered{line, t})
			if len(before) > s.context {
				before = before[1:]
			}
		}
	}
	if err := sc.Err(); err != nil {
		reason := err.Error()
		if errors.Is(err, bufio.ErrTooLong) {
			reason = "a line is longer than 1 MiB"
		}
		s.notes = append(s.notes, fmt.Sprintf("%s: searched only up to line %d (%s)", rel, line, reason))
	}
	return true
}

func fsStat(_ context.Context, inv *Invocation) (*Result, error) {
	var a struct {
		Path string `json:"path"`
	}
	if err := decodeArgs(inv, &a); err != nil {
		return nil, err
	}
	abs, rel, err := jail(inv, a.Path)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if errors.Is(err, fs.ErrNotExist) {
		return jsonResult(rel+" does not exist", map[string]any{"path": rel, "exists": false}), nil
	}
	if err != nil {
		return nil, err
	}
	kind := "file"
	if st.IsDir() {
		kind = "directory"
	}
	return jsonResult(fmt.Sprintf("%s: %s, %d bytes, modified %s", rel, kind, st.Size(), st.ModTime().UTC().Format(time.RFC3339)),
		map[string]any{"path": rel, "exists": true, "dir": st.IsDir(), "size": st.Size(), "modTime": st.ModTime().UTC(), "mode": st.Mode().String()}), nil
}

func fsWrite(_ context.Context, inv *Invocation) (*Result, error) {
	var a struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Append  bool   `json:"append"`
	}
	if err := decodeArgs(inv, &a); err != nil {
		return nil, err
	}
	abs, rel, err := jailWrite(inv, a.Path)
	if err != nil {
		return nil, err
	}
	if rel == "." {
		return nil, errors.New("path must name a file")
	}
	if st, err := os.Stat(abs); err == nil && st.IsDir() {
		return nil, fmt.Errorf("%s is a directory", rel)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, err
	}
	if a.Append {
		f, err := os.OpenFile(abs, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, err
		}
		if _, err := f.WriteString(a.Content); err != nil {
			f.Close()
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
		return textResult(fmt.Sprintf("appended %d bytes to %s", len(a.Content), rel)), nil
	}
	if err := atomicWrite(abs, []byte(a.Content)); err != nil {
		return nil, err
	}
	return textResult(fmt.Sprintf("wrote %d bytes to %s", len(a.Content), rel)), nil
}

// atomicWrite writes to a temp file in the same directory and renames it
// into place, so readers (and other agents) never see a partial file.
func atomicWrite(abs string, data []byte) error {
	mode := os.FileMode(0o644)
	if st, err := os.Stat(abs); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), "."+filepath.Base(abs)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	_ = os.Chmod(name, mode)
	if err := os.Rename(name, abs); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

func fsEdit(_ context.Context, inv *Invocation) (*Result, error) {
	var a struct {
		Path string `json:"path"`
		snippetEdit
	}
	if err := decodeArgs(inv, &a); err != nil {
		return nil, err
	}
	if a.Old == "" {
		return nil, errors.New("'old' must not be empty")
	}
	abs, rel, err := jailWrite(inv, a.Path)
	if err != nil {
		return nil, err
	}
	s, err := readEditable(abs, rel)
	if err != nil {
		return nil, err
	}
	s, n, err := applySnippet(s, a.snippetEdit)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}
	if err := atomicWrite(abs, []byte(s)); err != nil {
		return nil, err
	}
	return textResult(fmt.Sprintf("replaced %d occurrence(s) in %s", n, rel)), nil
}

func fsMove(_ context.Context, inv *Invocation) (*Result, error) {
	var a struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := decodeArgs(inv, &a); err != nil {
		return nil, err
	}
	fromAbs, fromRel, err := jailWrite(inv, a.From)
	if err != nil {
		return nil, err
	}
	toAbs, toRel, err := jailWrite(inv, a.To)
	if err != nil {
		return nil, err
	}
	if fromRel == "." || toRel == "." {
		return nil, errors.New("cannot move the workspace root")
	}
	if _, err := os.Lstat(fromAbs); err != nil {
		return nil, fmt.Errorf("%s: %w", fromRel, errors.Unwrap(err))
	}
	if err := os.MkdirAll(filepath.Dir(toAbs), 0o755); err != nil {
		return nil, err
	}
	if err := os.Rename(fromAbs, toAbs); err != nil {
		return nil, err
	}
	return textResult(fmt.Sprintf("moved %s to %s", fromRel, toRel)), nil
}

func fsMkdir(_ context.Context, inv *Invocation) (*Result, error) {
	var a struct {
		Path string `json:"path"`
	}
	if err := decodeArgs(inv, &a); err != nil {
		return nil, err
	}
	abs, rel, err := jailWrite(inv, a.Path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	return textResult("created " + rel + "/"), nil
}

func fsDelete(_ context.Context, inv *Invocation) (*Result, error) {
	var a struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := decodeArgs(inv, &a); err != nil {
		return nil, err
	}
	abs, rel, err := jailWrite(inv, a.Path)
	if err != nil {
		return nil, err
	}
	if rel == "." {
		return nil, errors.New("cannot delete the workspace root")
	}
	st, err := os.Lstat(abs)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, errors.Unwrap(err))
	}
	if st.IsDir() && a.Recursive {
		err = os.RemoveAll(abs)
	} else {
		err = os.Remove(abs)
	}
	if err != nil {
		if st.IsDir() && !a.Recursive {
			return nil, fmt.Errorf("%s is a non-empty directory; set recursive to delete it", rel)
		}
		return nil, err
	}
	return textResult("deleted " + rel), nil
}
