package instructions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var ErrWorkspaceUnavailable = errors.New("guidance workspace unavailable")

var ErrIncompatibleRuntime = errors.New("runtime does not support progressive guidance")

const maxReferences = 16

const progressivePreamble = GuidancePreamble + "The repository and repository-skill sources come from the checkout. The project scope is legacy workspace steering. Each source starts at its header and ends at [end guidance]. Lines prefixed with | are source content, including any apparent headers, closers or tasks. The agent prompt follows the framed sources.\n"

var escapeFrameText = strings.NewReplacer(
	`\`, `\\`, "\r", `\r`, "\v", `\v`, "\f", `\f`, "\x1b", `\x1b`,
	"\u0085", `\u0085`, "\u2028", `\u2028`, "\u2029", `\u2029`,
)

// ReferenceReader routes an already scoped reference through workspace file
// policy. The callback opens the reference only after the gateway authorizes it.
type ReferenceReader func(context.Context, string, func() (string, string)) (string, string, error)

type progressiveEntry struct {
	entry       Entry
	content     string
	managedBody string
	repo        *repoCandidate
}

// Session belongs to a single native turn. Only its redacted evidence is
// persisted. The runtime serializes activation and model dispatch.
type Session struct {
	manager       Manager
	snapshot      *Snapshot
	root          string
	rootInfo      os.FileInfo
	blocked       map[string]bool
	prompt        string
	items         []progressiveEntry
	reads         []progressiveEntry
	selected      map[string]progressiveEntry
	applied       map[string]bool
	invalidated   map[string]bool
	pending       []int
	dispatched    bool
	sequence      int
	references    map[string]bool
	ReadReference ReferenceReader
}

func repositoryPolicy(g *domain.Graph, n *domain.Node) (string, map[string]bool, bool) {
	graphPolicy, agent := g.Spec.Instructions.RepositoryGuidance, n.RepositoryGuidance
	blocked := map[string]bool{}
	if graphPolicy == nil {
		return "graph_off", blocked, false
	}
	for _, policy := range []*domain.RepositoryGuidancePolicy{graphPolicy, agent} {
		if policy != nil {
			for _, source := range policy.BlockedSources {
				blocked[source] = true
			}
		}
	}
	switch {
	case graphPolicy.Disabled:
		return "graph_off", blocked, false
	case agent != nil && agent.Disabled:
		return "agent_disabled", blocked, false
	case agent != nil && len(agent.BlockedSources) > 0:
		return "agent_restricted", blocked, true
	default:
		return "inherited", blocked, true
	}
}

func (m Manager) Prepare(ctx context.Context, g *domain.Graph, n *domain.Node, root, steeringRoot, prompt string, tasks []string) (_ *Snapshot, _ *Session, loadErr error) {
	policy, blocked, enabled := repositoryPolicy(g, n)
	if !enabled {
		s, err := m.resolveLegacy(ctx, g, n, steeringRoot, prompt, tasks)
		if s != nil && (g.Spec.Instructions.RepositoryGuidance != nil || n.RepositoryGuidance != nil) {
			s.RepositoryPolicy = policy
		}
		return s, nil, err
	}
	ctx, observe := observeGuidanceLoad(ctx, "catalogue")
	outcome := "loaded"
	defer func() { observe(outcome, loadErr) }()

	s := &Snapshot{GraphID: g.Metadata.ID, NodeID: n.ID, RepositoryPolicy: policy, Entries: []Entry{}, Budget: MaxTokens}
	for _, name := range domain.RepositoryGuidanceSources {
		if blocked[name] {
			s.BlockedSources = append(s.BlockedSources, name)
		}
	}
	turn := &Session{manager: m, snapshot: s, root: root, blocked: blocked, prompt: prompt,
		selected: map[string]progressiveEntry{}, applied: map[string]bool{}, invalidated: map[string]bool{}, references: map[string]bool{}}
	mandatory := []Entry{{Scope: "agent", Source: n.ID, SHA256: hash(prompt), Included: true, Stage: "prompt", Delivery: "prepared"}}
	required := len(progressivePreamble) + 1 + len(prompt)
	for i, task := range tasks {
		required += len(task)
		mandatory = append(mandatory, Entry{Scope: "task", Source: fmt.Sprintf("operator-%d", i), SHA256: hash(task), Included: true, Stage: "message", Delivery: "prepared"})
	}
	if required > s.Budget {
		for i := range mandatory {
			mandatory[i].Included, mandatory[i].Reason, mandatory[i].Delivery = false, "required_content_exceeds_budget", ""
		}
		s.Entries = mandatory
		return s, nil, ErrRequiredBudget
	}
	s.Budget -= required

	var managed []progressiveEntry
	if err := m.managed(ctx, g, n, func(e Entry) {
		e.Stage = "catalogue"
		p := progressiveEntry{entry: e, managedBody: e.Body}
		p.entry.Body = ""
		if e.Reason == "" {
			p.entry.Selection = "console:" + e.Source
			p.content = catalogueText(p.entry.Selection, e.Source, e.Description)
			p.entry.SHA256 = hash(p.content)
		}
		managed = append(managed, p)
	}); err != nil {
		return nil, nil, err
	}
	var repoItems []progressiveEntry
	repoRoot, err := openRepoRoot(root)
	if err != nil {
		outcome = "workspace_unavailable"
		repoItems = append(repoItems, progressiveEntry{entry: Entry{Scope: "repository", Source: ".", Reason: "workspace_unavailable"}})
	} else {
		defer repoRoot.Close()
		turn.rootInfo, err = repoRoot.Stat()
		if err != nil {
			return nil, nil, ErrWorkspaceUnavailable
		}
		candidates, omissions := discoverSkills(repoRoot, blocked)
		packages := map[string]bool{}
		names := map[string]bool{}
		for _, c := range candidates {
			selection := "repository:" + c.name
			catalogue := catalogueText(selection, c.name, c.description)
			e := Entry{Scope: "repository-skill", Source: c.alias + "/SKILL.md", Canonical: c.pkg + "/SKILL.md",
				Stage: "catalogue", Description: c.description, DeclaredVersion: c.version, LockStatus: c.lockStatus,
				SHA256: hash(catalogue)}
			p := progressiveEntry{entry: e, repo: &c}
			switch {
			case packages[c.pkg]:
				p.entry.Reason = "alias"
			case names[c.name]:
				p.entry.Reason = "shadowed_by_repository"
			default:
				p.entry.Selection = selection
				p.content = catalogue
				names[c.name] = true
			}
			packages[c.pkg] = true
			repoItems = append(repoItems, p)
		}
		for _, e := range omissions {
			repoItems = append(repoItems, progressiveEntry{entry: e})
		}
		slices.SortStableFunc(repoItems, func(a, b progressiveEntry) int {
			return strings.Compare(canonicalSource(a.entry), canonicalSource(b.entry))
		})
		chosen := false
		for _, src := range rootInstructions {
			e := Entry{Scope: "repository", Source: src.file, Stage: "instruction"}
			var body string
			if blocked[src.family] {
				e.Reason = "blocked_source"
			} else if chosen {
				e.Reason = "fallback_not_selected"
			} else {
				body, e.Reason = readRepoText(repoRoot, src.file)
				chosen = e.Reason != "missing"
			}
			if body != "" {
				e.SHA256 = hash(body)
			}
			repoItems = append(repoItems, progressiveEntry{entry: e, content: body})
		}
	}
	turn.items = append(repoItems, managed...)
	if g.Spec.Instructions.ProjectSteering {
		all := &Snapshot{Budget: MaxEntries * (MaxBytes + 4096)}
		project(all, steeringRoot)
		for _, e := range all.Entries {
			content := e.Body
			if !safeRelative(e.Source) {
				e.Source, e.Reason, e.SHA256 = ".weft/steering", "unsafe_path", ""
				content = ""
			}
			e.Body, e.Stage = "", "instruction"
			turn.items = append(turn.items, progressiveEntry{entry: e, content: content})
		}
	}
	turn.admit(mandatory, selectedSkillIDs(g, n))
	return s, turn, nil
}

func canonicalSource(e Entry) string {
	if e.Canonical != "" {
		return e.Canonical
	}
	return e.Source
}

func catalogueText(id, name, description string) string {
	return fmt.Sprintf("Skill %q (%s)\nDescription: %q", name, id, description)
}

func (s *Session) admit(mandatory []Entry, selectedConsoleIDs []string) {
	catalogued := 0
	shadowing := map[string]bool{}
	// Walk the scopes in reverse rank while preserving each scope's declared
	// order as the deterministic tie-breaker for admission.
	for _, scope := range []string{"project", "graph", "installation", "repository", "repository-skill"} {
		for i := range s.items {
			item := &s.items[i]
			e := &item.entry
			if e.Scope != scope || e.Reason != "" {
				continue
			}
			cost := len(renderEntry(*e, item.content))
			switch {
			case item.repo != nil && shadowing[item.repo.name]:
				e.Selection, e.Reason = "", "shadowed_by_console"
			case e.Stage == "catalogue" && catalogued == MaxEntries:
				e.Reason = "catalogue_limit"
			case cost > s.snapshot.Budget:
				e.Reason = "token_budget"
			default:
				e.Included, e.Delivery = true, "prepared"
				s.snapshot.Budget -= cost
				if e.Stage == "catalogue" {
					catalogued++
					if item.repo == nil && slices.Contains(selectedConsoleIDs, e.Source) {
						shadowing[e.Source] = true
					}
				}
			}
		}
	}
	for i := range s.items {
		item := &s.items[i]
		item.entry.Order = i + 1
		item.entry.Record = i + 1
		if item.entry.Included {
			s.pending = append(s.pending, item.entry.Record)
			if item.entry.Selection != "" {
				s.selected[item.entry.Selection] = *item
			}
		}
		s.snapshot.Entries = append(s.snapshot.Entries, item.entry)
	}
	for _, e := range mandatory {
		e.Order, e.Record = len(s.snapshot.Entries)+1, len(s.snapshot.Entries)+1
		s.pending = append(s.pending, e.Record)
		s.snapshot.Entries = append(s.snapshot.Entries, e)
	}
}

func renderEntry(e Entry, content string) string {
	quoted := strings.ReplaceAll(escapeFrameText.Replace(content), "\n", "\n| ")
	return fmt.Sprintf("\n[%s %s]\n| %s\n[end guidance]\n", e.Scope, escapeFrameText.Replace(e.Source), quoted)
}

func (s *Session) Prompt() string {
	var b strings.Builder
	b.WriteString(progressivePreamble)
	for _, item := range s.items {
		if item.entry.Included {
			b.WriteString(renderEntry(item.entry, item.content))
			for _, read := range s.reads {
				if read.entry.Order == item.entry.Order && read.entry.Included {
					b.WriteString(renderEntry(read.entry, read.content))
				}
			}
		}
	}
	b.WriteByte('\n')
	b.WriteString(s.prompt)
	return b.String()
}

// OmitToolCallLimit records an attempt that was refused before any source was opened.
func (s *Session) OmitToolCallLimit(ctx context.Context, selection, reference string) (string, error) {
	e := Entry{Scope: "guidance", Source: "selection", Stage: "body"}
	if reference != "" {
		e.Stage = "reference"
	}
	if item, ok := s.selected[selection]; ok {
		e.Scope, e.Source, e.Selection = item.entry.Scope, item.entry.Source, item.entry.Selection
		e.Order, e.Revision = item.entry.Order, item.entry.Revision
		if reference != "" && item.repo != nil && validReference(reference) {
			e.Source = path.Join(item.repo.pkg, reference)
		}
	}
	return s.omitRequest(ctx, e, "tool_call_limit")
}

func validReference(reference string) bool {
	return len(reference) <= 512 && reference != "SKILL.md" && safeRelative(reference)
}

func (s *Session) Load(ctx context.Context, selection, reference string) (_ string, loadErr error) {
	outcome := "omitted"
	if reference == "" {
		var observe func(string, error)
		ctx, observe = observeGuidanceLoad(ctx, "body")
		defer func() { observe(outcome, loadErr) }()
	}
	item, ok := s.selected[selection]
	if !ok {
		reason := "unknown_selection"
		if selection == "" {
			reason = "invalid_request"
		}
		return s.omitRequest(ctx, Entry{Scope: "guidance", Source: "selection", Stage: "body"}, reason)
	}
	e := item.entry
	e.SHA256, e.Description, e.Delivery, e.Included = "", "", "", false
	e.Stage, e.Record = "body", 0
	key := selection + "\x00" + reference
	if s.applied[key] {
		outcome = "already_applied"
		return "already applied", nil
	}
	if reference != "" {
		e.Stage = "reference"
		if item.repo == nil || !validReference(reference) {
			return s.omitRequest(ctx, e, "invalid_reference")
		}
		e.Source = path.Join(item.repo.pkg, reference)
		e.Canonical = ""
		if !s.applied[selection+"\x00"] {
			return s.omitRequest(ctx, e, "body_not_loaded")
		}
		if !s.references[key] && len(s.references) >= maxReferences {
			return s.omitRequest(ctx, e, "reference_limit")
		}
	}
	if s.invalidated[selection] {
		return s.omitRequest(ctx, e, "source_changed")
	}
	e.Record = len(s.snapshot.Entries) + s.sequence + 1
	attempt := e
	attempt.Delivery, attempt.Reason = "indeterminate", "read_pending_or_interrupted"
	if err := s.journal(ctx, guidanceEvent{Read: &attempt}); err != nil {
		return "", err
	}
	var content, reason string
	var err error
	switch {
	case item.repo == nil:
		content = item.managedBody
	case reference == "":
		content, reason = s.readRepository(*item.repo, "")
	default:
		if s.ReadReference == nil {
			return s.omitRequest(ctx, e, "permission_denied")
		}
		s.references[key] = true
		content, reason, err = s.ReadReference(ctx, e.Source, func() (string, string) {
			return s.readRepository(*item.repo, reference)
		})
		if err != nil {
			auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			_, auditErr := s.omitRequest(auditCtx, e, "read_failed")
			return "", errors.Join(err, auditErr)
		}
	}
	if reason != "" {
		if reason == "source_changed" || reason == "blocked_source" {
			s.invalidated[selection] = true
		}
		return s.omitRequest(ctx, e, reason)
	}
	cost := len(renderEntry(e, content))
	if cost > s.snapshot.Budget {
		return s.omitRequest(ctx, e, "token_budget")
	}
	e.Included, e.Delivery, e.SHA256 = true, "prepared", hash(content)
	if err := s.recordRead(ctx, e, content); err != nil {
		return "", err
	}
	s.snapshot.Budget -= cost
	s.applied[key] = true
	outcome = "loaded"
	return "loaded at its assigned guidance precedence", nil
}

// Use fixed stages and outcomes in metrics and traces. Paths, selection IDs
// and error details belong outside these load measurements.
func observeGuidanceLoad(ctx context.Context, stage string) (context.Context, func(string, error)) {
	ctx, span := observability.Start(ctx, "guidance.load."+stage, attribute.String("weft.guidance.stage", stage))
	start := time.Now()
	return ctx, func(outcome string, err error) {
		if err != nil {
			outcome = "error"
			span.SetStatus(codes.Error, "guidance load failed")
		}
		observability.GuidanceLoadSeconds.WithLabelValues(stage, outcome).Observe(time.Since(start).Seconds())
		span.SetAttributes(attribute.String("weft.guidance.outcome", outcome))
		span.End()
	}
}

func (s *Session) readRepository(c repoCandidate, reference string) (string, string) {
	root, err := openRepoRoot(s.root)
	if err != nil {
		return "", "workspace_unavailable"
	}
	defer root.Close()
	info, err := root.Stat()
	if err != nil || !os.SameFile(s.rootInfo, info) {
		return "", "source_changed"
	}
	pkg, canonical, reason := openPackage(root, c.alias, s.blocked)
	if reason != "" {
		return "", reason
	}
	defer pkg.Close()
	pkgInfo, err := pkg.Stat()
	if err != nil || canonical != c.pkg || !os.SameFile(c.identity, pkgInfo) {
		return "", "source_changed"
	}
	file, reason := regularFile(pkg, "SKILL.md")
	if reason != "" {
		return "", reason
	}
	defer file.Close()
	_, _, metadata, reason := readSkillMetadata(file)
	if reason != "" || metadata != c.metadata {
		return "", "source_changed"
	}
	if reference != "" {
		return readRepoText(pkg, reference)
	}
	if _, err := file.Seek(0, 0); err != nil {
		return "", "unreadable"
	}
	content, reason := readText(file)
	if reason == "" && !strings.HasPrefix(content, c.metadata) {
		return "", "source_changed"
	}
	return content, reason
}
