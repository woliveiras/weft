package scm

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSummaryValidationAndIdentity(t *testing.T) {
	a := summaryArgs{Connection: "app", Number: 7, HeadSHA: strings.Repeat("a", 40), BaseSHA: strings.Repeat("b", 40), Verdict: "approved", Summary: "No findings; tests passed."}
	if err := a.validate(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*summaryArgs){func(a *summaryArgs) { a.Verdict = "clean" }, func(a *summaryArgs) { a.HeadSHA = "HEAD" }, func(a *summaryArgs) { a.BaseSHA = "" }, func(a *summaryArgs) { a.Number = 0 }, func(a *summaryArgs) { a.Summary = " " }, func(a *summaryArgs) { a.Summary = strings.Repeat("x", MaxBody) }} {
		b := a
		change(&b)
		if b.validate() == nil {
			t.Fatalf("invalid accepted: %+v", b)
		}
	}
	c := Connection{Name: "app", Kind: "github", Repository: "acme/app", UpdatedAt: time.Now().UTC()}
	key := reviewReceiptKey(c, a)
	if key != reviewReceiptKey(c, a) {
		t.Fatal("unstable key")
	}
	c.UpdatedAt = c.UpdatedAt.Add(time.Second)
	if key == reviewReceiptKey(c, a) {
		t.Fatal("connection mutation reused receipt")
	}
	if len(summaryBody(a, "marker")) > MaxBody {
		t.Fatal("summary exceeds remote bound")
	}
	b := a
	b.Verdict = "changes_requested"
	if summaryDigest(a) == summaryDigest(b) {
		t.Fatal("verdict not included in digest")
	}
}

func TestReviewURLRedactsEncodedCredentials(t *testing.T) {
	token := `credential<&"value`
	safe := redactReviewURL("https://scm.example/pr/7?reflection="+url.QueryEscape(token), token)
	decoded, err := url.QueryUnescape(safe)
	if err != nil || strings.Contains(decoded, token) {
		t.Fatal("encoded secret escaped URL redaction")
	}
}
