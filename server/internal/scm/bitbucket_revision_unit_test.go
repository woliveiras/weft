package scm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBitbucketCloudFullCommitIdentity(t *testing.T) {
	const prefix = "0123456789ab"
	full := prefix + strings.Repeat("c", 28)
	for _, mode := range []string{"full", "abbreviated", "missing", "short", "path", "wrong-prefix", "wrong-repository", "missing-repository", "short-result", "denied", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			h, err := newProviderHTTP("https://api.bitbucket.org/2.0", "synthetic-credential")
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			h.client.Transport = bitbucketRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/2.0/repositories/acme/app/commit/"+prefix || r.URL.RawQuery != "" {
					t.Fatalf("resolution escaped immutable commit: %s %s", r.Method, r.URL)
				}
				hash, repo, status := full, "acme/app", 200
				switch mode {
				case "wrong-prefix":
					hash = strings.Repeat("d", 40)
				case "wrong-repository":
					repo = "fork/app"
				case "missing-repository":
					repo = ""
				case "short-result":
					hash = prefix
				case "denied":
					status = 403
				}
				body := fmt.Sprintf(`{"hash":%q,"repository":{"full_name":%q},"message":"synthetic-credential"}`, hash, repo)
				if mode == "malformed" {
					body = "{"
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			b := &bitbucketCloud{http: h, c: Connection{Repository: "acme/app"}}
			input := prefix
			switch mode {
			case "full":
				input = full
			case "missing":
				input = ""
			case "short":
				input = "0123456"
			case "path":
				input = "../main"
			}
			got, err := b.fullCommit(context.Background(), input)
			valid := mode == "full" || mode == "abbreviated"
			if valid && (err != nil || got != full) || !valid && (err == nil || got != "") {
				t.Fatalf("resolution: hash=%q error=%v", got, err)
			}
			wantCalls := 1
			if mode == "full" || mode == "missing" || mode == "short" || mode == "path" {
				wantCalls = 0
			}
			if calls != wantCalls || err != nil && strings.Contains(err.Error(), "synthetic-credential") {
				t.Fatalf("unsafe resolution: calls=%d error=%v", calls, err)
			}
		})
	}
}
