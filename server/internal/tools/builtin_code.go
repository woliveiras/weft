package tools

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
)

// Code navigation (NAT-04): read-only tools that let an agent understand a
// repository without reading every file. They share the filesystem.read
// capability, so they obey the same path grants as filesystem.read/search.
// code.map lives in builtin_code_map.go.

const (
	outlineMaxBytes   = 1 << 20
	outlineMaxLines   = 20000
	outlineMaxSymbols = 400
)

// ErrUnsupportedLanguage is returned by OutlineSource for files whose
// language code.outline does not know.
var ErrUnsupportedLanguage = errors.New("unsupported language")

// errTooLarge is returned by readBounded for files above its limit.
var errTooLarge = errors.New("file too large")

// ignoredDir reports directories that search and the code map skip unless
// the caller points inside them: VCS data, dependency trees, vendored code.
func ignoredDir(name string) bool { return skipDirs[name] || name == "vendor" }

func codeTools() []Tool {
	return []Tool{
		&funcTool{name: "code.outline", capability: capFSRead,
			description: "List the symbols of one source file (functions, methods, types, classes, constants) with line numbers. Go, TypeScript/JavaScript, Python, Java.",
			schema:      schema(`{"type":"object","properties":{"path":{"type":"string","description":"Source file relative to the workspace"}},"required":["path"],"additionalProperties":false}`),
			fn:          codeOutline},
		&funcTool{name: "code.map", capability: capFSRead,
			description: "Short map of the repository: directories and the main symbols of the most important files. Use it first to find where code lives.",
			schema:      schema(`{"type":"object","properties":{"path":{"type":"string","description":"Directory relative to the workspace (default '.')"},"maxTokens":{"type":"integer","minimum":200,"maximum":8000,"description":"Size limit of the map (default 1000)"}},"additionalProperties":false}`),
			fn:          codeMapTool},
	}
}

// Symbol is one declaration found by code.outline.
type Symbol struct {
	// Kind is func, method, type, struct, interface, class, enum, const or var.
	Kind string `json:"kind"`
	Name string `json:"name"`
	// Container is the receiver type (Go) or the enclosing class.
	Container string `json:"container,omitempty"`
	Line      int    `json:"line"`
	// EndLine is set when the parser knows it (Go).
	EndLine   int    `json:"endLine,omitempty"`
	Signature string `json:"signature,omitempty"`
}

// QualifiedName is Container.Name, or Name for top-level symbols.
func (s Symbol) QualifiedName() string {
	if s.Container != "" {
		return s.Container + "." + s.Name
	}
	return s.Name
}

// Outline is the symbol list of one file.
type Outline struct {
	Path     string   `json:"path"`
	Language string   `json:"language"`
	Symbols  []Symbol `json:"symbols"`
	// Truncated is set when the file exceeded the line or symbol bound.
	Truncated bool `json:"truncated,omitempty"`
	// ParseError is the first syntax error (Go); the symbols are those of
	// the partial syntax tree.
	ParseError string `json:"parseError,omitempty"`
	// Warning is set when a line heuristic lost track of the block
	// structure: symbols may be missing or attached to the wrong class.
	Warning string `json:"warning,omitempty"`
}

// Approximate reports whether the symbols may be incomplete or misplaced.
func (o *Outline) Approximate() bool { return o.ParseError != "" || o.Warning != "" }

// LanguageOf returns the outline language of a file name, or "".
func LanguageOf(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".go":
		return "go"
	case ".ts", ".tsx", ".mts", ".cts":
		return "typescript"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".py":
		return "python"
	case ".java":
		return "java"
	}
	return ""
}

// OutlineSource returns the symbols declared in src. Go is parsed with
// go/parser; the other languages use bounded line-based heuristics that
// find top-level declarations and the members of classes.
func OutlineSource(name string, src []byte) (*Outline, error) {
	lang := LanguageOf(name)
	o := &Outline{Path: name, Language: lang}
	switch lang {
	case "go":
		o.Symbols, o.ParseError = outlineGo(src)
	case "typescript", "javascript":
		o.Symbols, o.Truncated, o.Warning = outlineBraces(src, tsRules, braceCounter{opens: "{", closes: "}", jsLiterals: true})
	case "java":
		o.Symbols, o.Truncated, o.Warning = outlineBraces(src, javaRules, braceCounter{opens: "{", closes: "}", textBlocks: true})
	case "python":
		o.Symbols, o.Truncated, o.Warning = outlinePython(src)
	default:
		return nil, fmt.Errorf("%w: %s (code.outline supports Go, TypeScript/JavaScript, Python and Java; use filesystem.read)", ErrUnsupportedLanguage, name)
	}
	if len(o.Symbols) > outlineMaxSymbols {
		o.Symbols, o.Truncated = o.Symbols[:outlineMaxSymbols], true
	}
	return o, nil
}

// readBounded reads at most max bytes of a regular file and fails with
// errTooLarge beyond that, so a file that grows after a stat cannot
// bypass the limit.
func readBounded(abs string, max int64) ([]byte, error) {
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, errTooLarge
	}
	return b, nil
}

func codeOutline(_ context.Context, inv *Invocation) (*Result, error) {
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
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, errors.Unwrap(err))
	}
	if st.IsDir() {
		return nil, fmt.Errorf("%s is a directory; use code.map or filesystem.list", rel)
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file (%s)", rel, st.Mode().Type())
	}
	if LanguageOf(rel) == "" {
		return nil, fmt.Errorf("%w: %s (code.outline supports Go, TypeScript/JavaScript, Python and Java; use filesystem.read)", ErrUnsupportedLanguage, rel)
	}
	src, err := readBounded(abs, outlineMaxBytes)
	if errors.Is(err, errTooLarge) {
		return nil, fmt.Errorf("%s is above the %d-byte outline limit; use filesystem.search or filesystem.read with offset/limit", rel, outlineMaxBytes)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}
	o, err := OutlineSource(rel, src)
	if err != nil {
		return nil, err
	}
	res := jsonResult(formatOutline(o), o)
	res.Truncated = o.Truncated
	return res, nil
}

func formatOutline(o *Outline) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s, %d symbols)\n", o.Path, o.Language, len(o.Symbols))
	if o.ParseError != "" {
		fmt.Fprintf(&b, "syntax error: %s (symbols are from the part that parsed)\n", o.ParseError)
	}
	if o.Warning != "" {
		fmt.Fprintf(&b, "warning: %s\n", o.Warning)
	}
	for _, s := range o.Symbols {
		if s.EndLine > s.Line {
			fmt.Fprintf(&b, "L%d-%d %s %s", s.Line, s.EndLine, s.Kind, s.QualifiedName())
		} else {
			fmt.Fprintf(&b, "L%d %s %s", s.Line, s.Kind, s.QualifiedName())
		}
		if s.Signature != "" {
			b.WriteString(" | " + s.Signature)
		}
		b.WriteByte('\n')
	}
	if len(o.Symbols) == 0 {
		b.WriteString("(no symbols found)\n")
	}
	if o.Truncated {
		fmt.Fprintf(&b, "…[outline stopped at %d symbols or %d lines; read the rest with filesystem.read offset/limit]\n", outlineMaxSymbols, outlineMaxLines)
	}
	return b.String()
}

// --- Go -------------------------------------------------------------------

// outlineGo lists the declarations of a Go file. go/parser always returns a
// file when given source; on syntax errors it is the partial tree, and the
// first error is returned alongside its symbols.
func outlineGo(src []byte) (syms []Symbol, parseErr string) {
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, "src.go", src, parser.SkipObjectResolution)
	if perr != nil {
		parseErr = firstLine(perr.Error())
	}
	line := func(p token.Pos) int { return fset.Position(p).Line }
	text := func(from, to token.Pos) string {
		a, b := fset.Position(from).Offset, fset.Position(to).Offset
		if !from.IsValid() || !to.IsValid() || a < 0 || b > len(src) || a >= b {
			return ""
		}
		return compactSig(string(src[a:b]))
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			s := Symbol{Kind: "func", Name: d.Name.Name, Line: line(d.Pos()), EndLine: line(d.End())}
			if d.Recv != nil && len(d.Recv.List) > 0 {
				s.Kind, s.Container = "method", recvName(d.Recv.List[0].Type)
			}
			end := d.End()
			if d.Body != nil {
				end = d.Body.Lbrace
			}
			s.Signature = text(d.Pos(), end)
			syms = append(syms, s)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch sp := spec.(type) {
				case *ast.TypeSpec:
					kind := "type"
					switch sp.Type.(type) {
					case *ast.StructType:
						kind = "struct"
					case *ast.InterfaceType:
						kind = "interface"
					}
					s := Symbol{Kind: kind, Name: sp.Name.Name, Line: line(sp.Pos()), EndLine: line(sp.End())}
					if kind == "type" {
						s.Signature = text(sp.Pos(), sp.End())
					}
					syms = append(syms, s)
					if it, ok := sp.Type.(*ast.InterfaceType); ok && it.Methods != nil {
						for _, m := range it.Methods.List {
							for _, n := range m.Names {
								syms = append(syms, Symbol{Kind: "method", Name: n.Name, Container: sp.Name.Name, Line: line(n.Pos()), Signature: text(m.Pos(), m.End())})
							}
						}
					}
				case *ast.ValueSpec:
					for _, n := range sp.Names {
						if n.Name != "_" {
							syms = append(syms, Symbol{Kind: d.Tok.String(), Name: n.Name, Line: line(n.Pos())})
						}
					}
				}
			}
		}
	}
	return syms, parseErr
}

func recvName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return recvName(t.X)
	case *ast.ParenExpr:
		return recvName(t.X)
	case *ast.IndexExpr:
		return recvName(t.X)
	case *ast.IndexListExpr:
		return recvName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return "?"
}

// --- brace languages (TypeScript/JavaScript, Java) -------------------------

// braceRule recognises one declaration form on a single line.
type braceRule struct {
	re   *regexp.Regexp
	kind string
	// group is the submatch holding the name.
	group int
	// topLevel rules apply outside any block; member rules apply directly
	// inside a class body. container rules open a class body.
	topLevel, member, container bool
	// ctor matches only the enclosing class's name (Java constructors).
	ctor bool
}

const jsIdent = `[A-Za-z_$][\w$]*`

var tsRules = []braceRule{
	{re: regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:declare\s+)?(?:abstract\s+)?class\b\s*(` + jsIdent + `)?`), kind: "class", group: 1, topLevel: true, container: true},
	{re: regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:declare\s+)?(?:async\s+)?function\s*\*?\s*(` + jsIdent + `)`), kind: "func", group: 1, topLevel: true},
	{re: regexp.MustCompile(`^\s*(?:export\s+)?(?:declare\s+)?interface\s+(` + jsIdent + `)`), kind: "interface", group: 1, topLevel: true},
	{re: regexp.MustCompile(`^\s*(?:export\s+)?(?:declare\s+)?type\s+(` + jsIdent + `)\s*(?:<.*?>)?\s*=`), kind: "type", group: 1, topLevel: true},
	{re: regexp.MustCompile(`^\s*(?:export\s+)?(?:declare\s+)?(?:const\s+)?enum\s+(` + jsIdent + `)`), kind: "enum", group: 1, topLevel: true},
	{re: regexp.MustCompile(`^(?:export\s+)?(?:const|let|var)\s+(` + jsIdent + `)\s*(?::[^=]+)?=\s*(?:async\s+)?(?:function\b|(?:<[^>]*>\s*)?\([^)]*\)\s*(?::[^=]+)?=>|` + jsIdent + `\s*=>)`), kind: "func", group: 1, topLevel: true},
	{re: regexp.MustCompile(`^(?:export\s+)?(?:const|let|var)\s+(` + jsIdent + `)`), kind: "const", group: 1, topLevel: true},
	{re: regexp.MustCompile(`^\s+(?:(?:public|private|protected|static|readonly|override)\s+)*(#?` + jsIdent + `)\s*(?::[^=]+)?=\s*(?:async\s+)?(?:\([^)]*\)|` + jsIdent + `)\s*(?::[^=]+)?=>`), kind: "method", group: 1, member: true},
	{re: regexp.MustCompile(`^\s+(?:(?:public|private|protected|static|readonly|abstract|override|async|get|set)\s+)*\*?\s*(#?` + jsIdent + `)\s*(?:<[^>]*>)?\s*\(`), kind: "method", group: 1, member: true},
}

const javaMods = `(?:@\w+(?:\([^)]*\))?\s+)*(?:(?:public|protected|private|static|final|abstract|sealed|non-sealed|strictfp|synchronized|native|default|transient|volatile)\s+)*`

var javaRules = []braceRule{
	{re: regexp.MustCompile(`^\s*` + javaMods + `(class|interface|enum|record|@interface)\s+([A-Za-z_$][\w$]*)`), kind: "class", group: 2, topLevel: true, member: true, container: true},
	{re: regexp.MustCompile(`^\s*` + javaMods + `(?:<[^>]+>\s+)?([A-Za-z_$][\w$]*)\s*\(`), kind: "method", group: 1, member: true, ctor: true},
	{re: regexp.MustCompile(`^\s*` + javaMods + `(?:<[^>]+>\s+)?[\w$][\w$.]*(?:\s*<[^()]*>)?(?:\[\])*\s+([A-Za-z_$][\w$]*)\s*\(`), kind: "method", group: 1, member: true},
}

// notNames are keywords the member heuristics must not report as symbols.
var notNames = map[string]bool{"if": true, "for": true, "while": true, "switch": true, "catch": true, "return": true, "function": true,
	"new": true, "throw": true, "else": true, "do": true, "try": true, "case": true, "super": true, "this": true, "with": true, "typeof": true, "await": true, "yield": true}

type openBlock struct {
	name   string
	depth  int // brace depth of the block's body
	opened bool
}

// isCommentLine reports lines that are (the start or middle of) comments.
// A '*' line is a comment unless it starts a generator member ("*gen()").
func isCommentLine(trim string) bool {
	if strings.HasPrefix(trim, "//") || strings.HasPrefix(trim, "/*") {
		return true
	}
	return trim == "*" || strings.HasPrefix(trim, "* ") || strings.HasPrefix(trim, "*/") || strings.HasPrefix(trim, "**")
}

// outlineBraces finds declarations line by line while tracking brace depth.
// A declaration at column 0 that is only valid at top level resets the
// depth: the line is evidence that the tracking went wrong before it. Any
// reset, a negative depth or an unbalanced end produce a warning.
func outlineBraces(src []byte, rules []braceRule, bc braceCounter) (syms []Symbol, truncated bool, warning string) {
	var (
		depth   int
		classes []openBlock
		n       int
		lostAt  int
	)
	lost := func(line int) {
		if lostAt == 0 {
			lostAt = line
		}
	}
	sc := bufio.NewScanner(bytes.NewReader(src))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		n++
		if n > outlineMaxLines {
			return syms, true, desyncWarning(lostAt)
		}
		t := sc.Text()
		before := depth
		inLiteral := bc.inLiteral()
		d := bc.delta(t)
		depth = before + d
		trim := strings.TrimSpace(t)
		if inLiteral || trim == "" || isCommentLine(trim) {
			if depth < 0 {
				lost(n)
				depth = 0
			}
			continue
		}
		if before > 0 && t[0] != ' ' && t[0] != '\t' && matchesTopLevel(rules, t) {
			lost(n)
			before, depth, classes = 0, max(d, 0), nil
		}
		if depth < 0 {
			lost(n)
			depth = 0
		}
		var cls *openBlock
		if len(classes) > 0 && classes[len(classes)-1].depth == before {
			cls = &classes[len(classes)-1]
		}
		for _, r := range rules {
			if !(r.topLevel && before == 0) && !(r.member && cls != nil) {
				continue
			}
			m := r.re.FindStringSubmatch(t)
			if m == nil {
				continue
			}
			name := m[r.group]
			if r.container && (name == "" || name == "extends" || name == "implements") {
				name = "default" // anonymous class: "export default class extends Base"
			}
			if notNames[name] || (r.ctor && (cls == nil || name != cls.name)) {
				continue
			}
			s := Symbol{Kind: r.kind, Name: name, Line: n, Signature: compactSig(trim)}
			if cls != nil && before > 0 {
				s.Container = cls.name
			}
			syms = append(syms, s)
			if r.container && !(depth == before && strings.Contains(t, "{")) { // "class A {}" has no body to track
				classes = append(classes, openBlock{name: name, depth: before + 1})
			}
			break
		}
		for len(classes) > 0 {
			top := &classes[len(classes)-1]
			if depth >= top.depth {
				top.opened = true
				break
			}
			if !top.opened && depth == before {
				break // "class A" with the brace on a later line
			}
			classes = classes[:len(classes)-1]
		}
		if len(syms) > outlineMaxSymbols {
			return syms, true, desyncWarning(lostAt)
		}
	}
	if sc.Err() != nil {
		return syms, true, fmt.Sprintf("stopped reading at line %d: %v", n+1, sc.Err())
	}
	if depth != 0 || bc.inLiteral() {
		lost(n)
	}
	return syms, false, desyncWarning(lostAt)
}

func matchesTopLevel(rules []braceRule, line string) bool {
	for _, r := range rules {
		if r.topLevel && r.re.MatchString(line) {
			return true
		}
	}
	return false
}

func desyncWarning(line int) string {
	if line == 0 {
		return ""
	}
	return fmt.Sprintf("heuristic desync near line %d (unusual strings, regular expressions or braces); symbols may be missing or attached to the wrong class", line)
}

// braceCounter counts block delimiters on a line, skipping string literals,
// comments, JavaScript template and regular-expression literals and Java
// text blocks as far as a line scan can tell. Block comments, template
// literals and text blocks carry over to the next line.
type braceCounter struct {
	opens, closes string
	hashComments  bool // Python: '#' starts a comment, no block comments
	jsLiterals    bool // `template` literals and /regex/ literals
	textBlocks    bool // Java """text blocks"""

	inBlock, inTemplate, inTextBlock bool
}

// inLiteral reports whether the next line starts inside a comment or a
// multi-line literal.
func (c *braceCounter) inLiteral() bool { return c.inBlock || c.inTemplate || c.inTextBlock }

func (c *braceCounter) delta(line string) int {
	d := 0
	var quote byte
	var prev byte // last significant character outside literals
	for i := 0; i < len(line); i++ {
		ch := line[i]
		next := byte(0)
		if i+1 < len(line) {
			next = line[i+1]
		}
		switch {
		case c.inBlock:
			if ch == '*' && next == '/' {
				c.inBlock = false
				i++
			}
			continue
		case c.inTextBlock:
			if ch == '\\' {
				i++
			} else if strings.HasPrefix(line[i:], `"""`) {
				c.inTextBlock = false
				i += 2
			}
			continue
		case c.inTemplate:
			if ch == '\\' {
				i++
			} else if ch == '`' {
				c.inTemplate, prev = false, '`'
			}
			continue
		case quote != 0:
			if ch == '\\' {
				i++
			} else if ch == quote {
				quote, prev = 0, ch
			}
			continue
		}
		switch {
		case ch == ' ' || ch == '\t':
			continue
		case c.hashComments && ch == '#':
			return d
		case !c.hashComments && ch == '/' && next == '/':
			return d
		case !c.hashComments && ch == '/' && next == '*':
			c.inBlock = true
			i++
			continue
		case c.jsLiterals && ch == '/' && regexAllowed(prev, line[:i]):
			if end := regexEnd(line, i); end > i {
				i, prev = end, '/'
				continue
			}
		case c.jsLiterals && ch == '`':
			c.inTemplate = true
			continue
		case c.textBlocks && strings.HasPrefix(line[i:], `"""`):
			c.inTextBlock = true
			i += 2
			continue
		case ch == '"' || ch == '\'' || ch == '`':
			quote = ch
			continue
		case strings.IndexByte(c.opens, ch) >= 0:
			d++
		case strings.IndexByte(c.closes, ch) >= 0:
			d--
		}
		prev = ch
	}
	return d
}

// regexAllowed reports whether a '/' after prev starts a regular-expression
// literal rather than a division (JavaScript's own rule, approximated).
func regexAllowed(prev byte, before string) bool {
	if prev == 0 || strings.IndexByte("(,=:[!&|?{};+-*%<>~^", prev) >= 0 {
		return true
	}
	b := strings.TrimRight(before, " \t")
	for _, kw := range []string{"return", "typeof", "case", "do", "else", "in", "of", "void", "yield", "await"} {
		if strings.HasSuffix(b, kw) && (len(b) == len(kw) || !isIdentByte(b[len(b)-len(kw)-1])) {
			return true
		}
	}
	return false
}

// regexEnd returns the index of the '/' closing the regular expression
// starting at i, or i when the line does not close it.
func regexEnd(line string, i int) int {
	inClass := false
	for j := i + 1; j < len(line); j++ {
		switch line[j] {
		case '\\':
			j++
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '/':
			if !inClass {
				return j
			}
		}
	}
	return i
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '$' || (c|0x20 >= 'a' && c|0x20 <= 'z') || (c >= '0' && c <= '9')
}

// --- Python ----------------------------------------------------------------

var (
	pyDef   = regexp.MustCompile(`^(\s*)(?:async\s+)?def\s+([A-Za-z_]\w*)\s*\(`)
	pyClass = regexp.MustCompile(`^(\s*)class\s+([A-Za-z_]\w*)`)
	pyConst = regexp.MustCompile(`^([A-Z][A-Z0-9_]*)\s*(?::[^=]+)?=[^=]`)
)

type pyBlock struct {
	name    string
	indent  int
	isClass bool
}

func outlinePython(src []byte) (syms []Symbol, truncated bool, warning string) {
	var (
		bc     = braceCounter{opens: "([{", closes: ")]}", hashComments: true}
		parens int
		stack  []pyBlock
		inDoc  string
		n      int
	)
	sc := bufio.NewScanner(bytes.NewReader(src))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		n++
		if n > outlineMaxLines {
			return syms, true, ""
		}
		t := sc.Text()
		if inDoc != "" {
			if strings.Count(t, inDoc)%2 == 1 {
				inDoc = ""
			}
			continue
		}
		before := parens
		parens += bc.delta(t)
		if parens < 0 {
			warning = desyncWarning(n)
			parens = 0
		}
		for _, q := range []string{`"""`, `'''`} {
			if strings.Count(t, q)%2 == 1 {
				inDoc = q
				break
			}
		}
		trim := strings.TrimSpace(t)
		if before > 0 || trim == "" || strings.HasPrefix(trim, "#") {
			continue // continuation lines and comments do not change blocks
		}
		indent := len(t) - len(strings.TrimLeft(t, " \t"))
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		var parent *pyBlock
		if len(stack) > 0 {
			parent = &stack[len(stack)-1]
		}
		if m := pyClass.FindStringSubmatch(t); m != nil {
			if parent == nil || parent.isClass {
				s := Symbol{Kind: "class", Name: m[2], Line: n, Signature: compactSig(trim)}
				if parent != nil {
					s.Container = parent.name
				}
				syms = append(syms, s)
			}
			stack = append(stack, pyBlock{name: m[2], indent: indent, isClass: true})
		} else if m := pyDef.FindStringSubmatch(t); m != nil {
			switch {
			case parent == nil:
				syms = append(syms, Symbol{Kind: "func", Name: m[2], Line: n, Signature: compactSig(trim)})
			case parent.isClass:
				syms = append(syms, Symbol{Kind: "method", Name: m[2], Container: parent.name, Line: n, Signature: compactSig(trim)})
			}
			stack = append(stack, pyBlock{name: m[2], indent: indent})
		} else if m := pyConst.FindStringSubmatch(t); m != nil && indent == 0 {
			syms = append(syms, Symbol{Kind: "const", Name: m[1], Line: n})
		}
		if len(syms) > outlineMaxSymbols {
			return syms, true, warning
		}
	}
	if sc.Err() != nil {
		return syms, true, fmt.Sprintf("stopped reading at line %d: %v", n+1, sc.Err())
	}
	if warning == "" && (parens != 0 || inDoc != "") {
		warning = desyncWarning(n)
	}
	return syms, false, warning
}

// --- helpers ----------------------------------------------------------------

func compactSig(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimSuffix(strings.TrimSuffix(s, "{"), " ")
	if len(s) > 160 {
		s = truncateUTF8(s, 160) + "…"
	}
	return s
}
