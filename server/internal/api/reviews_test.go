package api_test

import (
	"context"
	"fmt"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/review"
	"os"
	"path/filepath"
	"testing"
)

func TestReviewAPIIdentityAndDecision(t *testing.T) {
	srv, a := setupApp(t, nil)
	ctx := context.Background()
	source := filepath.Join(t.TempDir(), "source")
	if e := os.Mkdir(source, 0755); e != nil {
		t.Fatal(e)
	}
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "g"}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "n", Isolation: domain.IsolationSpec{Mode: domain.IsolationDocker}, Config: map[string]any{"reviewPolicy": map[string]any{"reviewers": []string{"admin"}, "requiredMethod": "local"}}}}}}
	r, e := a.Reviews.Create(ctx, g, "n", 0, "api-review", source, review.Request{Title: "Review", Instructions: "Inspect"})
	if e != nil {
		t.Fatal(e)
	}
	if code := call(t, srv, "GET", "/api/v1/reviews", "", "", "", nil); code != 401 {
		t.Fatal(code)
	}
	if code := call(t, srv, "GET", "/api/v1/reviews", viewerTok, "", "", nil); code != 403 {
		t.Fatal(code)
	}
	var list []map[string]any
	if code := call(t, srv, "GET", "/api/v1/reviews", operatorTok, "", "", &list); code != 200 || len(list) != 0 {
		t.Fatal(code, list)
	}
	body := fmt.Sprintf(`{"state":"approved","method":"local","revision":%d}`, int64(r.Generation))
	if code := call(t, srv, "POST", "/api/v1/reviews/"+r.ID+"/decide", operatorTok, "application/json", body, nil); code != 403 {
		t.Fatal(code)
	}
	if code := call(t, srv, "POST", "/api/v1/reviews/"+r.ID+"/decide", adminTok, "application/json", body, nil); code != 200 {
		t.Fatal(code)
	}
	if code := call(t, srv, "POST", "/api/v1/reviews/"+r.ID+"/decide", adminTok, "application/json", body, nil); code != 409 {
		t.Fatal(code)
	}
	if code := call(t, srv, "GET", "/api/v1/reviews", adminTok, "", "", &list); code != 200 || len(list) != 1 {
		t.Fatal(code, list)
	}
	for _, key := range []string{"source", "directory", "credentials", "graph"} {
		if _, ok := list[0][key]; ok {
			t.Fatalf("internal %s exposed", key)
		}
	}
}
