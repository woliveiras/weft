package scm_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/tools"
)

func TestMergeUnavailableWithoutRemoteCalls(t *testing.T) {
	for _, kind := range []string{"github", "bitbucket-cloud", "bitbucket-dc"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; t.Error("unqualified merge contacted provider") }))
			defer srv.Close()
			m, _ := manager(t)
			if _, err := m.Save(ctx, scm.Input{Name: "review", Kind: kind, Repository: "acme/app", APIURL: srv.URL, Token: ptr(token)}, "admin", false); err != nil {
				t.Fatal(err)
			}
			reg := tools.NewRegistry()
			reg.MustRegister(scm.Tools(m)...)
			gw := tools.NewGateway(reg, nil, nil)
			raw := json.RawMessage(fmt.Sprintf(`{"connection":"review","number":7,"headSha":%q,"baseSha":%q,"reviewSummary":"No findings. Tests passed."}`, strings.Repeat("a", 40), strings.Repeat("b", 40)))
			for _, grant := range []string{"scm.pull_request.merge", "scm.*", "*"} {
				for i := 0; i < 2; i++ {
					res, err := gw.Invoke(ctx, principal("", domain.ToolGrant{Name: grant, Repositories: []string{"review"}}), "scm.pull_request.merge", raw)
					if grant == "scm.pull_request.merge" {
						if err != nil || res == nil || !res.IsError || !strings.Contains(res.Content, "atomic reviewed-head and destination") {
							t.Fatalf("unavailable: %+v %v", res, err)
						}
					} else if err == nil && (res == nil || !res.IsError) {
						t.Fatal("legacy wildcard enabled merge")
					}
				}
			}
			if calls != 0 {
				t.Fatalf("remote calls %d", calls)
			}
		})
	}
}
