// Security model of the tool layer
//
// 1. Default deny. A principal holds a list of grants (domain.ToolGrant).
//    A tool may run only if at least one grant names it, by exact tool
//    name, by capability ("filesystem.read" authorises filesystem.read,
//    .list, .search and .stat) or by wildcard ("git.*", "*"). Checkpoint
//    tools (checkpoint.list, checkpoint.restore) are never matched by a
//    wildcard: they need a grant naming them.
// 2. Grants are narrowed, never widened, by constraints: Paths (workspace
//    globs for filesystem/git capabilities), Commands (argv[0] or command
//    line globs for shell.exec) and Hosts (host globs for http.fetch). When
//    several grants match one tool they are merged by union: the call is
//    allowed if any matching grant would allow it. An unconstrained axis in
//    any matching grant therefore leaves that axis unconstrained.
// 3. The filesystem jail is enforced by ResolvePath: every path an agent
//    supplies is cleaned, joined to the replica's workdir and resolved
//    through symlinks (for the deepest existing ancestor when the target
//    does not exist yet), and refused when the result leaves the workdir.
//    The check is lexical-then-physical so neither "../" nor a planted
//    symlink can reach outside.
// 4. Commands never inherit the server's environment (that is where
//    provider API keys live). Sandboxes build a minimal environment.
// 5. What is NOT a boundary: a granted unrestricted shell can do anything
//    the sandbox's OS user can do inside (and, for the process sandbox,
//    outside) the workdir. The process sandbox confines the starting
//    directory, environment and lifetime, not the syscalls; use the docker
//    sandbox (network none, CPU/memory limits, only the workspace mounted)
//    or Kubernetes isolation where agents are not fully trusted. Command
//    allowlists are the in-between: a narrowed shell.exec only accepts
//    single simple commands without shell metacharacters.

package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/woliveiras/weft/server/internal/domain"
)

// capabilityOnly marks tools that only a grant matching their capability
// authorises (check.run needs a shell.exec grant), and that share their
// capability's rate-limit allowance instead of adding their own.
type capabilityOnly interface{ capabilityOnly() }

// argsDescriber lets a tool show what a call will really do (check.run
// resolves the declared command) in approval prompts and the audit log.
type argsDescriber interface {
	DescribeArgs(p *Principal, args json.RawMessage) json.RawMessage
}

// MatchingGrants returns every grant of p that names tool t (by tool name,
// capability or wildcard), in declaration order. A capabilityOnly tool is
// matched by its capability alone.
func MatchingGrants(p *Principal, t Tool) []domain.ToolGrant {
	return matchingGrants(p, t, false)
}

func matchingGrants(p *Principal, t Tool, includeCapabilityOnlyNames bool) []domain.ToolGrant {
	if p == nil || t == nil {
		return nil
	}
	var out []domain.ToolGrant
	for _, g := range p.Grants {
		if g.Name == "" {
			continue
		}
		if strings.HasPrefix(t.Name(), "mcp.") {
			if g.Name == t.Name() {
				out = append(out, g)
			}
			continue
		}
		// Checkpoint tools let an agent rewrite its workspace from history:
		// only a grant naming the tool or its capability enables them, never
		// a wildcard.
		if ExplicitGrantOnly(t) {
			if g.Name == t.Name() || g.Name == t.Capability() {
				out = append(out, g)
			}
			continue
		}
		if _, capOnly := t.(capabilityOnly); capOnly && !includeCapabilityOnlyNames {
			// Granted only through its capability: a grant naming the tool
			// itself ("check.run", "check.*") must not open a shell.
			if domain.GlobMatch(g.Name, t.Capability()) {
				out = append(out, g)
			}
			continue
		}
		if domain.GlobMatch(g.Name, t.Name()) || domain.GlobMatch(g.Name, t.Capability()) {
			out = append(out, g)
		}
	}
	return out
}

// ExplicitGrantOnly reports tools that wildcard grants ("*", "checkpoint.*")
// never cover.
func ExplicitGrantOnly(t Tool) bool {
	return strings.HasPrefix(t.Capability(), "checkpoint.")
}

// Authorize decides whether p may invoke t. It returns the merged grant the
// tool must apply its constraints from, or an error wrapping ErrDenied.
//
// Merge rules (union - "any matching grant permits"):
//   - Paths/Commands/Hosts: if any matching grant leaves the axis empty the
//     merged axis is empty (unrestricted); otherwise the patterns are unioned.
//   - RequiresApproval: true only if every matching grant requires it (a
//     grant without the requirement already permits the call).
//   - RateLimit: 0 (unlimited) if any matching grant is unlimited, otherwise
//     the most generous limit.
func Authorize(p *Principal, t Tool) (domain.ToolGrant, error) {
	if t == nil {
		return domain.ToolGrant{}, ErrUnknownTool
	}
	ms := MatchingGrants(p, t)
	if len(ms) == 0 {
		return domain.ToolGrant{}, fmt.Errorf("%w: %s (capability %s) is not granted", ErrDenied, t.Name(), t.Capability())
	}
	return mergeGrants(t.Name(), ms), nil
}

func mergeGrants(name string, ms []domain.ToolGrant) domain.ToolGrant {
	out := domain.ToolGrant{Name: name, RequiresApproval: true}
	pathsOpen, cmdsOpen, hostsOpen, rateOpen := false, false, false, false
	for _, g := range ms {
		if len(g.Paths) == 0 {
			pathsOpen = true
		}
		if len(g.Commands) == 0 {
			cmdsOpen = true
		}
		if len(g.Hosts) == 0 {
			hostsOpen = true
		}
		if g.RateLimit == 0 {
			rateOpen = true
		} else if g.RateLimit > out.RateLimit {
			out.RateLimit = g.RateLimit
		}
		if !g.RequiresApproval {
			out.RequiresApproval = false
		}
		out.Paths = append(out.Paths, g.Paths...)
		out.Commands = append(out.Commands, g.Commands...)
		out.Hosts = append(out.Hosts, g.Hosts...)
		// Repositories are only ever granted explicitly: the union of the
		// matching grants' lists (an empty list adds none).
		out.Repositories = append(out.Repositories, g.Repositories...)
	}
	if pathsOpen {
		out.Paths = nil
	}
	if cmdsOpen {
		out.Commands = nil
	}
	if hostsOpen {
		out.Hosts = nil
	}
	if rateOpen {
		out.RateLimit = 0
	}
	return out
}

// ---------------------------------------------------------------------------
// Constraint checks used by the tools.
// ---------------------------------------------------------------------------

// CheckPath verifies a workspace-relative, slash-separated path against the
// grant's Paths globs. "**" matches any number of path segments (including
// none), "*" and "?" match within one segment. Empty Paths allows everything.
func CheckPath(g domain.ToolGrant, rel string) error {
	if len(g.Paths) == 0 {
		return nil
	}
	rel = normRel(rel)
	for _, pat := range g.Paths {
		if MatchPathGlob(pat, rel) {
			return nil
		}
	}
	return fmt.Errorf("%w: path %q is outside the granted paths %v", ErrDenied, rel, g.Paths)
}

// pathMayContain reports whether directory rel could contain a path the
// grant allows (used so listings can descend towards permitted subtrees).
func pathMayContain(g domain.ToolGrant, rel string) bool {
	if len(g.Paths) == 0 {
		return true
	}
	rel = normRel(rel)
	var dir []string
	if rel != "." {
		dir = strings.Split(rel, "/")
	}
	for _, pat := range g.Paths {
		if MatchPathGlob(pat, rel) || prefixMatch(splitPattern(pat), dir) {
			return true
		}
	}
	return false
}

func normRel(rel string) string {
	rel = filepath.ToSlash(rel)
	rel = path.Clean("/" + rel)
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		return "."
	}
	return rel
}

func splitPattern(p string) []string {
	p = strings.Trim(filepath.ToSlash(p), "/")
	if p == "" || p == "." {
		return nil
	}
	return strings.Split(p, "/")
}

// MatchPathGlob matches a slash-separated relative path against a pattern
// supporting "**" (any number of segments), "*" and "?" (within a segment)
// and character classes.
func MatchPathGlob(pattern, rel string) bool {
	rel = normRel(rel)
	var segs []string
	if rel != "." {
		segs = strings.Split(rel, "/")
	}
	return matchSegs(splitPattern(pattern), segs)
}

func matchSegs(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			// Collapse consecutive ** and try every split point.
			rest := pat[1:]
			for len(rest) > 0 && rest[0] == "**" {
				rest = rest[1:]
			}
			if len(rest) == 0 {
				return true
			}
			for i := 0; i <= len(segs); i++ {
				if matchSegs(rest, segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		ok, err := path.Match(pat[0], segs[0])
		if err != nil || !ok {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}

// prefixMatch: can dir be the leading part of a path matching pat?
func prefixMatch(pat, dir []string) bool {
	for len(dir) > 0 {
		if len(pat) == 0 {
			return false
		}
		if pat[0] == "**" {
			return true
		}
		ok, err := path.Match(pat[0], dir[0])
		if err != nil || !ok {
			return false
		}
		pat, dir = pat[1:], dir[1:]
	}
	return true
}

// CheckCommand verifies argv against the grant's Commands patterns. A
// pattern without spaces is matched against the basename of argv[0]
// ("go", "pytest*"); a pattern with spaces is matched against the whole
// command line with argv[0] reduced to its basename ("npm *", "git status").
// Empty Commands allows everything.
func CheckCommand(g domain.ToolGrant, argv []string) error {
	if len(argv) == 0 || argv[0] == "" {
		return fmt.Errorf("%w: empty command", ErrDenied)
	}
	if len(g.Commands) == 0 {
		return nil
	}
	base := filepath.Base(filepath.ToSlash(argv[0]))
	base = path.Base(base)
	line := strings.Join(append([]string{base}, argv[1:]...), " ")
	for _, pat := range g.Commands {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}
		if !strings.Contains(pat, " ") {
			if domain.GlobMatch(pat, base) {
				return nil
			}
			continue
		}
		if domain.GlobMatch(pat, line) {
			return nil
		}
	}
	return fmt.Errorf("%w: command %q is not in the allowed commands %v", ErrDenied, line, g.Commands)
}

// CheckRepository verifies that the grant names an SCM connection.
// Repositories are never implied: an empty list allows none.
func CheckRepository(g domain.ToolGrant, connection string) error {
	for _, r := range g.Repositories {
		if r == connection {
			return nil
		}
	}
	return fmt.Errorf("%w: SCM connection %q is not granted to this agent", ErrDenied, connection)
}

// CheckHost verifies a host name against the grant's Hosts globs
// (case-insensitive). Empty Hosts allows every host (the SSRF guard still
// applies separately).
func CheckHost(g domain.ToolGrant, host string) error {
	if len(g.Hosts) == 0 {
		return nil
	}
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	for _, pat := range g.Hosts {
		if domain.GlobMatch(strings.ToLower(pat), h) {
			return nil
		}
	}
	return fmt.Errorf("%w: host %q is not in the allowed hosts %v", ErrDenied, host, g.Hosts)
}

// ResolvePath maps an agent-supplied path onto the workdir jail. It returns
// the absolute physical path and the slash-separated path relative to the
// (physical) workdir. Absolute inputs are accepted only when they already
// point inside the workdir. Symlinks are resolved for the deepest existing
// ancestor so a link cannot escape; the final component may not exist yet.
func ResolvePath(workdir, p string) (abs, rel string, err error) {
	if workdir == "" {
		return "", "", fmt.Errorf("%w: no workdir", ErrDenied)
	}
	rootAbs, err := filepath.Abs(workdir)
	if err != nil {
		return "", "", err
	}
	root, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", "", fmt.Errorf("workdir %q: %w", workdir, err)
	}
	if strings.ContainsRune(p, 0) {
		return "", "", fmt.Errorf("%w: invalid path", ErrDenied)
	}
	if p == "" {
		p = "."
	}
	p = filepath.FromSlash(p)
	var joined string
	if filepath.IsAbs(p) {
		joined = filepath.Clean(p)
		// Accept absolute paths spelled with either the given or physical root.
		if r, ok := within(rootAbs, joined); ok {
			joined = filepath.Join(root, r)
		}
	} else {
		joined = filepath.Join(root, p)
	}
	if _, ok := within(root, joined); !ok {
		return "", "", fmt.Errorf("%w: path %q escapes the workspace", ErrDenied, p)
	}
	// Resolve symlinks of the deepest existing ancestor.
	existing, rest := joined, ""
	for {
		if _, statErr := os.Lstat(existing); statErr == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", "", err
	}
	if rest != "" {
		resolved = filepath.Join(resolved, rest)
	}
	r, ok := within(root, resolved)
	if !ok {
		return "", "", fmt.Errorf("%w: path %q resolves outside the workspace", ErrDenied, p)
	}
	return resolved, filepath.ToSlash(r), nil
}

// within returns target relative to root when target is root or below it.
func within(root, target string) (string, bool) {
	r, err := filepath.Rel(root, target)
	if err != nil {
		return "", false
	}
	if r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) || filepath.IsAbs(r) {
		return "", false
	}
	return r, true
}
