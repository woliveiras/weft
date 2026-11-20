package scm_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/storage"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/tools"
)

func TestReviewSummaryPublicationAndRecovery(t *testing.T) {
	for _, kind := range []string{"github", "bitbucket-cloud", "bitbucket-dc"} {
		for _, mode := range []string{"ok", "timeout-after-write", "failed-write", "unavailable-list", "stale-head", "storage-read", "storage-reserve", "storage-confirm", "pending-comment", "concurrent"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				sha, base := strings.Repeat("a", 40), strings.Repeat("b", 40)
				posts := 0
				body := ""
				denyList := mode == "unavailable-list"
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != "Bearer "+token {
						t.Error("missing credential")
					}
					if strings.HasSuffix(r.URL.Path, "/comments") {
						if r.Method == "POST" {
							posts++
							var input struct {
								Body, Text string
								Content    struct{ Raw string }
							}
							if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
								t.Error(err)
							}
							body = input.Body
							if kind == "bitbucket-cloud" {
								body = input.Content.Raw
							}
							if kind == "bitbucket-dc" {
								body = input.Text
							}
							if mode == "failed-write" {
								body = ""
								w.WriteHeader(503)
								return
							}
							if mode == "timeout-after-write" {
								conn, _, err := w.(http.Hijacker).Hijack()
								if err != nil {
									t.Error(err)
									return
								}
								_ = conn.Close()
								return
							}
							fmt.Fprint(w, `{"id":1}`)
							return
						}
						if denyList {
							w.WriteHeader(503)
							return
						}
						if body == "" {
							switch kind {
							case "github":
								fmt.Fprint(w, `[]`)
							case "bitbucket-cloud":
								fmt.Fprint(w, `{"values":[]}`)
							case "bitbucket-dc":
								fmt.Fprint(w, `{"values":[],"isLastPage":true}`)
							}
							return
						}
						switch kind {
						case "github":
							_ = json.NewEncoder(w).Encode([]any{map[string]any{"body": body}})
						case "bitbucket-cloud":
							_ = json.NewEncoder(w).Encode(map[string]any{"values": []any{map[string]any{"content": map[string]any{"raw": body}, "pending": mode == "pending-comment"}}})
						case "bitbucket-dc":
							_ = json.NewEncoder(w).Encode(map[string]any{"values": []any{map[string]any{"text": body, "state": func() string {
								if mode == "pending-comment" {
									return "PENDING"
								}
								return "OPEN"
							}()}}, "isLastPage": true})
						}
						return
					}
					publishedURL := "https://scm.example/pr/7?reflection=" + url.QueryEscape(token)
					head := sha
					if mode == "stale-head" {
						head = strings.Repeat("c", 40)
					}
					switch kind {
					case "github":
						fmt.Fprintf(w, `{"number":7,"html_url":%q,"state":"open","head":{"ref":"weft/fix","sha":%q,"repo":{"full_name":"acme/app"}},"base":{"ref":"main","sha":%q,"repo":{"full_name":"acme/app"}}}`, publishedURL, head, base)
					case "bitbucket-cloud":
						fmt.Fprintf(w, `{"id":7,"state":"OPEN","source":{"branch":{"name":"weft/fix"},"repository":{"full_name":"acme/app"},"commit":{"hash":%q}},"destination":{"branch":{"name":"main"},"repository":{"full_name":"acme/app"},"commit":{"hash":%q}},"links":{"html":{"href":%q}}}`, head, base, publishedURL)
					case "bitbucket-dc":
						fmt.Fprintf(w, `{"id":7,"state":"OPEN","fromRef":{"id":"refs/heads/weft/fix","latestCommit":%q,"repository":{"slug":"app","project":{"key":"acme"}}},"toRef":{"id":"refs/heads/main","latestCommit":%q,"repository":{"slug":"app","project":{"key":"acme"}}},"links":{"self":[{"href":%q}]}}`, head, base, publishedURL)
					}
				}))
				defer srv.Close()
				m, _ := manager(t)
				if _, err := m.Save(ctx, scm.Input{Name: "review", Kind: kind, Repository: "acme/app", APIURL: srv.URL, Token: ptr(token)}, "admin", false); err != nil {
					t.Fatal(err)
				}
				st := m.Store
				m.Bus = events.NewBus(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
				fault := &reviewFaultStore{Store: m.Store, mode: mode, ready: make(chan struct{})}
				m.Store = fault
				grant := domain.ToolGrant{Name: "scm.pull_request.review", Repositories: []string{"review"}}
				args := json.RawMessage(fmt.Sprintf(`{"connection":"review","number":7,"headSha":%q,"baseSha":%q,"verdict":"approved","summary":"No findings. Verified tests passed."}`, sha, base))
				invoke := func() *tools.Result {
					reg := tools.NewRegistry()
					reg.MustRegister(scm.Tools(m)...)
					gw := tools.NewGateway(reg, nil, nil)
					res, err := gw.Invoke(ctx, principal("", grant), grant.Name, args)
					if err != nil || res == nil {
						t.Fatalf("%+v %v", res, err)
					}
					return res
				}
				var first *tools.Result
				if mode == "concurrent" {
					var wg sync.WaitGroup
					wg.Add(2)
					results := make(chan *tools.Result, 2)
					for i := 0; i < 2; i++ {
						go func() { defer wg.Done(); results <- invoke() }()
					}
					wg.Wait()
					close(results)
					for result := range results {
						if result != nil && !result.IsError {
							first = result
						}
					}
					if first == nil {
						t.Fatal("neither concurrent review published")
					}
				} else {
					first = invoke()
				}
				wantFirstError := mode != "ok" && mode != "concurrent" && !(mode == "pending-comment" && kind == "github")
				if first.IsError != wantFirstError {
					t.Fatalf("first: %+v", first)
				}
				denyList = false
				// Rebuild gateway/provider: deduplication comes from the durable receipt.
				second := invoke()
				wantError := mode == "failed-write" || mode == "stale-head" || (mode == "pending-comment" && kind != "github")
				if second.IsError != wantError {
					t.Fatalf("reconcile: %+v", second)
				}
				originalArgs := args
				args = json.RawMessage(strings.Replace(string(args), `"verdict":"approved"`, `"verdict":"changes_requested"`, 1))
				if changed := invoke(); !changed.IsError {
					t.Fatal("a different review overwrote the receipt")
				}
				args = originalArgs
				reg := tools.NewRegistry()
				reg.MustRegister(scm.Tools(m)...)
				gw := tools.NewGateway(reg, nil, nil)
				if _, err := gw.Invoke(ctx, principal("", domain.ToolGrant{Name: grant.Name, Repositories: []string{"elsewhere"}}), grant.Name, args); err == nil {
					t.Fatal("review scope escaped")
				}
				expectedPosts := 1
				if mode == "stale-head" {
					expectedPosts = 0
				}
				if posts != expectedPosts {
					t.Fatalf("replayed publication: %d", posts)
				}
				if !wantError && !strings.Contains(second.Content, `"status":"published"`) {
					t.Fatal(second.Content)
				}
				if strings.Contains(second.Content, token) {
					t.Fatal("URL secret in result")
				}
				settings, err := st.ListSettings(ctx, "scm/reviews/")
				if err != nil {
					t.Fatal(err)
				}
				for _, setting := range settings {
					var receipt scm.ReviewReceipt
					if json.Unmarshal(setting.Value, &receipt) != nil {
						t.Fatal("bad receipt")
					}
					decoded, _ := url.QueryUnescape(receipt.URL)
					if strings.Contains(decoded, token) {
						t.Fatal("URL secret persisted")
					}
				}
				events, err := st.ListEvents(ctx, storage.EventFilter{Types: []domain.EventType{"scm.review_summary_published"}})
				if err != nil {
					t.Fatal(err)
				}
				for _, event := range events {
					decoded, _ := url.QueryUnescape(string(event.Data))
					if strings.Contains(decoded, token) {
						t.Fatal("URL secret in event")
					}
				}
			})
		}
	}
}

type reviewFaultStore struct {
	storage.Store
	mode   string
	failed bool
	reads  atomic.Int32
	ready  chan struct{}
}

func (s *reviewFaultStore) GetSetting(ctx context.Context, key string) (*storage.Setting, error) {
	if strings.HasPrefix(key, "scm/reviews/") && s.mode == "storage-read" && !s.failed {
		s.failed = true
		return nil, errors.New("storage unavailable")
	}
	if s.mode == "concurrent" && strings.HasPrefix(key, "scm/reviews/") {
		record, err := s.Store.GetSetting(ctx, key)
		n := s.reads.Add(1)
		if n <= 2 {
			if n == 2 {
				close(s.ready)
			}
			<-s.ready
		}
		return record, err
	}
	return s.Store.GetSetting(ctx, key)
}
func (s *reviewFaultStore) PutSetting(ctx context.Context, key string, raw []byte, rev int64, actor string) (int64, error) {
	if strings.HasPrefix(key, "scm/reviews/") && !s.failed && ((s.mode == "storage-reserve" && rev == 0) || (s.mode == "storage-confirm" && rev > 0)) {
		s.failed = true
		return 0, errors.New("storage unavailable")
	}
	return s.Store.PutSetting(ctx, key, raw, rev, actor)
}
