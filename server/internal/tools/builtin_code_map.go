package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/woliveiras/weft/server/internal/domain"
)

const (
	codeMapDefaultTokens = 1000
	codeMapMinTokens     = 200
	// codeMapMaxTokens keeps the text (4 characters per token) below the
	// gateway's 32 KiB result cap.
	codeMapMaxTokens     = 8000
	codeMapSymbolsPerRow = 8
)

// Scan limits of code.map. Variables so tests can lower them.
var (
	codeMapMaxEntries          = 50000 // directory entries visited
	codeMapMaxFiles            = 5000  // source files outlined
	codeMapMaxFileBytes  int64 = 512 << 10
	codeMapMaxTotalBytes int64 = 48 << 20
)

// mapIgnoredDirs are build outputs and dependency trees the map skips on
// top of ignoredDir (search still covers them, except vendor).
var mapIgnoredDirs = map[string]bool{"build": true, "dist": true, "target": true, "out": true, "coverage": true, "bower_components": true}

// Skip reasons reported by CodeMap.Skipped.
const (
	skipUnreadable = "unreadable"
	skipTooLarge   = "too large"
	skipUnparsed   = "not outlined"
)

// CodeMapOptions bounds BuildCodeMap.
type CodeMapOptions struct {
	// Path is the directory to map, relative to the workspace (default ".").
	Path string
	// MaxTokens is the size budget of the text, estimated as four
	// characters per token (default 1000, clamped to 200..8000).
	MaxTokens int
}

// CodeMap is a token-bounded overview of a repository.
type CodeMap struct {
	Text string `json:"-"`
	// Files is the number of source files outlined; Shown the number listed
	// with symbols.
	Files   int `json:"files"`
	Symbols int `json:"symbols"`
	Shown   int `json:"shown"`
	// Approximate counts outlines with syntax errors or heuristic desync.
	Approximate int `json:"approximate,omitempty"`
	// Skipped counts source files left out, by reason.
	Skipped map[string]int `json:"skipped,omitempty"`
	// ScanLimited is set when a scan limit stopped the walk.
	ScanLimited bool `json:"scanLimited,omitempty"`
	// Truncated is set when the budget, a scan limit or a skip left
	// files out.
	Truncated bool `json:"truncated"`
}

type mapFile struct {
	rel, dir  string
	abs       string
	lines     int
	generated bool
	syms      []Symbol
	ranks     []float64
	score     float64
}

// BuildCodeMapFor returns the map a code.map call by p would return, for
// the system prompt of a planning agent. It goes through the gateway, so
// grants, task consent, rate limits, workspace preparation and audit apply
// as for any call. It never waits for a person: when the call would need
// an approval it fails with ErrDenied (the agent can still call code.map).
func BuildCodeMapFor(ctx context.Context, gw *Gateway, p *Principal, opts CodeMapOptions) (*CodeMap, error) {
	if gw == nil {
		return nil, errors.New("code map: no tool gateway")
	}
	args := map[string]any{}
	if opts.Path != "" {
		args["path"] = opts.Path
	}
	if opts.MaxTokens > 0 {
		args["maxTokens"] = opts.MaxTokens
	}
	b, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("code map arguments: %w", err)
	}
	res, err := gw.Invoke(WithoutApproval(ctx), p, "code.map", b)
	if err != nil {
		return nil, err
	}
	if res.IsError {
		return nil, fmt.Errorf("code.map: %s", res.Content)
	}
	var m CodeMap
	if err := json.Unmarshal(res.Data, &m); err != nil {
		return nil, fmt.Errorf("code.map result: %w", err)
	}
	m.Text = res.Content
	m.Truncated = m.Truncated || res.Truncated
	return &m, nil
}

func codeMapTool(ctx context.Context, inv *Invocation) (*Result, error) {
	var a struct {
		Path      string `json:"path"`
		MaxTokens int    `json:"maxTokens"`
	}
	if err := decodeArgs(inv, &a); err != nil {
		return nil, err
	}
	m, err := BuildCodeMap(ctx, workdirOf(inv), inv.Grant, CodeMapOptions{Path: a.Path, MaxTokens: a.MaxTokens})
	if err != nil {
		return nil, err
	}
	res := jsonResult(m.Text, m)
	res.Truncated = m.Truncated
	return res, nil
}

// BuildCodeMap walks the source files under opts.Path that grant allows,
// outlines them and renders directories plus the top symbols of the files
// ranked by size and by how many other files mention their symbols. It
// takes the grant directly; agents and prompts go through code.map or
// BuildCodeMapFor.
func BuildCodeMap(ctx context.Context, workdir string, grant domain.ToolGrant, opts CodeMapOptions) (*CodeMap, error) {
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = codeMapDefaultTokens
	}
	maxTokens = min(max(maxTokens, codeMapMinTokens), codeMapMaxTokens)
	abs, rel, err := ResolvePath(workdir, opts.Path)
	if err != nil {
		return nil, err
	}
	if CheckPath(grant, rel) != nil && !pathMayContain(grant, rel) {
		return nil, CheckPath(grant, rel)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, errors.Unwrap(err))
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("%s is a file; use code.outline", rel)
	}
	// ResolvePath returned a physical path; resolve the root once so the
	// walk needs no symlink lookups per entry.
	root, err := filepath.Abs(workdir)
	if err != nil {
		return nil, err
	}
	if root, err = filepath.EvalSymlinks(root); err != nil {
		return nil, fmt.Errorf("workdir: %w", err)
	}
	ignore := loadIgnoreNames(root, abs)

	m := &CodeMap{Skipped: map[string]int{}}
	var (
		files   []*mapFile
		total   int64
		entries int
	)
	err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			if p == abs {
				return werr
			}
			m.Skipped[skipUnreadable]++
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		entries++
		if entries > codeMapMaxEntries {
			m.ScanLimited = true
			return filepath.SkipAll
		}
		r, ok := within(root, p)
		if !ok {
			return nil
		}
		r = filepath.ToSlash(r)
		name := d.Name()
		if d.IsDir() {
			if p != abs && (ignoredDir(name) || mapIgnoredDirs[name] || strings.HasPrefix(name, ".") ||
				ignore.match(name, true) || isVirtualenv(p) || !pathMayContain(grant, r)) {
				return filepath.SkipDir
			}
			return nil
		}
		if LanguageOf(r) == "" || strings.HasSuffix(name, ".min.js") || ignore.match(name, false) || CheckPath(grant, r) != nil {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			m.Skipped[skipUnreadable]++
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil // symlinks and devices are not followed
		}
		if info.Size() > codeMapMaxFileBytes {
			m.Skipped[skipTooLarge]++
			return nil
		}
		if len(files) >= codeMapMaxFiles || total+info.Size() > codeMapMaxTotalBytes {
			m.ScanLimited = true
			return filepath.SkipAll
		}
		src, rerr := readBounded(p, codeMapMaxFileBytes)
		if errors.Is(rerr, errTooLarge) {
			m.Skipped[skipTooLarge]++
			return nil
		}
		if rerr != nil {
			m.Skipped[skipUnreadable]++
			return nil
		}
		total += int64(len(src))
		o, oerr := OutlineSource(r, src)
		if oerr != nil {
			m.Skipped[skipUnparsed]++
			return nil
		}
		if o.Approximate() {
			m.Approximate++
		}
		files = append(files, &mapFile{rel: r, dir: path.Dir(r), abs: p, lines: bytes.Count(src, []byte{'\n'}) + 1,
			generated: isGenerated(src), syms: o.Symbols})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("code map of %s: %w", rel, err)
	}
	if err := rankFiles(ctx, files, m); err != nil {
		return nil, err
	}
	m.Files = len(files)
	for _, f := range files {
		m.Symbols += len(f.syms)
	}
	for k, v := range m.Skipped {
		if v == 0 {
			delete(m.Skipped, k)
		}
	}
	m.Text, m.Shown = renderCodeMap(rel, files, m, maxTokens*4)
	m.Truncated = m.ScanLimited || len(m.Skipped) > 0 || m.Shown < countWithSymbols(files)
	return m, nil
}

// isVirtualenv reports Python virtual environments (a pyvenv.cfg file).
func isVirtualenv(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, "pyvenv.cfg"))
	return err == nil
}

var generatedRe = regexp.MustCompile(`(?m)^// Code generated .* DO NOT EDIT\.$|@generated`)

func isGenerated(src []byte) bool { return generatedRe.Match(src[:min(len(src), 2048)]) }

// ignoreNames holds the simple name patterns of .gitignore files: entries
// without an inner slash ("dist/", "*.pb.go"). Path patterns and negations
// are not interpreted, so they never hide a file by accident.
type ignoreNames []struct {
	glob    string
	dirOnly bool
}

func loadIgnoreNames(dirs ...string) ignoreNames {
	var out ignoreNames
	seen := map[string]bool{}
	for _, d := range dirs {
		if seen[d] {
			continue
		}
		seen[d] = true
		b, err := readBounded(filepath.Join(d, ".gitignore"), 64<<10)
		if err != nil {
			continue // no (or an unreadable) .gitignore: nothing extra to skip
		}
		sc := bufio.NewScanner(bytes.NewReader(b))
		for sc.Scan() {
			l := strings.TrimSpace(sc.Text())
			if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "!") {
				continue
			}
			dirOnly := strings.HasSuffix(l, "/")
			l = strings.TrimPrefix(strings.TrimSuffix(l, "/"), "/")
			if l == "" || strings.Contains(l, "/") || strings.Contains(l, "**") {
				continue
			}
			if _, err := filepath.Match(l, "x"); err != nil {
				continue
			}
			out = append(out, struct {
				glob    string
				dirOnly bool
			}{l, dirOnly})
		}
	}
	return out
}

func (ig ignoreNames) match(name string, dir bool) bool {
	for _, p := range ig {
		if p.dirOnly && !dir {
			continue
		}
		if ok, _ := filepath.Match(p.glob, name); ok {
			return true
		}
	}
	return false
}

func countWithSymbols(files []*mapFile) int {
	n := 0
	for _, f := range files {
		if len(f.syms) > 0 {
			n++
		}
	}
	return n
}

var kindWeight = map[string]float64{"type": 3, "struct": 3, "interface": 3, "class": 3, "enum": 2, "func": 2, "method": 1}

// rankFiles counts, for every symbol name, how many files mention it (a
// second read of the files, looking only for known names so memory stays
// bounded by the symbols), then scores symbols and files.
func rankFiles(ctx context.Context, files []*mapFile, m *CodeMap) error {
	defs := map[string]int{} // files defining each name
	for _, f := range files {
		seen := map[string]bool{}
		for _, s := range f.syms {
			if rankable(s.Name) && !seen[s.Name] {
				seen[s.Name] = true
				defs[s.Name]++
			}
		}
	}
	mentions := map[string]int{}
	for _, f := range files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		src, err := readBounded(f.abs, codeMapMaxFileBytes)
		if err != nil {
			m.Skipped[skipUnreadable]++ // vanished or changed since the walk: its mentions are not counted
			continue
		}
		for id := range knownIdentifiers(src, defs) {
			mentions[id]++
		}
	}
	for _, f := range files {
		rankFile(f, mentions, defs)
	}
	return nil
}

// rankable excludes names whose mentions say nothing: very short names and
// Python dunder methods.
func rankable(name string) bool {
	return len(name) >= 3 && !(strings.HasPrefix(name, "__") && strings.HasSuffix(name, "__"))
}

// rankFile scores each symbol (mentions from other files, span, kind) and
// the file (size plus the mentions of its symbols), then orders the
// symbols by rank. Names defined in many files ("String", "init") have
// their mentions divided among them; generated and test files rank lower.
func rankFile(f *mapFile, mentions, defs map[string]int) {
	f.ranks = make([]float64, len(f.syms))
	refs := 0.0
	for i, s := range f.syms {
		r := 0.0
		if rankable(s.Name) {
			r = float64(max(mentions[s.Name]-defs[s.Name], 0)) / float64(max(defs[s.Name], 1))
		}
		r = math.Min(r, 50)
		refs += r
		span := 0
		if s.EndLine > s.Line {
			span = s.EndLine - s.Line
		}
		f.ranks[i] = 2*r + math.Sqrt(float64(span)) + kindWeight[s.Kind]
	}
	f.score = math.Sqrt(float64(f.lines)) + refs
	if isTestFile(f.rel) {
		f.score *= 0.3
	}
	if f.generated {
		f.score *= 0.2
	}
	idx := make([]int, len(f.syms))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return f.ranks[idx[a]] > f.ranks[idx[b]] })
	syms, ranks := make([]Symbol, len(idx)), make([]float64, len(idx))
	for i, j := range idx {
		syms[i], ranks[i] = f.syms[j], f.ranks[j]
	}
	f.syms, f.ranks = syms, ranks
}

func isTestFile(rel string) bool {
	base := path.Base(rel)
	return strings.HasSuffix(base, "_test.go") || strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") ||
		strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "Test.java")
}

// knownIdentifiers returns the identifiers of src that are keys of known.
func knownIdentifiers(src []byte, known map[string]int) map[string]struct{} {
	out := map[string]struct{}{}
	isStart := func(c byte) bool { return c == '_' || c == '$' || (c|0x20 >= 'a' && c|0x20 <= 'z') }
	for i := 0; i < len(src); {
		if !isStart(src[i]) || (i > 0 && isIdentByte(src[i-1])) {
			i++
			continue
		}
		j := i + 1
		for j < len(src) && isIdentByte(src[j]) {
			j++
		}
		if _, ok := known[string(src[i:j])]; ok {
			out[string(src[i:j])] = struct{}{}
		}
		i = j
	}
	return out
}

// renderCodeMap writes the header, the notices (before the content, so a
// cut result still says what is missing), the directories and the files
// that fit in maxChars, grouped by directory.
func renderCodeMap(root string, files []*mapFile, m *CodeMap, maxChars int) (string, int) {
	header := fmt.Sprintf("Repository map of %s: %d source files (most important first per directory; L = line).\n", root, len(files))
	const noticeReserve = 240
	used := len(header) + noticeReserve

	counts := map[string]int{}
	for _, f := range files {
		counts[f.dir]++
	}
	dirs := make([]string, 0, len(counts))
	for d := range counts {
		dirs = append(dirs, d)
	}
	sort.Slice(dirs, func(i, j int) bool {
		if counts[dirs[i]] != counts[dirs[j]] {
			return counts[dirs[i]] > counts[dirs[j]]
		}
		return dirs[i] < dirs[j]
	})
	var dl strings.Builder
	shownDirs := 0
	for _, d := range dirs {
		item := fmt.Sprintf("%s (%d)", d, counts[d])
		if dl.Len()+len(item)+2 > maxChars/4 {
			break
		}
		if dl.Len() > 0 {
			dl.WriteString(", ")
		}
		dl.WriteString(item)
		shownDirs++
	}
	dirLine := ""
	if len(dirs) > 0 {
		dirLine = "Directories: " + dl.String()
		if shownDirs < len(dirs) {
			dirLine += fmt.Sprintf(", … %d more", len(dirs)-shownDirs)
		}
		dirLine += "\n"
	}
	used += len(dirLine)

	ranked := make([]*mapFile, 0, len(files))
	for _, f := range files {
		if len(f.syms) > 0 {
			ranked = append(ranked, f)
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].rel < ranked[j].rel
	})
	rows := map[string]string{}
	byDir := map[string][]*mapFile{}
	for _, f := range ranked {
		cost := 0
		if _, ok := byDir[f.dir]; !ok {
			cost += len(f.dir) + 2
		}
		row := ""
		for _, n := range []int{codeMapSymbolsPerRow, 3, 1} {
			row = mapRow(f, n)
			if used+cost+len(row) <= maxChars {
				break
			}
			row = ""
		}
		if row == "" {
			continue
		}
		used += cost + len(row)
		rows[f.rel] = row
		byDir[f.dir] = append(byDir[f.dir], f)
	}

	var b strings.Builder
	b.WriteString(header)
	if left := len(ranked) - len(rows); left > 0 {
		fmt.Fprintf(&b, "…[%d more files not shown; use code.map with a narrower path or a larger maxTokens, or code.outline]\n", left)
	}
	if m.ScanLimited {
		fmt.Fprintf(&b, "…[scan stopped at %d entries, %d files or %d MiB; use a narrower path]\n", codeMapMaxEntries, codeMapMaxFiles, codeMapMaxTotalBytes>>20)
	}
	if len(m.Skipped) > 0 {
		reasons := make([]string, 0, len(m.Skipped))
		for k := range m.Skipped {
			reasons = append(reasons, k)
		}
		sort.Strings(reasons)
		parts := make([]string, len(reasons))
		for i, k := range reasons {
			parts[i] = fmt.Sprintf("%d %s", m.Skipped[k], k)
		}
		fmt.Fprintf(&b, "…[skipped: %s (limit %d KiB per file)]\n", strings.Join(parts, ", "), codeMapMaxFileBytes>>10)
	}
	if m.Approximate > 0 {
		fmt.Fprintf(&b, "…[%d outlines are approximate (syntax errors or heuristic desync); check with code.outline]\n", m.Approximate)
	}
	b.WriteString(dirLine)
	shownDirNames := make([]string, 0, len(byDir))
	for d := range byDir {
		shownDirNames = append(shownDirNames, d)
	}
	sort.Strings(shownDirNames)
	for _, d := range shownDirNames {
		b.WriteString(d + "/\n")
		for _, f := range byDir[d] {
			b.WriteString(rows[f.rel])
		}
	}
	if len(files) == 0 {
		b.WriteString("(no Go, TypeScript/JavaScript, Python or Java files)\n")
	}
	return b.String(), len(rows)
}

func mapRow(f *mapFile, n int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s (%d lines):", path.Base(f.rel), f.lines)
	for i, s := range f.syms {
		if i == n {
			fmt.Fprintf(&b, " +%d more", len(f.syms)-n)
			break
		}
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, " %s %s L%d", s.Kind, s.QualifiedName(), s.Line)
	}
	b.WriteByte('\n')
	return b.String()
}
