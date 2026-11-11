package tools

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
)

func grants(names ...string) []domain.ToolGrant {
	out := make([]domain.ToolGrant, len(names))
	for i, n := range names {
		out[i] = domain.ToolGrant{Name: n}
	}
	return out
}

func TestAuthorizeMatching(t *testing.T) {
	reg := Builtin(BuiltinConfig{})
	tool := func(n string) Tool { t, _ := reg.Get(n); return t }
	cases := []struct {
		grants []domain.ToolGrant
		tool   string
		ok     bool
	}{
		{nil, "filesystem.read", false},                            // default deny
		{grants("filesystem.read"), "filesystem.read", true},       // exact
		{grants("filesystem.read"), "filesystem.list", true},       // capability
		{grants("filesystem.read"), "filesystem.write", false},     // other capability
		{grants("filesystem.*"), "filesystem.delete", true},        // wildcard
		{grants("git.read"), "git.commit", false},                  // read != write
		{grants("git.*"), "git.commit", true},                      // wildcard over capabilities
		{grants("*"), "shell.exec", true},                          // everything
		{grants("filesystem.list"), "filesystem.read", false},      // tool name grant is narrow
		{grants("shell.exec", "http.fetch"), "http.fetch", true},   // any grant
		{[]domain.ToolGrant{{Name: ""}}, "filesystem.read", false}, // empty name never matches
	}
	for _, c := range cases {
		p := &Principal{Grants: c.grants}
		_, err := Authorize(p, tool(c.tool))
		if (err == nil) != c.ok {
			t.Errorf("grants %v tool %s: got err=%v, want ok=%v", c.grants, c.tool, err, c.ok)
		}
		if err != nil && !errors.Is(err, ErrDenied) {
			t.Errorf("denial must wrap ErrDenied: %v", err)
		}
	}
}

func TestMergeGrants(t *testing.T) {
	g := mergeGrants("filesystem.write", []domain.ToolGrant{
		{Name: "filesystem.write", Paths: []string{"src/**"}, RequiresApproval: true, RateLimit: 5},
		{Name: "filesystem.*", Paths: []string{"docs/*.md"}, RequiresApproval: true, RateLimit: 10},
	})
	if len(g.Paths) != 2 || !g.RequiresApproval || g.RateLimit != 10 {
		t.Fatalf("unexpected merge: %+v", g)
	}
	g = mergeGrants("x", []domain.ToolGrant{{Name: "x", Paths: []string{"a"}, RequiresApproval: true}, {Name: "x"}})
	if g.Paths != nil || g.RequiresApproval || g.RateLimit != 0 {
		t.Fatalf("unconstrained grant must open the axis: %+v", g)
	}
}

func TestPathGlobs(t *testing.T) {
	cases := []struct {
		pat, path string
		ok        bool
	}{
		{"src/**", "src", true},
		{"src/**", "src/a/b/c.go", true},
		{"src/*", "src/a.go", true},
		{"src/*", "src/a/b.go", false},
		{"**/*.md", "README.md", true},
		{"**/*.md", "docs/x/y.md", true},
		{"**/*.md", "docs/x/y.go", false},
		{"docs/**/index.md", "docs/index.md", true},
		{"docs/**/index.md", "docs/a/b/index.md", true},
		{"*.go", "a/b.go", false},
		{"src/**", "srcx/a.go", false},
	}
	for _, c := range cases {
		if got := MatchPathGlob(c.pat, c.path); got != c.ok {
			t.Errorf("MatchPathGlob(%q,%q)=%v want %v", c.pat, c.path, got, c.ok)
		}
	}
	g := domain.ToolGrant{Paths: []string{"src/**"}}
	if CheckPath(g, "src/x.go") != nil || CheckPath(g, "etc/passwd") == nil {
		t.Fatal("CheckPath")
	}
	if !pathMayContain(g, ".") || pathMayContain(g, "docs") || !pathMayContain(g, "src/a") {
		t.Fatal("pathMayContain")
	}
	if CheckPath(domain.ToolGrant{}, "anything") != nil {
		t.Fatal("empty Paths must allow all")
	}
}

func TestResolvePathJail(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "a"), filepath.Join(root, "inner")); err != nil {
		t.Fatal(err)
	}
	ok := []struct{ in, rel string }{
		{"", "."}, {".", "."}, {"a", "a"}, {"a/../a/new.txt", "a/new.txt"},
		{"new/deep/file.txt", "new/deep/file.txt"}, {"inner/x.txt", "a/x.txt"},
		{filepath.Join(root, "a", "f"), "a/f"},
	}
	for _, c := range ok {
		_, rel, err := ResolvePath(root, c.in)
		if err != nil || rel != c.rel {
			t.Errorf("ResolvePath(%q) = %q, %v; want %q", c.in, rel, err, c.rel)
		}
	}
	bad := []string{"..", "../x", "a/../../x", "/etc/passwd", "escape", "escape/secret.txt", "escape/new/file"}
	for _, in := range bad {
		if _, _, err := ResolvePath(root, in); !errors.Is(err, ErrDenied) {
			t.Errorf("ResolvePath(%q) should be denied, got %v", in, err)
		}
	}
}

func TestCheckCommand(t *testing.T) {
	g := domain.ToolGrant{Commands: []string{"go", "npm *", "pytest*"}}
	allow := [][]string{{"go", "test", "./..."}, {"/usr/local/go/bin/go", "build"}, {"npm", "test"}, {"pytest-3", "-q"}}
	for _, a := range allow {
		if err := CheckCommand(g, a); err != nil {
			t.Errorf("%v should be allowed: %v", a, err)
		}
	}
	deny := [][]string{{"rm", "-rf", "/"}, {"npm"}, {"gofmt"}, {}}
	for _, a := range deny {
		if err := CheckCommand(g, a); !errors.Is(err, ErrDenied) {
			t.Errorf("%v should be denied, got %v", a, err)
		}
	}
	if err := CheckCommand(domain.ToolGrant{}, []string{"anything"}); err != nil {
		t.Fatal(err)
	}
}

func TestCheckHost(t *testing.T) {
	g := domain.ToolGrant{Hosts: []string{"*.wikipedia.org", "example.com"}}
	if CheckHost(g, "en.wikipedia.org") != nil || CheckHost(g, "EXAMPLE.com") != nil {
		t.Fatal("expected allow")
	}
	if CheckHost(g, "evil.com") == nil || CheckHost(g, "wikipedia.org") == nil {
		t.Fatal("expected deny")
	}
}

func TestSplitCommand(t *testing.T) {
	got, err := SplitCommand(`go test -run 'Test A' "./x y" a\ b`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"go", "test", "-run", "Test A", "./x y", "a b"}
	if len(got) != len(want) {
		t.Fatalf("%q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%q", got)
		}
	}
	if _, err := SplitCommand(`echo "unterminated`); err == nil {
		t.Fatal("expected error")
	}
}

func TestMCPMatchingRequiresExactName(t *testing.T) {
	remote := &funcTool{name: "mcp.team.search", capability: "mcp.team.search"}
	for _, name := range []string{"*", "mcp.*", "mcp.team.*", "mcp.other.search"} {
		p := &Principal{Grants: []domain.ToolGrant{{Name: name}}}
		if _, err := Authorize(p, remote); !errors.Is(err, ErrDenied) {
			t.Fatalf("grant %q: %v", name, err)
		}
	}
	p := &Principal{Grants: []domain.ToolGrant{{Name: "*"}, {Name: remote.Name(), RequiresApproval: true, RateLimit: 2}}}
	grant, err := Authorize(p, remote)
	if err != nil || !grant.RequiresApproval || grant.RateLimit != 2 {
		t.Fatalf("wildcard weakened exact MCP restrictions: %+v %v", grant, err)
	}
}
