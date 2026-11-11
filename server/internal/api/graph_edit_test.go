package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// callIfMatch sends a JSON graph with an If-Match version.
func callIfMatch(t *testing.T, srv *httptest.Server, method, path, tok, body string, version int, out any) int {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", strconv.Itoa(version))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if out != nil && len(b) > 0 {
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatalf("%s %s: decode %q: %v", method, path, b, err)
		}
	}
	return resp.StatusCode
}

// Editors round-trip documents they only partly understand: extension keys
// survive every save, and keys the schema does not define are rejected with
// a path instead of being dropped.
func TestGraphDocumentFieldsAreKeptOrRejected(t *testing.T) {
	srv := setup(t)
	const extDoc = `
apiVersion: weft.io/v1
kind: Graph
x-editor: {zoom: 2}
metadata: {name: ext-demo, x-owner: platform}
spec:
  defaults: {runtime: {kind: echo}}
  nodes: [{id: a, x-color: blue, position: {x: 10, y: 20}}]
  channels: [{id: in, to: [a], x-note: keep}]
`
	type issue struct{ Path, Message string }
	var bad struct {
		Code    string
		Details struct {
			Valid  bool
			Errors []issue
		}
	}

	typo := strings.Replace(extDoc, "x-color: blue", "colour: blue", 1)
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", typo, &bad); c != 422 {
		t.Fatalf("create with unknown field: %d %+v", c, bad)
	}
	if bad.Code != "invalid" || len(bad.Details.Errors) != 1 || bad.Details.Errors[0].Path != "spec.nodes[0].colour" {
		t.Fatalf("unknown field report: %+v", bad)
	}
	if c := call(t, srv, "GET", "/api/v1/graphs/ext-demo", viewerTok, "", "", nil); c != 404 {
		t.Fatalf("rejected graph was stored: %d", c)
	}

	// Validation reports unknown keys alongside semantic errors.
	var rep struct {
		Valid  bool
		Errors []issue
	}
	both := strings.Replace(typo, "to: [a]", "to: [ghost]", 1)
	if c := call(t, srv, "POST", "/api/v1/graphs/validate", viewerTok, "application/yaml", both, &rep); c != 200 || rep.Valid {
		t.Fatalf("validate: %d %+v", c, rep)
	}
	paths := map[string]bool{}
	for _, e := range rep.Errors {
		paths[e.Path] = true
	}
	if !paths["spec.nodes[0].colour"] || !paths["spec.channels[0].to[0]"] {
		t.Fatalf("validate paths: %+v", rep.Errors)
	}

	// Extensions survive create, read, JSON update and YAML export.
	var sum struct {
		Graph  map[string]any
		Record struct{ Version int }
	}
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", extDoc, &sum); c != 201 {
		t.Fatalf("create: %d", c)
	}
	if c := call(t, srv, "GET", "/api/v1/graphs/ext-demo", viewerTok, "", "", &sum); c != 200 {
		t.Fatalf("get: %d", c)
	}
	assertExt := func(g map[string]any) {
		t.Helper()
		spec := g["spec"].(map[string]any)
		node := spec["nodes"].([]any)[0].(map[string]any)
		ch := spec["channels"].([]any)[0].(map[string]any)
		meta := g["metadata"].(map[string]any)
		if g["x-editor"] == nil || meta["x-owner"] != "platform" || node["x-color"] != "blue" || ch["x-note"] != "keep" {
			t.Fatalf("extensions lost: %v", g)
		}
	}
	assertExt(sum.Graph)

	// A JSON save from an editor that changed only a position keeps the rest.
	g := sum.Graph
	g["spec"].(map[string]any)["nodes"].([]any)[0].(map[string]any)["position"] = map[string]any{"x": 50, "y": 60}
	body, _ := json.Marshal(g)
	if c := callIfMatch(t, srv, "PUT", "/api/v1/graphs/ext-demo", adminTok, string(body), sum.Record.Version, &sum); c != 200 {
		t.Fatalf("update: %d", c)
	}
	assertExt(sum.Graph)
	if sum.Record.Version != 2 {
		t.Fatalf("version: %d", sum.Record.Version)
	}

	// Stale editors get a recoverable conflict and the stored version is untouched.
	if c := callIfMatch(t, srv, "PUT", "/api/v1/graphs/ext-demo", adminTok, string(body), 1, &bad); c != 409 || bad.Code != "conflict" {
		t.Fatalf("stale update: %d %+v", c, bad)
	}
	// Unknown keys on update are rejected without creating a version.
	g["spec"].(map[string]any)["nodes"].([]any)[0].(map[string]any)["promt"] = "typo"
	body, _ = json.Marshal(g)
	if c := callIfMatch(t, srv, "PUT", "/api/v1/graphs/ext-demo", adminTok, string(body), 2, &bad); c != 422 || bad.Details.Errors[0].Path != "spec.nodes[0].promt" {
		t.Fatalf("update with unknown field: %d %+v", c, bad)
	}
	var versions []map[string]any
	call(t, srv, "GET", "/api/v1/graphs/ext-demo/versions", viewerTok, "", "", &versions)
	if len(versions) != 2 {
		t.Fatalf("versions: %d", len(versions))
	}

	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/graphs/ext-demo/export", nil)
	req.Header.Set("Authorization", "Bearer "+viewerTok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	exported, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(exported), "x-color: blue") || !strings.Contains(string(exported), "x-editor:") {
		t.Fatalf("export: %d %s", resp.StatusCode, exported)
	}
}

// Repository URLs entered in the console must point at a remote host: a
// local path would clone another graph's workspace or platform files.
func TestGraphRepositoryMustBeRemote(t *testing.T) {
	srv := setup(t)
	const repoDoc = `
apiVersion: weft.io/v1
kind: Graph
metadata: {name: repo-demo}
spec:
  workspace: {repo: REPO}
  defaults: {runtime: {kind: echo}}
  nodes: [{id: a}]
`
	var bad struct {
		Details struct {
			Errors []struct{ Path, Message string }
		}
	}
	for _, repo := range []string{"/var/lib/weft/workspaces/other", "'file:///etc'", "'ext::sh -c id'"} {
		if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", strings.Replace(repoDoc, "REPO", repo, 1), &bad); c != 422 ||
			len(bad.Details.Errors) == 0 || bad.Details.Errors[0].Path != "spec.workspace.repo" {
			t.Fatalf("repo %s: %d %+v", repo, c, bad)
		}
	}
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", strings.Replace(repoDoc, "REPO", "https://github.com/woliveiras/bookaneer.git", 1), nil); c != 201 {
		t.Fatalf("https repo: %d", c)
	}
}
