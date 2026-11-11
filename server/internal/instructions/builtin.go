package instructions

import (
	"embed"
	"errors"
	"sort"
	"strings"
	"time"
)

// Built-in role skills (NAT-08) ship with Weft: planner, implementer,
// reviewer and verifier. They are read-only, never installation defaults,
// and graphs opt in by ID like any skill. Like all guidance they are
// untrusted text and grant nothing; the tools they mention work only when
// the node is granted them.

//go:embed builtin/*.md
var builtinFS embed.FS

// BuiltinPrefix marks IDs reserved for built-in skills.
const BuiltinPrefix = "weft-"

// ErrBuiltinReadOnly: built-in skills cannot be created, edited or deleted.
var ErrBuiltinReadOnly = errors.New("built-in skills are read-only; IDs starting with weft- are reserved")

var builtinMeta = map[string][2]string{
	"weft-planner":     {"Planner role", "Read-only planning: map the code, write a plan with plan.update, name the checks."},
	"weft-implementer": {"Implementer role", "Edit, verify with checks, and keep the plan current until the checks pass."},
	"weft-reviewer":    {"Reviewer role", "Read-only review of the change against the request, with concrete findings."},
	"weft-verifier":    {"Verifier role", "Run the declared checks and report their real results."},
}

// builtinAt is the fixed version time of the shipped skills.
var builtinAt = time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)

// IsBuiltin reports whether id is reserved for a built-in skill.
func IsBuiltin(id string) bool { return strings.HasPrefix(id, BuiltinPrefix) }

func builtin(id string) (*Skill, bool) {
	meta, ok := builtinMeta[id]
	if !ok {
		return nil, false
	}
	body, err := builtinFS.ReadFile("builtin/" + id + ".md")
	if err != nil {
		return nil, false
	}
	return &Skill{Version: Version{
		Input:    Input{ID: id, Title: meta[0], Description: meta[1], Body: string(body), Enabled: true},
		Revision: 1, Actor: "weft", At: builtinAt,
	}, History: []Version{}, Builtin: true}, true
}

func builtins() []*Skill {
	ids := make([]string, 0, len(builtinMeta))
	for id := range builtinMeta {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*Skill, 0, len(ids))
	for _, id := range ids {
		if s, ok := builtin(id); ok {
			out = append(out, s)
		}
	}
	return out
}
