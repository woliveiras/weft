package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
)

// symbolsOf renders an outline as "kind qualified@line" for compact asserts.
func symbolsOf(t *testing.T, name, src string) []string {
	t.Helper()
	o, err := OutlineSource(name, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(o.Symbols))
	for i, s := range o.Symbols {
		out[i] = fmt.Sprintf("%s %s@%d", s.Kind, s.QualifiedName(), s.Line)
	}
	return out
}

func assertSymbols(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("symbols\n got: %q\nwant: %q", got, want)
	}
}

func TestOutlineGo(t *testing.T) {
	src := `package demo

import "fmt"

const Max, min = 10, 1

var _ = fmt.Sprint

type Store struct {
	items map[string]int
}

type Getter interface {
	Get(key string) (int, error)
}

type ID = string

func New() *Store { return &Store{} }

func (s *Store) Get(key string) (int, error) {
	return s.items[key], nil
}

func (l List[T]) Len() int { return 0 }
`
	assertSymbols(t, symbolsOf(t, "demo.go", src), []string{
		"const Max@5", "const min@5", "struct Store@9", "interface Getter@13", "method Getter.Get@14",
		"type ID@17", "func New@19", "method Store.Get@21", "method List.Len@25",
	})
	o, _ := OutlineSource("demo.go", []byte(src))
	get := o.Symbols[7]
	if get.EndLine != 23 || get.Signature != "func (s *Store) Get(key string) (int, error)" {
		t.Fatalf("method details: %+v", get)
	}
	if o.Symbols[5].Signature != "ID = string" {
		t.Fatalf("alias signature: %q", o.Symbols[5].Signature)
	}
}

func TestOutlineGoSyntaxErrorKeepsParsedPart(t *testing.T) {
	o, err := OutlineSource("bad.go", []byte("package x\n\nfunc Good() {}\n\nfunc Bad( {\n"))
	if err != nil {
		t.Fatal(err)
	}
	if o.ParseError == "" || len(o.Symbols) == 0 || o.Symbols[0].Name != "Good" {
		t.Fatalf("partial outline: %+v", o)
	}
	o, err = OutlineSource("notgo.go", []byte("not go at all"))
	if err != nil || o.ParseError == "" || len(o.Symbols) != 0 {
		t.Fatalf("a file without a package clause reports the syntax error: %v %+v", err, o)
	}
}

func TestOutlineTypeScript(t *testing.T) {
	src := `import { x } from "./x";

export interface Props { name: string }
export type Mode = "a" | "b";
export enum Color { Red, Green }
const LIMIT = 3;

export class Store extends Base {
  private items: string[] = [];
  static create(): Store {
    if (x) { return new Store(); }
    return new Store();
  }
  async load(id: string): Promise<void> {
    const s = "}{";
  }
  handle = (e: Event) => {
    function inner() {}
  };
}

// function commented() {}
export default function render(props: Props) {
  function helper() {}
  return null;
}

export const useThing = async (a: number) => {
  return a;
};
class Empty {}
function after() {}
`
	assertSymbols(t, symbolsOf(t, "store.ts", src), []string{
		"interface Props@3", "type Mode@4", "enum Color@5", "const LIMIT@6",
		"class Store@8", "method Store.create@10", "method Store.load@14", "method Store.handle@17",
		"func render@23", "func useThing@28", "class Empty@31", "func after@32",
	})
}

func TestOutlinePython(t *testing.T) {
	src := `"""Module doc.

def not_a_function():
"""
MAX_SIZE = 10


class Repo(Base):
    """Repo doc."""

    def __init__(self, path):
        self.path = path

    async def fetch(self,
        ref):
        def inner():
            pass
        return inner

    class Meta:
        pass


def main():
    print(call(
  1))


async def run():
    pass
`
	assertSymbols(t, symbolsOf(t, "repo.py", src), []string{
		"const MAX_SIZE@5", "class Repo@8", "method Repo.__init__@11", "method Repo.fetch@14",
		"class Repo.Meta@20", "func main@24", "func run@29",
	})
}

func TestOutlineJava(t *testing.T) {
	src := `package demo;

import java.util.List;

/* class Hidden { } */
@Service
public final class OrderService implements Service {
    private final Repo repo = Repo.create();

    public OrderService(Repo repo) {
        this.repo = repo;
    }

    @Override
    public <T> List<T> find(String id, int limit) {
        if (id == null) { return null; }
        return repo.find(id);
    }

    static Map<String, Integer> counts() { return null; }

    enum Status { OPEN, CLOSED }
}

interface Repo {
    List<String> find(String id);
}
`
	assertSymbols(t, symbolsOf(t, "OrderService.java", src), []string{
		"class OrderService@7", "method OrderService.OrderService@10", "method OrderService.find@15",
		"method OrderService.counts@20", "class OrderService.Status@22", "class Repo@25", "method Repo.find@26",
	})
}

func TestOutlineUnsupportedAndBounds(t *testing.T) {
	if _, err := OutlineSource("README.md", []byte("# x")); !errors.Is(err, ErrUnsupportedLanguage) {
		t.Fatalf("want ErrUnsupportedLanguage, got %v", err)
	}
	var b strings.Builder
	for i := 0; i < outlineMaxSymbols+50; i++ {
		fmt.Fprintf(&b, "def f%d():\n    pass\n", i)
	}
	o, err := OutlineSource("many.py", []byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if !o.Truncated || len(o.Symbols) != outlineMaxSymbols {
		t.Fatalf("symbol bound: truncated=%v n=%d", o.Truncated, len(o.Symbols))
	}
	if !strings.Contains(formatOutline(o), "outline stopped") {
		t.Fatal("truncation must be explained")
	}
}

func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func codeRepo(t *testing.T) string {
	dir := t.TempDir()
	files := map[string]string{
		"core/store.go":         "package core\n\ntype Store struct{}\n\nfunc (s *Store) Save() error { return nil }\n\nfunc rarelyUsed() {}\n",
		"web/src/app.ts":        "export function renderApp() {}\n",
		"secrets/keys.go":       "package secrets\n\nfunc SecretKey() string { return \"\" }\n",
		"vendor/lib/lib.go":     "package lib\n\nfunc Vendored() {}\n",
		"web/node_modules/x.js": "function fromDependency() {}\n",
		".git/hooks/pre.py":     "def hook(): pass\n",
		"docs/guide.md":         "# Store\n",
		"core/store_test.go":    "package core\n\nfunc TestStoreThing() {}\n",
		"scripts/tool.py":       "def run_tool():\n    pass\n",
		"service/Main.java":     "class Main {\n    void start() {}\n}\n",
		"core/zzz_unrelated.go": "package core\n\nfunc Unrelated() {}\n",
	}
	for i := 0; i < 5; i++ {
		files[fmt.Sprintf("api/h%d.go", i)] = fmt.Sprintf("package api\n\nfunc Handler%d(s *Store) { s.Save() }\n", i)
	}
	writeTree(t, dir, files)
	return dir
}

func TestBuildCodeMapRanksAndSkipsIgnoredTrees(t *testing.T) {
	dir := codeRepo(t)
	m, err := BuildCodeMap(context.Background(), dir, domain.ToolGrant{}, CodeMapOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, hidden := range []string{"Vendored", "fromDependency", "hook", "guide.md"} {
		if strings.Contains(m.Text, hidden) {
			t.Fatalf("map must skip %s:\n%s", hidden, m.Text)
		}
	}
	for _, want := range []string{"core/\n", "store.go", "struct Store L3", "method Store.Save L5", "renderApp", "run_tool", "Main.start", "Directories: "} {
		if !strings.Contains(m.Text, want) {
			t.Fatalf("map lacks %q:\n%s", want, m.Text)
		}
	}
	// Store is referenced by five other files, so it is the first file of core/.
	core := m.Text[strings.Index(m.Text, "core/\n"):]
	if strings.Index(core, "store.go") > strings.Index(core, "zzz_unrelated.go") {
		t.Fatalf("referenced file must rank first:\n%s", core)
	}
	if strings.Index(core, "Store L3") > strings.Index(core, "rarelyUsed") {
		t.Fatalf("referenced symbol must rank first:\n%s", core)
	}
	if m.Files != 12 || m.Truncated || m.Shown != 12 {
		t.Fatalf("counts: %+v", m)
	}
}

func TestBuildCodeMapRespectsBudget(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{}
	for i := 0; i < 200; i++ {
		files[fmt.Sprintf("pkg%d/file%d.go", i%20, i)] = fmt.Sprintf("package p\n\nfunc LongFunctionName%d() {}\n\ntype SomeType%d struct{}\n", i, i)
	}
	writeTree(t, dir, files)
	m, err := BuildCodeMap(context.Background(), dir, domain.ToolGrant{}, CodeMapOptions{MaxTokens: 300})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Text) > 300*4 {
		t.Fatalf("map of %d characters exceeds the 300-token budget", len(m.Text))
	}
	if !m.Truncated || m.Shown == 0 || m.Shown >= 200 || !strings.Contains(m.Text, "more files not shown") {
		t.Fatalf("budget: %+v\n%s", m, m.Text)
	}
	big, _ := BuildCodeMap(context.Background(), dir, domain.ToolGrant{}, CodeMapOptions{MaxTokens: 100000})
	if big.Shown != 200 || big.Truncated {
		t.Fatalf("a large budget (clamped to %d) should show all files: %+v", codeMapMaxTokens, big)
	}
}

func TestBuildCodeMapErrors(t *testing.T) {
	dir := codeRepo(t)
	ctx := context.Background()
	if _, err := BuildCodeMap(ctx, dir, domain.ToolGrant{Paths: []string{"core/**"}}, CodeMapOptions{Path: "secrets"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("ungranted path: %v", err)
	}
	if _, err := BuildCodeMap(ctx, dir, domain.ToolGrant{}, CodeMapOptions{Path: "../"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("escape: %v", err)
	}
	if _, err := BuildCodeMap(ctx, dir, domain.ToolGrant{}, CodeMapOptions{Path: "core/store.go"}); err == nil || !strings.Contains(err.Error(), "code.outline") {
		t.Fatalf("file path: %v", err)
	}
	if _, err := BuildCodeMap(ctx, dir, domain.ToolGrant{}, CodeMapOptions{Path: "missing"}); err == nil {
		t.Fatal("missing directory must fail")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := BuildCodeMap(cancelled, dir, domain.ToolGrant{}, CodeMapOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
	m, err := BuildCodeMap(ctx, t.TempDir(), domain.ToolGrant{}, CodeMapOptions{})
	if err != nil || !strings.Contains(m.Text, "no Go, TypeScript/JavaScript, Python or Java files") {
		t.Fatalf("empty repository: %v %+v", err, m)
	}
}

func TestBuildCodeMapForGoesThroughTheGateway(t *testing.T) {
	dir := codeRepo(t)
	ctx := context.Background()
	aud := &captureAuditor{}
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, aud)
	p := principal(dir, domain.ToolGrant{Name: "filesystem.read", Paths: []string{"core/**", "api/**"}})
	m, err := BuildCodeMapFor(ctx, gw, p, CodeMapOptions{MaxTokens: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(m.Text, "SecretKey") || strings.Contains(m.Text, "renderApp") || !strings.Contains(m.Text, "Store") || m.Files != 8 {
		t.Fatalf("prompt map must follow the grants: %+v\n%s", m, m.Text)
	}
	if got := strings.Join(aud.types(), ","); got != string(domain.EvToolInvoked)+","+string(domain.EvToolCompleted) {
		t.Fatalf("prompt map must be audited like a call: %s", got)
	}
	if _, err := BuildCodeMapFor(ctx, gw, principal(dir, domain.ToolGrant{Name: "shell.exec"}), CodeMapOptions{}); !errors.Is(err, ErrDenied) {
		t.Fatalf("no read grant: %v", err)
	}
	if _, err := BuildCodeMapFor(ctx, gw, p, CodeMapOptions{Path: "missing"}); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("tool errors must surface: %v", err)
	}
	if _, err := BuildCodeMapFor(ctx, nil, p, CodeMapOptions{}); err == nil {
		t.Fatal("a nil gateway must fail")
	}
}

func TestBuildCodeMapForNeverWaitsForApproval(t *testing.T) {
	dir := codeRepo(t)
	ap := &fakeApprover{status: domain.ApprovalApproved}
	aud := &captureAuditor{}
	gw := NewGateway(Builtin(BuiltinConfig{}), ap, aud)
	p := principal(dir, domain.ToolGrant{Name: "filesystem.read", RequiresApproval: true})
	if _, err := BuildCodeMapFor(context.Background(), gw, p, CodeMapOptions{}); !errors.Is(err, ErrDenied) || !strings.Contains(err.Error(), "non-interactive") {
		t.Fatalf("approval-gated prompt map must fail closed: %v", err)
	}
	if len(ap.got) != 0 {
		t.Fatal("building a prompt must not ask a person")
	}
	if got := strings.Join(aud.types(), ","); got != string(domain.EvToolDenied) {
		t.Fatalf("the refusal must be audited: %s", got)
	}
	// The agent itself can still call code.map and get it approved.
	if r, err := call(t, gw, p, "code.map", map[string]any{}); err != nil || r.IsError || len(ap.got) != 1 {
		t.Fatalf("interactive call: %v %+v", err, r)
	}
}

func setLimit[T any](t *testing.T, v *T, to T) {
	old := *v
	*v = to
	t.Cleanup(func() { *v = old })
}

func TestBuildCodeMapScanLimitsAndSkips(t *testing.T) {
	dir := codeRepo(t)
	ctx := context.Background()
	setLimit(t, &codeMapMaxEntries, 5)
	m, err := BuildCodeMap(ctx, dir, domain.ToolGrant{}, CodeMapOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !m.ScanLimited || !m.Truncated || !strings.Contains(m.Text, "scan stopped at 5 entries") {
		t.Fatalf("entry limit: %+v\n%s", m, m.Text)
	}
	codeMapMaxEntries = 50000
	setLimit(t, &codeMapMaxFileBytes, 70)
	m, err = BuildCodeMap(ctx, dir, domain.ToolGrant{}, CodeMapOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if m.Skipped[skipTooLarge] == 0 || !m.Truncated || !strings.Contains(m.Text, "too large") || strings.Contains(m.Text, "struct Store") {
		t.Fatalf("large files must be counted, not silently dropped: %+v\n%s", m, m.Text)
	}
	if strings.Index(m.Text, "skipped:") > strings.Index(m.Text, "Directories:") {
		t.Fatalf("notices come before the content:\n%s", m.Text)
	}
}

func TestBuildCodeMapUnreadableEntries(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads files regardless of permissions")
	}
	dir := codeRepo(t)
	ctx := context.Background()
	locked := filepath.Join(dir, "core", "store.go")
	os.Chmod(locked, 0)
	t.Cleanup(func() { os.Chmod(locked, 0o644) })
	m, err := BuildCodeMap(ctx, dir, domain.ToolGrant{}, CodeMapOptions{})
	if err != nil || m.Skipped[skipUnreadable] != 1 || !strings.Contains(m.Text, "1 unreadable") {
		t.Fatalf("unreadable file: %v %+v", err, m)
	}
	root := filepath.Join(dir, "api")
	os.Chmod(root, 0)
	t.Cleanup(func() { os.Chmod(root, 0o755) })
	if _, err := BuildCodeMap(ctx, dir, domain.ToolGrant{}, CodeMapOptions{Path: "api"}); err == nil {
		t.Fatal("an unreadable root must be an error, not an empty map")
	}
}

func TestBuildCodeMapIgnoresBuildOutputsAndRanksHonestly(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		".gitignore":            "# outputs\ngenerated_out/\n*.gen.go\n!keep.go\n/docs/x/\n",
		"src/main.go":           "package main\n\ntype Engine struct{}\n\nfunc (e *Engine) String() string { return \"\" }\n",
		"src/zz.gen.go":         "package main\n\nfunc IgnoredByGitignore() {}\n",
		"src/gen.go":            "// Code generated by tool. DO NOT EDIT.\n\npackage main\n\ntype Wire struct{}\n",
		"src/a.py":              "class Thing:\n    def __init__(self):\n        pass\n",
		"web/app.min.js":        "function minified(){}\n",
		"generated_out/x.go":    "package x\n\nfunc InGitignoredDir() {}\n",
		"venv/pyvenv.cfg":       "home = /usr\n",
		"venv/lib/site.py":      "def from_venv(): pass\n",
		"bower_components/b.js": "function fromBower() {}\n",
	}
	for _, d := range []string{"build", "dist", "target", "out", "coverage"} {
		files[d+"/x.go"] = "package x\n\nfunc From_" + d + "() {}\n"
	}
	for i := 0; i < 4; i++ {
		files[fmt.Sprintf("src/user%d.go", i)] = fmt.Sprintf("package main\n\nfunc use%d(e *Engine, w *Wire) { _ = e.String(); _ = Thing; _ = __init__ }\n", i)
		files[fmt.Sprintf("other/s%d.go", i)] = fmt.Sprintf("package other\n\ntype T%d struct{}\n\nfunc (T%d) String() string { return \"\" }\n", i, i)
	}
	writeTree(t, dir, files)
	m, err := BuildCodeMap(context.Background(), dir, domain.ToolGrant{}, CodeMapOptions{MaxTokens: 2000})
	if err != nil {
		t.Fatal(err)
	}
	for _, hidden := range []string{"IgnoredByGitignore", "minified", "InGitignoredDir", "from_venv", "fromBower", "From_build", "From_dist", "From_target", "From_out", "From_coverage"} {
		if strings.Contains(m.Text, hidden) {
			t.Fatalf("map must skip %s:\n%s", hidden, m.Text)
		}
	}
	src := m.Text[strings.Index(m.Text, "src/\n"):]
	// Engine and Wire are both used by four files, but gen.go is generated.
	if strings.Index(src, "main.go") > strings.Index(src, "gen.go") {
		t.Fatalf("generated files rank lower:\n%s", src)
	}
	// Engine.String shares its name with four other definitions, so its
	// mentions are divided and the type ranks first.
	if strings.Index(src, "struct Engine") > strings.Index(src, "Engine.String") {
		t.Fatalf("names defined in many files rank lower:\n%s", src)
	}
}

func TestDefaultCodeMapFitsTheSmallestResultCap(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{}
	for i := 0; i < 300; i++ {
		files[fmt.Sprintf("pkg%d/file%d.go", i%30, i)] = fmt.Sprintf("package p\n\nfunc LongFunctionName%d() {}\n", i)
	}
	writeTree(t, dir, files)
	m, err := BuildCodeMap(context.Background(), dir, domain.ToolGrant{}, CodeMapOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// runtime/native caps one result at about 4 800 characters for an 8k window.
	if len(m.Text) > 4000 || !strings.Contains(m.Text[:300], "more files not shown") {
		t.Fatalf("default map of %d characters:\n%s", len(m.Text), m.Text)
	}
}

func TestSearcherContext(t *testing.T) {
	re := mustRe(t, "hit")
	s := &searcher{re: re, context: 1, max: 10}
	s.file("a.txt", strings.NewReader("one\nhit 1\ntwo\nthree\nfour\nhit 2\nhit 3\nfive\n"))
	s.file("b.txt", strings.NewReader("hit b\n"))
	want := []string{
		"a.txt-1- one", "a.txt:2: hit 1", "a.txt-3- two", "--",
		"a.txt-5- four", "a.txt:6: hit 2", "a.txt:7: hit 3", "a.txt-8- five", "--",
		"b.txt:1: hit b",
	}
	if strings.Join(s.out, "\n") != strings.Join(want, "\n") || s.matches != 4 {
		t.Fatalf("context output:\n%s", strings.Join(s.out, "\n"))
	}
	s = &searcher{re: re, max: 1}
	if s.file("a.txt", strings.NewReader("hit\nhit\n")) || !s.more || len(s.out) != 1 {
		t.Fatalf("limit: %+v", s)
	}
	s = &searcher{re: re, max: 5}
	if !s.file("bin", strings.NewReader("hit\x00\nhit\n")) || s.matches != 0 {
		t.Fatal("binary files must be skipped")
	}
}

// --- integration through the gateway ------------------------------------

func TestCodeToolsThroughGatewayWithGrants(t *testing.T) {
	dir := codeRepo(t)
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, nil)
	p := principal(dir, domain.ToolGrant{Name: "filesystem.read", Paths: []string{"core/**", "api/**", "docs/**"}})

	r := run(t, gw, p, "code.outline", map[string]any{"path": "core/store.go"})
	if !strings.Contains(r.Content, "L3 struct Store") || !strings.Contains(r.Content, "L5 method Store.Save | func (s *Store) Save() error") || len(r.Data) == 0 {
		t.Fatalf("outline: %q", r.Content)
	}
	if _, err := call(t, gw, p, "code.outline", map[string]any{"path": "secrets/keys.go"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("outline outside the grant: %v", err)
	}
	for _, bad := range []map[string]any{{"path": "docs/guide.md"}, {"path": "core"}, {"path": "core/missing.go"}} {
		if r, err := call(t, gw, p, "code.outline", bad); err != nil || !r.IsError {
			t.Fatalf("outline %v must be a tool error, got %v %+v", bad, err, r)
		}
	}

	r = run(t, gw, p, "code.map", map[string]any{"maxTokens": 500})
	if strings.Contains(r.Content, "SecretKey") || strings.Contains(r.Content, "renderApp") || !strings.Contains(r.Content, "Handler0") {
		t.Fatalf("map must follow the grants: %q", r.Content)
	}
	if _, err := call(t, gw, p, "code.map", map[string]any{"path": "secrets"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("map outside the grant: %v", err)
	}

	noRead := principal(dir, domain.ToolGrant{Name: "shell.exec"})
	if _, err := call(t, gw, noRead, "code.map", map[string]any{}); !errors.Is(err, ErrDenied) {
		t.Fatalf("code.map needs filesystem.read: %v", err)
	}
	var names []string
	for _, s := range gw.Registry.Schemas(p) {
		names = append(names, s.Name)
	}
	if got := strings.Join(names, ","); !strings.Contains(got, "code.map") || !strings.Contains(got, "code.outline") {
		t.Fatalf("filesystem.read must expose the code tools: %s", got)
	}
}

func TestFilesystemSearchModesThroughGateway(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"src/a.go":            "package a\n\n// call a.b(x)\nfunc f() {\n\ta.b(x)\n}\n",
		"vendor/dep/dep.go":   "package dep // a.b(x)\n",
		"node_modules/m/i.js": "a.b(x)\n",
		"secrets/s.go":        "a.b(x)\n",
	})
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, nil)
	p := principal(dir, domain.ToolGrant{Name: "filesystem.read", Paths: []string{"src/**", "vendor/**", "node_modules/**"}})

	r := run(t, gw, p, "filesystem.search", map[string]any{"pattern": "a.b(x)", "literal": true})
	if r.Content != "src/a.go:3: // call a.b(x)\nsrc/a.go:5: \ta.b(x)" {
		t.Fatalf("literal search must skip vendored and dependency code: %q", r.Content)
	}
	r = run(t, gw, p, "filesystem.search", map[string]any{"pattern": "a.b(x)", "literal": true, "path": "vendor"})
	if !strings.Contains(r.Content, "vendor/dep/dep.go:1:") {
		t.Fatalf("an explicit path searches inside vendor: %q", r.Content)
	}
	r = run(t, gw, p, "filesystem.search", map[string]any{"pattern": `^func`, "context": 1})
	if r.Content != "src/a.go-3- // call a.b(x)\nsrc/a.go:4: func f() {\nsrc/a.go-5- \ta.b(x)" {
		t.Fatalf("context: %q", r.Content)
	}
	r = run(t, gw, p, "filesystem.search", map[string]any{"pattern": "a", "maxResults": 1})
	if !r.Truncated || !strings.Contains(r.Content, "stopped at 1 matches") {
		t.Fatalf("limit: %+v", r)
	}
	for _, bad := range []map[string]any{{"pattern": "a.b(x"}, {"pattern": ""}, {"pattern": "a", "context": 11}} {
		r, err := call(t, gw, p, "filesystem.search", bad)
		if err != nil || !r.IsError {
			t.Fatalf("search %v must be a tool error, got %v %+v", bad, err, r)
		}
	}
	if r, _ := call(t, gw, p, "filesystem.search", map[string]any{"pattern": "a.b(x"}); !strings.Contains(r.Content, "literal") {
		t.Fatalf("an invalid regexp should suggest literal: %q", r.Content)
	}
	if _, err := call(t, gw, p, "filesystem.search", map[string]any{"pattern": "a", "path": "secrets"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("search outside the grant: %v", err)
	}
}

func mustRe(t *testing.T, expr string) *regexp.Regexp {
	t.Helper()
	re, err := regexp.Compile(expr)
	if err != nil {
		t.Fatal(err)
	}
	return re
}

func TestOutlineBraceHeuristicsHardCases(t *testing.T) {
	ts := "export const quote = /[\"']/g;\n" +
		"export const brace = /\\{/;\n" +
		"const tpl = `line {\n" +
		"  still { in template\n" +
		"`;\n" +
		"export default class extends Base {\n" +
		"  *gen() {\n" +
		"    yield 1;\n" +
		"  }\n" +
		"  /**\n" +
		"   * docs {\n" +
		"   */\n" +
		"  run() {}\n" +
		"}\n" +
		"export type Box<T = string> = { value: T };\n" +
		"function last() {}\n"
	assertSymbols(t, symbolsOf(t, "hard.ts", ts), []string{
		"const quote@1", "const brace@2", "const tpl@3", "class default@6", "method default.gen@7",
		"method default.run@13", "type Box@15", "func last@16",
	})
	if o, _ := OutlineSource("hard.ts", []byte(ts)); o.Warning != "" {
		t.Fatalf("balanced input must not warn: %s", o.Warning)
	}

	java := "class Box {\n" +
		"    String text = \"\"\"\n" +
		"        { not a block\n" +
		"        \"\"\";\n" +
		"    public <T> Box(T value) {}\n" +
		"    char c = '{';\n" +
		"    void after() {}\n" +
		"}\n"
	assertSymbols(t, symbolsOf(t, "Box.java", java), []string{"class Box@1", "method Box.Box@5", "method Box.after@7"})
}

func TestOutlineHeuristicDesyncIsReported(t *testing.T) {
	// "a /{/" reads as a division, so the brace is counted and the depth
	// stays open; the column-0 declaration resets it and the outline warns.
	src := "const x = a /{/ 2;\nexport function after() {}\n"
	o, err := OutlineSource("d.js", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Symbols) != 2 || o.Symbols[1].Name != "after" || !strings.Contains(o.Warning, "heuristic desync near line 2") || !o.Approximate() {
		t.Fatalf("reset and warning: %+v", o)
	}
	if !strings.Contains(formatOutline(o), "warning: heuristic desync") {
		t.Fatal("the warning must reach the model")
	}
	o, _ = OutlineSource("e.ts", []byte("}\nfunction f() {}\n"))
	if o.Warning == "" {
		t.Fatal("a negative depth must warn")
	}
	o, _ = OutlineSource("u.java", []byte("class A {\n    void f() {\n"))
	if !strings.Contains(o.Warning, "line 2") {
		t.Fatalf("an unbalanced end must warn: %+v", o)
	}
	o, _ = OutlineSource("p.py", []byte("def f(\n"))
	if o.Warning == "" {
		t.Fatal("an unclosed Python bracket must warn")
	}
}

func TestReadBoundedAndOutlineLimits(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "big.go")
	os.WriteFile(p, []byte(strings.Repeat("x", 11)), 0o644)
	if _, err := readBounded(p, 10); !errors.Is(err, errTooLarge) {
		t.Fatalf("want errTooLarge, got %v", err)
	}
	if b, err := readBounded(p, 11); err != nil || len(b) != 11 {
		t.Fatalf("exact size: %v", err)
	}
	if _, err := readBounded(filepath.Join(dir, "missing"), 10); err == nil {
		t.Fatal("missing file must fail")
	}
	long := "def f():\n    x = '" + strings.Repeat("a", 2<<20) + "'\ndef g():\n    pass\n"
	o, err := OutlineSource("long.py", []byte(long))
	if err != nil || !o.Truncated || !strings.Contains(o.Warning, "stopped reading at line 2") {
		t.Fatalf("an over-long line must be reported: %v %+v", err, o.Warning)
	}
}

func TestFilesystemSearchReportsSkipsThroughGateway(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"src/long.txt":        "needle 1\n" + strings.Repeat("a", 1<<20+100<<10) + "\nneedle 2\n",
		"node_modules/m/i.js": "only here\n",
		"vendor/v.go":         "only here\n",
	})
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, nil)
	p := principal(dir, domain.ToolGrant{Name: "filesystem.read"})
	r := run(t, gw, p, "filesystem.search", map[string]any{"pattern": "needle", "literal": true})
	if !strings.Contains(r.Content, "src/long.txt:1: needle 1") || !strings.Contains(r.Content, "searched only up to line 1 (a line is longer than 1 MiB)") {
		t.Fatalf("an over-long line must be reported: %q", r.Content)
	}
	r = run(t, gw, p, "filesystem.search", map[string]any{"pattern": "only here", "literal": true})
	if !strings.Contains(r.Content, "no matches (not searched: node_modules/, vendor/; set path inside one to search it)") {
		t.Fatalf("empty results must name the skipped directories: %q", r.Content)
	}
	narrow := principal(dir, domain.ToolGrant{Name: "filesystem.read", Paths: []string{"src/**"}})
	if r := run(t, gw, narrow, "filesystem.search", map[string]any{"pattern": "only here", "literal": true}); strings.Contains(r.Content, "node_modules") || strings.Contains(r.Content, "vendor") || !strings.HasPrefix(r.Content, "no matches") {
		t.Fatalf("ungranted directories must not be named: %q", r.Content)
	}
}

func TestCodeToolsDoNotFollowSymlinksOutOfTheWorkspace(t *testing.T) {
	outside := t.TempDir()
	writeTree(t, outside, map[string]string{"secret.go": "package s\n\nfunc OutsideSecret() {} // token\n"})
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"src/a.go": "package a\n\nfunc Inside() {} // token\n"})
	if err := os.Symlink(filepath.Join(outside, "secret.go"), filepath.Join(dir, "src", "link.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "linkdir")); err != nil {
		t.Fatal(err)
	}
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, nil)
	p := principal(dir, domain.ToolGrant{Name: "filesystem.read"})
	r := run(t, gw, p, "code.map", map[string]any{})
	if strings.Contains(r.Content, "OutsideSecret") || !strings.Contains(r.Content, "Inside") {
		t.Fatalf("code.map followed a symlink: %q", r.Content)
	}
	for _, path := range []string{"src/link.go", "linkdir/secret.go"} {
		if _, err := call(t, gw, p, "code.outline", map[string]any{"path": path}); !errors.Is(err, ErrDenied) {
			t.Fatalf("code.outline %s must be denied, got %v", path, err)
		}
	}
	r = run(t, gw, p, "filesystem.search", map[string]any{"pattern": "token", "literal": true})
	if strings.Contains(r.Content, "OutsideSecret") || !strings.Contains(r.Content, "src/a.go:3:") {
		t.Fatalf("search followed a symlink: %q", r.Content)
	}
}
