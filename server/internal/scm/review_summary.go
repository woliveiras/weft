package scm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
)

const CapReviewSummary = "scm.pull_request.review"
const reviewReceiptPrefix = "scm/reviews/"

// ReviewReceipt records an authorized publication attempt. Pending attempts are
// reconciled by reads only; a timeout must never cause a blind second POST.
type ReviewReceipt struct {
	Connection string    `json:"connection"`
	Provider   string    `json:"provider"`
	Repository string    `json:"repository"`
	Number     int       `json:"number"`
	HeadSHA    string    `json:"headSha"`
	BaseSHA    string    `json:"baseSha"`
	Verdict    string    `json:"verdict"`
	Status     string    `json:"status"`
	URL        string    `json:"url"`
	Digest     string    `json:"digest"`
	Marker     string    `json:"marker"`
	CreatedAt  time.Time `json:"createdAt"`
}
type summaryArgs struct {
	ConnectionRevision string `json:"connectionRevision,omitempty"`
	Connection         string `json:"connection"`
	Number             int    `json:"number"`
	HeadSHA            string `json:"headSha"`
	BaseSHA            string `json:"baseSha"`
	Verdict            string `json:"verdict"`
	Summary            string `json:"summary"`
}

func (a summaryArgs) validate() error {
	if a.Number <= 0 || !commitID.MatchString(a.HeadSHA) || !commitID.MatchString(a.BaseSHA) {
		return errors.New("name a positive PR number and the exact head/base commit IDs reviewed")
	}
	if a.Verdict != "approved" && a.Verdict != "changes_requested" {
		return errors.New("verdict must be approved or changes_requested")
	}
	if strings.TrimSpace(a.Summary) == "" || len(a.Summary) > MaxBody-512 {
		return errors.New("write a nonempty review summary of at most 15872 bytes")
	}
	return nil
}
func reviewReceiptKey(c Connection, a summaryArgs) string {
	raw := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d\x00%s\x00%s", c.Name, c.Kind, c.Repository, c.UpdatedAt.UTC().Format(time.RFC3339Nano), a.Number, a.HeadSHA, a.BaseSHA)
	sum := sha256.Sum256([]byte(raw))
	return reviewReceiptPrefix + hex.EncodeToString(sum[:])
}
func summaryDigest(a summaryArgs) string {
	sum := sha256.Sum256([]byte(a.Verdict + "\x00" + a.Summary))
	return hex.EncodeToString(sum[:])
}
func summaryBody(a summaryArgs, marker string) string {
	return fmt.Sprintf("## Weft code review\n\nReviewed head: `%s`\nBase: `%s`\nVerdict: **%s**\n\n%s\n\n<!-- weft-review:%s -->", a.HeadSHA, a.BaseSHA, a.Verdict, a.Summary, marker)
}

type commentFinder interface {
	FindComment(context.Context, int, string) (bool, error)
}
type reviewSummaryTool struct{ m Manager }

func (reviewSummaryTool) Name() string       { return "scm.pull_request.review" }
func (reviewSummaryTool) Capability() string { return CapReviewSummary }
func (reviewSummaryTool) Description() string {
	return "Publish a review summary for the exact PR head/base reviewed, including an explicit verdict and findings or a no-findings explanation. One summary per connection revision and PR revision pair. Retries reconcile an uncertain publication by lookup without reposting. This is a review summary comment, not a provider approval or a merge."
}
func (reviewSummaryTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"connection":{"type":"string"},"connectionRevision":{"type":"string","description":"Connection revision from the delivery event or read result; refuses reconfigured connections"},"number":{"type":"integer","minimum":1},"headSha":{"type":"string"},"baseSha":{"type":"string"},"verdict":{"type":"string","enum":["approved","changes_requested"]},"summary":{"type":"string","minLength":1,"maxLength":15872}},"required":["connection","number","headSha","baseSha","verdict","summary"],"additionalProperties":false}`)
}
func (t reviewSummaryTool) Invoke(ctx context.Context, inv *tools.Invocation) (*tools.Result, error) {
	var a summaryArgs
	if err := json.Unmarshal(inv.Args, &a); err != nil {
		return nil, err
	}
	if err := tools.CheckRepository(inv.Grant, a.Connection); err != nil {
		return nil, err
	}
	fail := func(err error) (*tools.Result, error) { return &tools.Result{IsError: true, Content: err.Error()}, nil }
	if err := a.validate(); err != nil {
		return fail(err)
	}
	c, err := t.m.Get(ctx, a.Connection)
	if err != nil {
		return fail(err)
	}
	if a.ConnectionRevision != "" && a.ConnectionRevision != c.UpdatedAt.UTC().Format(time.RFC3339Nano) {
		return fail(errors.New("SCM connection changed since the reviewed snapshot"))
	}
	p, token, err := t.m.Open(c)
	if err != nil {
		return fail(err)
	}
	if strings.Contains(a.Summary, token) {
		return fail(errors.New("review summary contains protected credential material"))
	}
	reader, ok := p.(reviewReader)
	finder, findOK := p.(commentFinder)
	if !ok || !findOK {
		return fail(errors.New("review summary publication is unavailable for this adapter"))
	}
	snapshot, err := reader.ReadPullRequest(ctx, a.Number)
	if err != nil {
		return fail(errors.New(redact(err, token)))
	}
	if snapshot.HeadSHA != a.HeadSHA || snapshot.BaseSHA != a.BaseSHA || snapshot.State != "open" || snapshot.Draft {
		return fail(errors.New("pull request is not open and ready at the reviewed revisions; read and review again"))
	}
	snapshot.URL = redactReviewURL(snapshot.URL, token)
	key := reviewReceiptKey(c, a)
	var receipt ReviewReceipt
	var revision int64
	stored, err := t.m.Store.GetSetting(ctx, key)
	fresh := errors.Is(err, storage.ErrNotFound)
	if err != nil && !fresh {
		return fail(errors.New("review publication record could not be read; no comment was sent"))
	}
	if !fresh {
		revision = stored.Revision
		if json.Unmarshal(stored.Value, &receipt) != nil || receipt.Digest != summaryDigest(a) || receipt.Marker == "" || receipt.Connection != c.Name || receipt.Provider != c.Kind || receipt.Repository != c.Repository || receipt.Number != a.Number || receipt.HeadSHA != a.HeadSHA || receipt.BaseSHA != a.BaseSHA || receipt.Verdict != a.Verdict || (receipt.Status != "pending" && receipt.Status != "published") {
			return fail(errors.New("a different or invalid review is already recorded for these revisions; no comment was sent"))
		}
	} else {
		receipt = ReviewReceipt{a.Connection, c.Kind, c.Repository, a.Number, a.HeadSHA, a.BaseSHA, a.Verdict, "pending", snapshot.URL, summaryDigest(a), domain.NewID("review"), time.Now().UTC()}
		raw, err := json.Marshal(receipt)
		if err != nil {
			return nil, err
		}
		revision, err = t.m.Store.PutSetting(ctx, key, raw, 0, inv.Principal.Actor)
		if err != nil {
			return fail(errors.New("review publication record could not be reserved; retry to reconcile without a duplicate comment"))
		}
	}
	body := summaryBody(a, receipt.Marker)
	if fresh {
		// Once the attempt exists, even an ambiguous network failure is read-only on
		// retry. No process-local lock or happy-path-only deduplication is relied on.
		if err = p.Comment(ctx, a.Number, body); err != nil {
			return fail(errors.New("review publication is unconfirmed; retry the same review to reconcile by lookup: " + redact(err, token)))
		}
	}
	found, err := finder.FindComment(ctx, a.Number, body)
	if err != nil {
		return fail(errors.New("review publication could not be reconciled: " + redact(err, token)))
	}
	if !found {
		return fail(errors.New("review comment is not confirmed; the attempt is retained and will not be blindly posted again"))
	}
	if receipt.Status != "published" {
		receipt.Status = "published"
		raw, err := json.Marshal(receipt)
		if err != nil {
			return nil, err
		}
		if _, err = t.m.Store.PutSetting(ctx, key, raw, revision, inv.Principal.Actor); err != nil {
			return fail(errors.New("comment exists but its publication record could not be confirmed; retry the same review"))
		}
		t.m.event(ctx, inv.Principal, domain.EvReviewSummaryPublished, map[string]any{"connection": c.Name, "provider": c.Kind, "repository": c.Repository, "number": a.Number, "headSha": a.HeadSHA, "baseSha": a.BaseSHA, "verdict": a.Verdict, "url": snapshot.URL})
	}
	data, err := json.Marshal(map[string]any{"connection": receipt.Connection, "number": receipt.Number, "headSha": receipt.HeadSHA, "baseSha": receipt.BaseSHA, "verdict": receipt.Verdict, "status": receipt.Status, "url": receipt.URL})
	if err != nil {
		return nil, err
	}
	return &tools.Result{Content: string(data), Data: data}, nil
}
