package environment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/hitl"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
	"go.opentelemetry.io/otel/attribute"
)

const proposalPrefix = "environment/proposals/"

// ErrStartDeferred means the graph transition could not begin.
var ErrStartDeferred = errors.New("environment proposal start deferred")

type StartBudgetBlock struct {
	Scope string `json:"scope"`
	ID    string `json:"id"`
}

type Proposal struct {
	ID           string            `json:"id"`
	GraphID      string            `json:"graphId"`
	GraphVersion int               `json:"graphVersion"`
	State        string            `json:"state"`
	Error        string            `json:"error,omitempty"`
	Actor        string            `json:"actor"`
	Resume       bool              `json:"resume"`
	StartBlocked *StartBudgetBlock `json:"startBlocked,omitempty"`
	CreatedAt    time.Time         `json:"createdAt"`
	Analysis     *Analysis         `json:"analysis,omitempty"`
	InputSHA256  string            `json:"inputSHA256,omitempty"`
	Revision     int64             `json:"-"`
}

type Proposals struct {
	Store     storage.Store
	Approvals *hitl.Service
	Models    AnalysisModel
	Manager   *Manager
	Directory func(*domain.Graph) (string, error)
	Save      func(context.Context, *domain.Graph, int, string) error
	Start     func(context.Context, string, string) (*StartBudgetBlock, error)
}

func (p *Proposals) Get(ctx context.Context, id string) (*Proposal, error) {
	setting, err := p.Store.GetSetting(ctx, proposalPrefix+id)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result Proposal
	if err := json.Unmarshal(setting.Value, &result); err != nil {
		return nil, err
	}
	result.Revision = setting.Revision
	if result.State == "analyzing" && time.Since(result.CreatedAt) > 2*time.Minute {
		result.State = "failed"
		result.Error = "Analysis interrupted or timed out; request a new proposal"
	}
	return &result, nil
}
func (p *Proposals) put(ctx context.Context, v *Proposal) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	rev, err := p.Store.PutSetting(ctx, proposalPrefix+v.GraphID, data, v.Revision, v.Actor)
	if err == nil {
		v.Revision = rev
		if p.Manager.opt.Bus != nil {
			data := map[string]any{"proposal": v.ID, "state": v.State, "resume": v.Resume}
			if v.StartBlocked != nil {
				data["startBlocked"] = v.StartBlocked
			}
			p.Manager.opt.Bus.Publish(ctx, domain.NewEvent("environment.proposal", v.GraphID, data).WithActor(v.Actor))
		}
		p.Manager.opt.Log.Info("environment proposal state", "graph", v.GraphID, "proposal", v.ID, "state", v.State)
	}
	return err
}

// Request performs one bounded, tool-free model turn and creates a durable gate.
func (p *Proposals) Request(ctx context.Context, g *domain.Graph, ref domain.ModelRef, resume bool, actor string) (result *Proposal, err error) {
	return p.request(ctx, g, ref, resume, actor, nil, nil)
}

// RequestDefinition accepts a complete agent definition, never an approval.
func (p *Proposals) RequestDefinition(ctx context.Context, g *domain.Graph, a *Analysis, caller *tools.Principal) (*Proposal, error) {
	if caller == nil || caller.GraphID != g.Metadata.ID || g.Node(caller.NodeID) == nil {
		return nil, tools.ErrDenied
	}
	node := g.Node(caller.NodeID)
	if node.Isolation.Mode != domain.IsolationDocker || node.Isolation.Image != "" {
		return nil, fmt.Errorf("environment.propose requires a Docker node using the graph environment")
	}
	if a == nil {
		return nil, fmt.Errorf("complete proposal is required")
	}
	if err := ValidateAnalysis(a); err != nil {
		return nil, err
	}
	return p.request(ctx, g, domain.ModelRef{}, false, caller.Actor, a, caller)
}
func (p *Proposals) request(ctx context.Context, g *domain.Graph, ref domain.ModelRef, resume bool, actor string, supplied *Analysis, caller *tools.Principal) (result *Proposal, err error) {
	ctx, span := observability.Start(ctx, "environment.propose", attribute.String("weft.graph.id", g.Metadata.ID))
	defer func() { observability.EndSpan(span, err) }()
	policy, err := p.Manager.CurrentPolicy(ctx)
	if err != nil {
		return nil, err
	}
	if policy.DisableAgentProposals {
		return nil, fmt.Errorf("%w: installation disables agent proposals", ErrInvalidDefinition)
	}
	if g.Spec.Environment != nil {
		return nil, fmt.Errorf("remove the existing environment before requesting analysis")
	}
	rec, e := p.Store.GetGraphRecord(ctx, g.Metadata.ID)
	if e != nil {
		return nil, e
	}
	if supplied == nil && rec.State != domain.GraphStopped && rec.State != domain.GraphFailed {
		return nil, fmt.Errorf("stop the graph before analyzing its environment")
	}
	old, e := p.Get(ctx, g.Metadata.ID)
	if e != nil {
		return nil, e
	}
	if old != nil && old.State != "failed" && old.State != "rejected" && old.State != "cancelled" && old.State != "expired" {
		return nil, fmt.Errorf("%w: an environment proposal already exists", storage.ErrConflict)
	}
	result = &Proposal{ID: domain.NewID("apr"), GraphID: g.Metadata.ID, GraphVersion: g.Metadata.Version, State: "analyzing", Actor: actor, Resume: resume, CreatedAt: time.Now().UTC()}
	if old != nil {
		result.Revision = old.Revision
	}
	if err = p.put(ctx, result); err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			result.State = "failed"
			result.Error = err.Error()
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if saveErr := p.put(cleanup, result); saveErr != nil {
				err = errors.Join(err, saveErr)
			}
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	proposalID := result.ID
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				active, e := p.Get(ctx, g.Metadata.ID)
				if e != nil || active == nil || active.ID != proposalID || active.State == "cancelled" {
					cancel()
					return
				}
			}
		}
	}()
	dir, e := p.Directory(g)
	if e != nil {
		return result, e
	}
	evidence, e := ReadEvidence(ctx, dir)
	if e != nil {
		return result, e
	}
	analysis := supplied
	if analysis == nil {
		analysis, e = analyzeWithPolicy(ctx, p.Models, ref, evidence, policy)
		if e != nil {
			return result, e
		}
	}
	if e = ctx.Err(); e != nil {
		return result, e
	}
	current, e := ReadEvidence(ctx, dir)
	if e != nil {
		return result, e
	}
	if current.SHA256 != evidence.SHA256 {
		return result, fmt.Errorf("repository changed during analysis; request a new proposal")
	}
	env := analysis.Environment
	env.Origin = &domain.EnvironmentOrigin{Source: "agent", Path: result.ID, SHA256: evidence.SHA256, Files: map[string]string{}}
	for name, data := range evidence.Files {
		sum := sha256.Sum256([]byte(data))
		env.Origin.Files[name] = hex.EncodeToString(sum[:])
	}
	if env.Dockerfile != "" {
		env.Context = "."
		snapshot, e := SnapshotContext(ctx, dir, env)
		if e != nil {
			return result, e
		}
		env.Origin.ContextSHA256 = snapshot.SHA256
		snapshot.Close()
	}
	if err := p.Manager.ValidatePolicy(ctx, env); err != nil {
		return result, err
	}
	result.Analysis = analysis
	result.InputSHA256 = evidence.SHA256
	latest, e := p.Store.GetGraph(ctx, g.Metadata.ID, 0)
	if e != nil {
		return result, e
	}
	if latest.Metadata.Version != g.Metadata.Version {
		return result, fmt.Errorf("graph changed during analysis")
	}
	// Persist full proposal first: an orphaned pending approval cannot authorize a
	// different graph or replacement proposal, because every use checks this ID.
	result.State = "awaiting_approval"
	if e = p.put(ctx, result); e != nil {
		return result, e
	}
	detail, e := json.Marshal(result)
	if e != nil {
		return result, e
	}
	expires := time.Now().Add(24 * time.Hour)
	approval := &domain.Approval{ID: result.ID, GraphID: result.GraphID, Kind: domain.ApprovalEnvironment, Title: "Approve proposed development environment", Detail: detail, ExpiresAt: &expires}
	if caller != nil {
		approval.NodeID = caller.NodeID
		approval.Replica = caller.Replica
		approval.ExecutionID = caller.ExecutionID
	}
	_, e = p.Approvals.Create(ctx, approval)
	return result, e
}

func (p *Proposals) ValidateDecision(ctx context.Context, a *domain.Approval) error {
	v, err := p.Get(ctx, a.GraphID)
	if err != nil {
		return err
	}
	if v == nil || v.ID != a.ID || v.State != "awaiting_approval" {
		return fmt.Errorf("proposal is no longer awaiting approval")
	}
	if a.ExpiresAt != nil && !time.Now().Before(*a.ExpiresAt) {
		return fmt.Errorf("proposal approval has expired")
	}
	g, err := p.Store.GetGraph(ctx, a.GraphID, 0)
	if err != nil {
		return err
	}
	if g.Metadata.Version != v.GraphVersion || g.Spec.Environment != nil {
		return fmt.Errorf("graph changed after proposal; request a new analysis")
	}
	dir, err := p.Directory(g)
	if err != nil {
		return err
	}
	if v.Analysis == nil {
		return fmt.Errorf("proposal analysis is missing")
	}
	if err := p.Manager.ValidatePolicy(ctx, v.Analysis.Environment); err != nil {
		return err
	}
	return validateProposalFiles(ctx, dir, v.Analysis.Environment)
}

func validateProposalFiles(ctx context.Context, dir string, env *domain.EnvironmentSpec) error {
	if env == nil || env.Origin == nil || env.Origin.Source != "agent" {
		return fmt.Errorf("invalid proposed environment origin")
	}
	evidence, err := ReadEvidence(ctx, dir)
	if err != nil {
		return err
	}
	if evidence.SHA256 != env.Origin.SHA256 {
		return fmt.Errorf("%w: analysis inputs changed", ErrInvalidDefinition)
	}
	if env.Context != "" {
		snapshot, err := SnapshotContext(ctx, dir, env)
		if err != nil {
			return err
		}
		defer snapshot.Close()
		if snapshot.SHA256 != env.Origin.ContextSHA256 {
			return fmt.Errorf("%w: proposed build context changed", ErrInvalidDefinition)
		}
	}
	return nil
}

func (p *Proposals) ValidateAccepted(ctx context.Context, g *domain.Graph) error {
	if err := p.Manager.ValidatePolicy(ctx, g.Spec.Environment); err != nil {
		return err
	}
	env := g.Spec.Environment
	if env == nil || env.Origin == nil || env.Origin.Source != "agent" {
		return nil
	}
	a, err := p.Store.GetApproval(ctx, env.Origin.Path)
	if err != nil {
		return err
	}
	if a.Kind != domain.ApprovalEnvironment || a.GraphID != g.Metadata.ID || a.Status != domain.ApprovalApproved || a.DecidedBy == "system:timeout" {
		return fmt.Errorf("environment proposal has not been approved by a developer")
	}
	var approved Proposal
	if err = json.Unmarshal(a.Detail, &approved); err != nil {
		return err
	}
	if approved.Analysis == nil || !sameDefinition(env, approved.Analysis.Environment) {
		return fmt.Errorf("environment differs from the approved proposal")
	}
	v, err := p.Get(ctx, g.Metadata.ID)
	if err != nil {
		return err
	}
	if v == nil || v.ID != a.ID || v.State == "cancelled" || v.State == "rejected" {
		return fmt.Errorf("environment proposal is no longer active")
	}
	dir, err := p.Directory(g)
	if err != nil {
		return err
	}
	return validateProposalFiles(ctx, dir, env)
}

// Apply is retryable after a decided approval, including after a process restart.
func (p *Proposals) Apply(ctx context.Context, id string) error {
	return p.locked(ctx, id, func(ctx context.Context) error { return p.apply(ctx, id) })
}
func (p *Proposals) apply(ctx context.Context, id string) error {
	v, err := p.Get(ctx, id)
	if err != nil || v == nil {
		return err
	}
	a, err := p.Store.GetApproval(ctx, v.ID)
	if err != nil {
		return err
	}
	if a.Status != domain.ApprovalApproved {
		v.State = string(a.Status)
		if a.Status == domain.ApprovalPending {
			v.State = "awaiting_approval"
		}
		return p.put(ctx, v)
	}
	g, err := p.Store.GetGraph(ctx, id, 0)
	if err != nil {
		return err
	}
	if v.Analysis == nil || v.Analysis.Environment == nil {
		return fmt.Errorf("proposal has no valid definition")
	}
	env := v.Analysis.Environment
	if g.Spec.Environment == nil {
		if g.Metadata.Version != v.GraphVersion {
			return fmt.Errorf("graph changed after proposal")
		}
		candidate := *g
		candidate.Spec.Environment = env
		if err = p.ValidateAccepted(ctx, &candidate); err != nil {
			return err
		}
		if err = p.Save(ctx, &candidate, v.GraphVersion, a.DecidedBy); err != nil {
			return err
		}
		g = &candidate
	} else if !sameDefinition(g.Spec.Environment, env) {
		return fmt.Errorf("graph environment differs from approved proposal")
	}
	if err = p.ValidateAccepted(ctx, g); err != nil {
		return err
	}
	if _, err = p.Manager.BuildSaved(ctx, g, false, a.DecidedBy); err != nil {
		return err
	}
	v.State = "building"
	v.Error = ""
	return p.put(ctx, v)
}

func (p *Proposals) Check(ctx context.Context, g *domain.Graph) error {
	v, err := p.Get(ctx, g.Metadata.ID)
	if err != nil {
		return err
	}
	if v == nil {
		return nil
	}
	if g.Spec.Environment != nil && (g.Spec.Environment.Origin == nil || g.Spec.Environment.Origin.Source != "agent") && (v.State == "cancelled" || v.State == "rejected" || v.State == "expired") {
		return nil
	}
	if v.State != "ready" {
		return fmt.Errorf("environment proposal is %s; approve and build it before starting tasks", v.State)
	}
	if g.Spec.Environment == nil || v.Analysis == nil || !sameDefinition(g.Spec.Environment, v.Analysis.Environment) {
		return fmt.Errorf("approved environment was removed or changed")
	}
	return p.ValidateAccepted(ctx, g)
}

func (p *Proposals) Cancel(ctx context.Context, id, actor string) error {
	return p.locked(ctx, id, func(ctx context.Context) error { return p.cancel(ctx, id, actor) })
}
func (p *Proposals) cancel(ctx context.Context, id, actor string) error {
	v, err := p.Get(ctx, id)
	if err != nil || v == nil {
		return err
	}
	if v.State == "building" || v.State == "ready" {
		return fmt.Errorf("cannot cancel a built environment proposal")
	}
	if v.State == "awaiting_approval" {
		if _, err = p.Approvals.Decide(ctx, v.ID, false, actor, "proposal cancelled", ""); err != nil && !errors.Is(err, hitl.ErrNotPending) {
			return err
		}
	}
	v.State = "cancelled"
	v.Resume = false
	v.Actor = actor
	return p.put(ctx, v)
}

// Reconcile restores approved work after restarts and starts an explicitly
// requested graph only after its environment is ready. Errors remain visible.
func (p *Proposals) Reconcile(ctx context.Context) {
	settings, err := p.Store.ListSettings(ctx, proposalPrefix)
	if err != nil {
		p.Manager.opt.Log.Error("list environment proposals", "error", err)
		return
	}
	for _, setting := range settings {
		var v Proposal
		if err := json.Unmarshal(setting.Value, &v); err != nil {
			p.Manager.opt.Log.Error("read environment proposal", "error", err)
			continue
		}
		v.Revision = setting.Revision
		if v.State != "awaiting_approval" && v.State != "building" && !(v.State == "ready" && (v.Resume || v.StartBlocked != nil)) {
			continue
		}
		err = p.locked(ctx, v.GraphID, func(ctx context.Context) error {
			fresh, err := p.Get(ctx, v.GraphID)
			if err != nil || fresh == nil {
				return err
			}
			if err = p.reconcileOne(ctx, fresh); err != nil {
				if fresh.StartBlocked != nil || (fresh.State == "ready" && fresh.Resume) || errors.Is(err, storage.ErrConflict) {
					return err
				}
				fresh.State = "failed"
				fresh.Error = err.Error()
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer cancel()
				return errors.Join(err, p.put(cleanup, fresh))
			}
			return nil
		})
		if err != nil && !errors.Is(err, storage.ErrConflict) {
			p.Manager.opt.Log.Error("reconcile environment proposal", "graph", v.GraphID, "error", err)
		}
	}
}
func (p *Proposals) reconcileOne(ctx context.Context, v *Proposal) error {
	if v.State == "ready" && v.StartBlocked != nil && !v.Resume {
		rec, err := p.Store.GetGraphRecord(ctx, v.GraphID)
		if errors.Is(err, storage.ErrNotFound) {
			return nil
		}
		if err != nil || rec.DesiredState != domain.GraphRunning {
			return err
		}
		return p.clearStartBlock(ctx, v)
	}
	if v.State != "awaiting_approval" && v.State != "building" && !(v.State == "ready" && v.Resume) {
		return nil
	}
	if v.State == "awaiting_approval" {
		a, err := p.Store.GetApproval(ctx, v.ID)
		if errors.Is(err, storage.ErrNotFound) && time.Since(v.CreatedAt) < 2*time.Minute {
			return nil
		}
		if err != nil {
			return err
		}
		if a.Status == domain.ApprovalPending {
			return nil
		}
		return p.apply(ctx, v.GraphID)
	}
	g, err := p.Store.GetGraph(ctx, v.GraphID, 0)
	if err != nil {
		return err
	}
	view, err := p.Manager.Get(ctx, g)
	if err != nil {
		return err
	}
	if view.State == StateBuilding {
		return nil
	}
	if !view.UpToDate {
		return fmt.Errorf("environment build failed or changed: %s", view.Error)
	}
	if err = p.ValidateAccepted(ctx, g); err != nil {
		return err
	}
	if v.State != "ready" {
		v.State = "ready"
		if err = p.put(ctx, v); err != nil {
			return err
		}
	}
	if v.Resume && p.Start != nil {
		rec, e := p.Store.GetGraphRecord(ctx, v.GraphID)
		if e != nil {
			return e
		}
		// Consume the intent before an effect that may already have committed
		// when the final proposal write fails.
		v.Resume = false
		if err := p.put(ctx, v); err != nil {
			v.Resume = true
			return err
		}
		if rec.DesiredState == domain.GraphRunning {
			return nil
		}
		v.StartBlocked, err = p.Start(ctx, v.GraphID, v.Actor)
		if errors.Is(err, ErrStartDeferred) {
			v.Resume = true
			return p.put(ctx, v)
		}
		if err != nil || v.StartBlocked == nil {
			return err
		}
		latest, err := p.Store.GetGraphRecord(ctx, v.GraphID)
		if err != nil {
			return err
		}
		if latest.DesiredState == domain.GraphRunning {
			return nil
		}
		return p.put(ctx, v)
	}
	return nil
}

// ClearStartBlock is called after the graph's running intent commits. The
// proposal revision protects a replacement proposal or a concurrent update.
func (p *Proposals) ClearStartBlock(ctx context.Context, id string) error {
	v, err := p.Get(ctx, id)
	if err != nil || v == nil || v.State != "ready" || v.StartBlocked == nil {
		return err
	}
	return p.clearStartBlock(ctx, v)
}

func (p *Proposals) clearStartBlock(ctx context.Context, v *Proposal) error {
	cleared := *v
	cleared.StartBlocked = nil
	return p.put(ctx, &cleared)
}

func (p *Proposals) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.Reconcile(ctx)
		}
	}
}

func (p *Proposals) locked(ctx context.Context, id string, fn func(context.Context) error) (err error) {
	holder := domain.NewID("proposal")
	ok, err := p.Store.AcquireLease(ctx, "proposal-apply/"+id, holder, time.Minute)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: proposal transition in progress", storage.ErrConflict)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err = errors.Join(err, p.Store.ReleaseLease(cleanup, "proposal-apply/"+id, holder))
	}()
	bounded, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	return fn(bounded)
}

// Stop/pause intent must revoke a previous request to start after the build.
func (p *Proposals) DisableResume(ctx context.Context, id string) error {
	return p.locked(ctx, id, func(ctx context.Context) error {
		v, err := p.Get(ctx, id)
		if err != nil || v == nil {
			return err
		}
		if !v.Resume {
			return nil
		}
		v.Resume = false
		return p.put(ctx, v)
	})
}
