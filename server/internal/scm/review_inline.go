package scm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
)

const CapReviewComment = "scm.pull_request.comment"
const MaxReviewComments = 6
const reviewCommentReceiptPrefix = "scm/review-comments/"

type ReviewComment struct {
	Path string `json:"path"`
	Side string `json:"side"`
	Line int    `json:"line"`
	Body string `json:"body"`
}

func (c ReviewComment) Valid() bool {
	return c.Path != "" && len(c.Path) <= 200 && utf8.ValidString(c.Path) && !path.IsAbs(c.Path) && path.Clean(c.Path) == c.Path && c.Path != "." && c.Path != ".." && !strings.HasPrefix(c.Path, "../") &&
		strings.IndexFunc(c.Path, unicode.IsControl) < 0 && (c.Side == "LEFT" || c.Side == "RIGHT") && c.Line > 0 && c.Line <= 10000000 &&
		strings.TrimSpace(c.Body) != "" && len(c.Body) <= 480 && !strings.Contains(c.Body, "<!-- weft-inline:") && utf8.ValidString(c.Body) &&
		strings.IndexFunc(c.Body, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) < 0
}

type ReviewCommentsRequest struct {
	Connection         string          `json:"connection"`
	ConnectionRevision string          `json:"connectionRevision"`
	Number             int             `json:"number"`
	HeadSHA            string          `json:"headSha"`
	BaseSHA            string          `json:"baseSha"`
	Comments           []ReviewComment `json:"comments"`
	Reconcile          bool            `json:"reconcile,omitempty"`
}

type ReviewCommentResult struct {
	State string `json:"state"`
	ID    int64  `json:"id,omitempty"`
	Code  string `json:"code,omitempty"`
}

type reviewCommentReceipt struct {
	ReviewCommentResult
	Marker string `json:"marker"`
}

type reviewCommentTool struct{ m Manager }
type reviewedCommentKey struct{}

// InvokeReviewedComments preserves the gateway grant, approval and rate gates
// for a control-plane operation whose exact selection has already been reviewed.
func InvokeReviewedComments(ctx context.Context, gw *tools.Gateway, principal *tools.Principal, request ReviewCommentsRequest) (*tools.Result, error) {
	args, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	ctx = context.WithValue(ctx, reviewedCommentKey{}, true)
	return gw.Invoke(tools.WithoutApproval(ctx), principal, CapReviewComment, args)
}

func (reviewCommentTool) Name() string       { return CapReviewComment }
func (reviewCommentTool) Capability() string { return CapReviewComment }
func (reviewCommentTool) Description() string {
	return "Allow an operator to publish reviewed line comments through Chat on a granted GitHub pull request. Reconciliation only reads the prior attempt."
}
func (reviewCommentTool) SchemaFor(*tools.Principal) (llm.ToolSchema, bool) {
	return llm.ToolSchema{}, false
}
func (reviewCommentTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["connection","connectionRevision","number","headSha","baseSha","comments"],"properties":{"connection":{"type":"string"},"connectionRevision":{"type":"string"},"number":{"type":"integer","minimum":1},"headSha":{"type":"string"},"baseSha":{"type":"string"},"reconcile":{"type":"boolean"},"comments":{"type":"array","minItems":1,"maxItems":6,"items":{"type":"object","additionalProperties":false,"required":["path","side","line","body"],"properties":{"path":{"type":"string"},"side":{"type":"string","enum":["LEFT","RIGHT"]},"line":{"type":"integer","minimum":1},"body":{"type":"string","maxLength":480}}}}}}`)
}

// DescribeArgs removes repository-derived comment content from the tool audit.
func (reviewCommentTool) DescribeArgs(_ *tools.Principal, raw json.RawMessage) json.RawMessage {
	var a ReviewCommentsRequest
	_ = json.Unmarshal(raw, &a)
	return json.RawMessage(fmt.Sprintf(`{"number":%d,"comments":%d,"reconcile":%t}`, a.Number, len(a.Comments), a.Reconcile))
}

func (a ReviewCommentsRequest) valid() bool {
	if a.Connection == "" || a.ConnectionRevision == "" || a.Number <= 0 || !commitID.MatchString(a.HeadSHA) || !commitID.MatchString(a.BaseSHA) || len(a.Comments) == 0 || len(a.Comments) > MaxReviewComments {
		return false
	}
	seen := make(map[ReviewComment]bool)
	for _, comment := range a.Comments {
		if !comment.Valid() || seen[comment] {
			return false
		}
		seen[comment] = true
	}
	return true
}

func (t reviewCommentTool) Invoke(ctx context.Context, inv *tools.Invocation) (*tools.Result, error) {
	if ctx.Value(reviewedCommentKey{}) != true {
		return nil, fmt.Errorf("%w: line comments require a reviewed Chat operation", tools.ErrDenied)
	}
	var a ReviewCommentsRequest
	if json.Unmarshal(inv.Args, &a) != nil || !a.valid() {
		return nil, errors.New("invalid line comment scope or selection")
	}
	if err := tools.CheckRepository(inv.Grant, a.Connection); err != nil {
		return nil, err
	}
	c, err := t.m.Get(ctx, a.Connection)
	if err != nil || a.ConnectionRevision != c.UpdatedAt.UTC().Format(time.RFC3339Nano) || c.Kind != "github" {
		return nil, errors.New("current SCM connection is unavailable for this reviewed revision")
	}
	p, token, err := t.m.Open(c)
	if err != nil {
		return nil, errors.New("could not open the reviewed SCM connection")
	}
	g, ok := p.(*github)
	if !ok {
		return nil, errors.New("commit-anchored comments are unavailable for this adapter")
	}
	for _, comment := range a.Comments {
		if containsCredentialMaterial(c, token, comment.Path+"\n"+comment.Body) {
			return nil, errors.New("selected comments contain protected credential material")
		}
	}
	results := make([]ReviewCommentResult, 0, len(a.Comments))
	for _, comment := range a.Comments {
		results = append(results, t.publish(ctx, inv.Principal, c, g, a, comment))
	}
	data, err := json.Marshal(results)
	return &tools.Result{Data: data, Content: string(data)}, err
}

func reviewCommentKey(c Connection, a ReviewCommentsRequest, comment ReviewComment) string {
	// Scope the receipt to the connected repository and exact reviewed comment.
	// Repeated selections and analysis runs converge on the same receipt.
	raw, _ := json.Marshal(struct {
		Connection, Repository, Revision, Head, Base string
		Number                                       int
		Comment                                      ReviewComment
	}{c.Name, c.Repository, a.ConnectionRevision, a.HeadSHA, a.BaseSHA, a.Number, comment})
	sum := sha256.Sum256(raw)
	return reviewCommentReceiptPrefix + hex.EncodeToString(sum[:])
}

func (t reviewCommentTool) publish(ctx context.Context, principal *tools.Principal, c Connection, g *github, a ReviewCommentsRequest, comment ReviewComment) ReviewCommentResult {
	key := reviewCommentKey(c, a, comment)
	row, err := t.m.Store.GetSetting(ctx, key)
	unknown := ReviewCommentResult{State: "unknown", Code: "receipt_unconfirmed"}
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return unknown
	}
	fresh := row == nil
	var r reviewCommentReceipt
	var id int64
	var revision int64
	if fresh {
		if a.Reconcile {
			return ReviewCommentResult{State: "failed", Code: "not_attempted"}
		}
		snap, err := g.ReadPullRequest(ctx, a.Number)
		if err != nil {
			return ReviewCommentResult{State: "failed", Code: "source_unavailable"}
		}
		if snap.HeadSHA != a.HeadSHA || snap.BaseSHA != a.BaseSHA || snap.State != "open" || snap.Draft {
			return ReviewCommentResult{State: "failed", Code: "source_changed"}
		}
		r = reviewCommentReceipt{ReviewCommentResult: unknown, Marker: domain.NewID("inline")}
		if revision, err = t.save(ctx, key, 0, r, principal.Actor); err != nil {
			return unknown
		}
		id, err = g.writeInlineComment(ctx, a.Number, a.HeadSHA, comment, reviewCommentBody(comment.Body, r.Marker))
		if err != nil {
			if !definiteCommentRefusal(err) {
				return unknown
			}
			r.ReviewCommentResult = ReviewCommentResult{State: "failed", Code: "provider_rejected"}
		}
	} else {
		if json.Unmarshal(row.Value, &r) != nil || r.Marker == "" {
			return unknown
		}
		if r.State == "published" && r.ID > 0 || r.State == "failed" {
			return r.ReviewCommentResult
		}
		if r.State != "unknown" {
			return unknown
		}
		revision = row.Revision
		id, err = g.findInlineComment(ctx, a.Number, a.HeadSHA, comment, reviewCommentBody(comment.Body, r.Marker))
		if err != nil || id == 0 {
			return unknown
		}
	}
	if id > 0 {
		r.ReviewCommentResult = ReviewCommentResult{State: "published", ID: id}
	}
	if _, err = t.save(ctx, key, revision, r, principal.Actor); err != nil {
		return unknown
	}
	if r.State == "published" {
		t.m.event(ctx, principal, domain.EvReviewCommentPublished, map[string]any{"connection": c.Name, "number": a.Number, "headSha": a.HeadSHA, "baseSha": a.BaseSHA, "id": r.ID})
	}
	return r.ReviewCommentResult
}

func (t reviewCommentTool) save(ctx context.Context, key string, revision int64, r reviewCommentReceipt, actor string) (int64, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return 0, err
	}
	return t.m.Store.PutSetting(ctx, key, raw, revision, actor)
}

func reviewCommentBody(body, marker string) string {
	return body + "\n\n<!-- weft-inline:" + marker + " -->"
}

func definiteCommentRefusal(err error) bool {
	var status *providerHTTPError
	if !errors.As(err, &status) {
		return false
	}
	switch status.Status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity:
		return true
	}
	return false
}
