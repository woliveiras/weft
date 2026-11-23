package scm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBitbucketCloudChecksUseCommitStatusCollection(t *testing.T) {
	sha := strings.Repeat("a", 40)
	for _, mode := range []string{"ok", "empty", "unknown", "denied", "malformed", "missing-values"} {
		t.Run(mode, func(t *testing.T) {
			h, err := newProviderHTTP("https://api.bitbucket.org/2.0", "synthetic")
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			h.client.Transport = bitbucketRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/2.0/repositories/acme/app/commit/"+sha+"/statuses" || r.URL.RawQuery != "pagelen=100" {
					t.Fatalf("not the complete immutable commit-status collection: %s %s", r.Method, r.URL)
				}
				status, body := 200, `{"values":[{"key":"build","state":"SUCCESSFUL"}]}`
				switch mode {
				case "empty":
					body = `{"values":[]}`
				case "unknown":
					body = `{"values":[{"key":"build","state":"NEW_VENDOR_STATE"}]}`
				case "denied":
					status, body = 403, `{"error":{"message":"synthetic"}}`
				case "malformed":
					body = "{"
				case "missing-values":
					body = `{}`
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			b := &bitbucketCloud{http: h, c: Connection{Repository: "acme/app"}}
			out, err := b.PullRequestChecks(context.Background(), ReviewSnapshot{HeadSHA: sha})
			valid := mode == "ok" || mode == "empty" || mode == "unknown"
			if calls != 1 || valid != (err == nil) || out.Complete != valid || out.RequiredRules != "unavailable" {
				t.Fatalf("checks: %+v error=%v calls=%d", out, err, calls)
			}
			if mode == "ok" && (len(out.Items) != 1 || out.Items[0].State != "passed") || mode == "unknown" && (len(out.Items) != 1 || out.Items[0].State != "unknown") {
				t.Fatalf("unsafe reported state: %+v", out)
			}
			if err != nil && strings.Contains(err.Error(), "synthetic") {
				t.Fatalf("credential in failure: %v", err)
			}
		})
	}
}

func TestReportedChecksNeverInferRequiredRulesOrUnknownSuccess(t *testing.T) {
	for _, tc := range []struct{ state, want string }{{"SUCCESSFUL", "passed"}, {"success", "passed"}, {"FAILED", "failed"}, {"INPROGRESS", "pending"}, {"neutral", "unknown"}, {"skipped", "unknown"}, {"NEW_VENDOR_STATE", "unknown"}, {"", "unknown"}} {
		out, err := checkEntry("build", tc.state)
		if err != nil || out.State != tc.want {
			t.Fatalf("%s: %+v %v", tc.state, out, err)
		}
	}
	if _, err := checkEntry("", "success"); err == nil {
		t.Fatal("accepted missing context")
	}
	if _, err := fileEntry("", "", "modified"); err == nil {
		t.Fatal("accepted missing path")
	}
	if _, err := fileEntry("a\x00b", "", "modified"); err == nil {
		t.Fatal("accepted invalid path")
	}
	if _, err := fileEntry(strings.Repeat("x", 4097), "", "modified"); err == nil {
		t.Fatal("accepted excessive path")
	}
	f, err := fileEntry("new.go", "old.go", "renamed")
	if err != nil || f.PreviousPath != "old.go" {
		t.Fatal(f, err)
	}
}

func TestEvidencePaginationPreservesComparisonAndUnfilteredChecks(t *testing.T) {
	for _, tc := range []struct {
		path      string
		topic, ok bool
	}{
		{"/diffstat/a..b?topic=true&page=2&pagelen=100", true, true},
		{"/checks?page=2", false, true},
		{"/diffstat/a..b?topic=false&page=2", true, false},
		{"/diffstat/a..b?page=2", true, false},
		{"/diffstat/a..b?topic=true&path=only.go", true, false},
		{"/checks?q=state=SUCCESSFUL", false, false},
		{"/checks?pagelen=1000", false, false},
		{"/checks?page=1&page=2", false, false},
	} {
		if err := reviewPageQuery(tc.path, tc.topic); (err == nil) != tc.ok {
			t.Fatal(tc, err)
		}
	}
}
