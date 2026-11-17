package gemini

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/llm"
)

const userAccessToken = "ya29.user-access-token"

// tokenEndpoint is a fake OAuth token endpoint.
func tokenEndpoint(t *testing.T, check func(form url.Values), status int, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		_ = r.ParseForm()
		if check != nil {
			check(r.PostForm)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func writeJSON(t *testing.T, v any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "adc.json")
	b, _ := json.Marshal(v)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeUserCredentials(t *testing.T) string {
	return writeJSON(t, map[string]string{"type": "authorized_user", "client_id": "cid", "client_secret": "csecret",
		"refresh_token": "refresh-me", "quota_project_id": "billing-project"})
}

func useUserTokenURL(t *testing.T, u string) {
	old := userTokenURL
	userTokenURL = u
	t.Cleanup(func() { userTokenURL = old })
}

func TestVertexWithUserCredentials(t *testing.T) {
	tokens, fetched := tokenEndpoint(t, func(form url.Values) {
		if form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != "refresh-me" || form.Get("client_id") != "cid" {
			t.Errorf("token request = %v", form)
		}
	}, 200, `{"access_token":"`+userAccessToken+`","expires_in":3600,"token_type":"Bearer"}`)
	useUserTokenURL(t, tokens.URL)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", writeUserCredentials(t))

	f := newFixture(t)
	p, err := New(llm.ProviderConfig{Name: "vtx", Project: "my-project", Region: "europe-west4", BaseURL: f.URL, DefaultModel: "gemini-test", MaxRetries: -1})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "hi"}}}); err != nil {
			t.Fatal(err)
		}
	}
	r := f.reqs[0]
	if r.URL.Path != "/v1/projects/my-project/locations/europe-west4/publishers/google/models/gemini-test:generateContent" {
		t.Fatalf("path = %s", r.URL.Path)
	}
	if r.Header.Get("Authorization") != "Bearer "+userAccessToken || r.Header.Get("x-goog-user-project") != "billing-project" || r.Header.Get("x-goog-api-key") != "" {
		t.Fatalf("headers = %v", r.Header)
	}
	if strings.Contains(f.raw[0], userAccessToken) || strings.Contains(f.raw[0], "refresh-me") {
		t.Fatal("credential in the request body")
	}
	if fetched.Load() != 1 {
		t.Fatalf("token fetched %d times, want 1 (cached)", fetched.Load())
	}

	// An expiring token is renewed.
	p.vertex.tokens.now = func() time.Time { return time.Now().Add(time.Hour) }
	if _, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "hi"}}}); err != nil {
		t.Fatal(err)
	}
	if fetched.Load() != 2 {
		t.Fatalf("token fetched %d times, want 2", fetched.Load())
	}

	// Errors name the Vertex AI fix and never contain the token.
	f.script(http.StatusForbidden, `{"error":{"code":403,"message":"caller lacks permission; token `+userAccessToken+`","status":"PERMISSION_DENIED"}}`)
	_, err = p.Chat(context.Background(), &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "hi"}}})
	if !errors.Is(err, ErrPermission) || !strings.Contains(err.Error(), "project my-project") || !strings.Contains(err.Error(), "roles/aiplatform.user") ||
		strings.Contains(err.Error(), userAccessToken) {
		t.Fatalf("err = %v", err)
	}
	f.script(http.StatusNotFound, `{"error":{"code":404,"status":"NOT_FOUND"}}`)
	if _, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "hi"}}}); !errors.Is(err, ErrModelNotFound) || !strings.Contains(err.Error(), "region europe-west4") {
		t.Fatalf("err = %v", err)
	}
}

func TestVertexModels(t *testing.T) {
	tokens, _ := tokenEndpoint(t, nil, 200, `{"access_token":"`+userAccessToken+`","expires_in":3600}`)
	useUserTokenURL(t, tokens.URL)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", writeUserCredentials(t))
	f := newFixture(t)
	f.script(http.StatusOK, `{"publisherModels":[{"name":"publishers/google/models/gemini-a"},{"name":"publishers/google/models/imagen-3"}],"nextPageToken":"n"}`,
		`{"publisherModels":[{"name":"publishers/google/models/gemini-b"}]}`)
	p, err := New(llm.ProviderConfig{Name: "vtx", Project: "my-project", BaseURL: f.URL, MaxRetries: -1})
	if err != nil {
		t.Fatal(err)
	}
	ms, err := p.Models(context.Background())
	if err != nil || len(ms) != 2 || ms[0].ID != "gemini-a" || ms[1].ID != "gemini-b" {
		t.Fatalf("models = %+v, %v", ms, err)
	}
	if f.reqs[0].URL.Path != "/v1beta1/publishers/google/models" || f.reqs[1].URL.Query().Get("pageToken") != "n" {
		t.Fatalf("requests = %s, %s", f.reqs[0].URL, f.reqs[1].URL)
	}
	if p.vertex.region != "global" {
		t.Fatalf("default region = %q", p.vertex.region)
	}

	// User credentials without a quota project bill the provider's project.
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", writeJSON(t, map[string]string{"type": "authorized_user", "client_id": "cid", "refresh_token": "r"}))
	p, err = New(llm.ProviderConfig{Name: "vtx", Project: "my-project", BaseURL: f.URL, DefaultModel: "gemini-test", MaxRetries: -1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Models(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.reqs[len(f.reqs)-1].Header.Get("x-goog-user-project"); got != "my-project" {
		t.Fatalf("quota project = %q", got)
	}
}

func TestVertexDefaultEndpoints(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", writeUserCredentials(t))
	for region, want := range map[string]string{"": "https://aiplatform.googleapis.com", "us-central1": "https://us-central1-aiplatform.googleapis.com"} {
		p, err := New(llm.ProviderConfig{Name: "vtx", Project: "my-project", Region: region})
		if err != nil || p.HTTP().BaseURL != want {
			t.Fatalf("region %q: base = %v, %v", region, p, err)
		}
	}
}

func TestServiceAccountAssertion(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	var assertion string
	tokens, _ := tokenEndpoint(t, func(form url.Values) {
		if form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			t.Errorf("grant = %v", form)
		}
		assertion = form.Get("assertion")
	}, 200, `{"access_token":"sa-token","expires_in":3600}`)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", writeJSON(t, map[string]string{
		"type": "service_account", "client_email": "weft@my-project.iam.gserviceaccount.com", "private_key_id": "kid-1",
		"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), "token_uri": tokens.URL,
	}))
	ts, err := findADC(http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := ts.Token(context.Background())
	if err != nil || tok != "sa-token" || ts.quotaProject != "" {
		t.Fatalf("token = %q, %v", tok, err)
	}
	parts := strings.Split(assertion, ".")
	if len(parts) != 3 {
		t.Fatalf("assertion = %q", assertion)
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("signature: %v", err)
	}
	var header, claims map[string]any
	h, _ := base64.RawURLEncoding.DecodeString(parts[0])
	c, _ := base64.RawURLEncoding.DecodeString(parts[1])
	_ = json.Unmarshal(h, &header)
	_ = json.Unmarshal(c, &claims)
	if header["alg"] != "RS256" || header["kid"] != "kid-1" || claims["iss"] != "weft@my-project.iam.gserviceaccount.com" ||
		claims["aud"] != tokens.URL || claims["scope"] != cloudPlatformScope {
		t.Fatalf("header %v claims %v", header, claims)
	}
}

func TestMetadataServer(t *testing.T) {
	md := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Metadata-Flavor") != "Google" || r.URL.Path != "/computeMetadata/v1/instance/service-accounts/default/token" {
			http.Error(w, "bad request", 400)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"md-token","expires_in":1800}`)
	}))
	t.Cleanup(md.Close)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("CLOUDSDK_CONFIG", t.TempDir()) // no well-known file
	t.Setenv("GCE_METADATA_HOST", strings.TrimPrefix(md.URL, "http://"))
	ts, err := findADC(http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	if tok, err := ts.Token(context.Background()); err != nil || tok != "md-token" {
		t.Fatalf("token = %q, %v", tok, err)
	}
}

func TestMissingCredentialsFailWithoutRetry(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("CLOUDSDK_CONFIG", t.TempDir())
	unreachable := httptest.NewServer(http.NotFoundHandler())
	t.Setenv("GCE_METADATA_HOST", strings.TrimPrefix(unreachable.URL, "http://"))
	unreachable.Close()

	f := newFixture(t)
	p, err := New(llm.ProviderConfig{Name: "vtx", Project: "my-project", BaseURL: f.URL, DefaultModel: "gemini-test", MaxRetries: 3})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Chat(context.Background(), &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "hi"}}})
	if !errors.Is(err, llm.ErrCredentials) || !strings.Contains(err.Error(), "gcloud auth application-default login") || len(f.reqs) != 0 {
		t.Fatalf("err = %v after %d requests", err, len(f.reqs))
	}

	// A rejected refresh token is explained without echoing it.
	tokens, _ := tokenEndpoint(t, nil, 400, `{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`)
	useUserTokenURL(t, tokens.URL)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", writeUserCredentials(t))
	p, err = New(llm.ProviderConfig{Name: "vtx", Project: "my-project", BaseURL: f.URL, DefaultModel: "gemini-test"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Models(context.Background())
	if !errors.Is(err, llm.ErrCredentials) || !strings.Contains(err.Error(), "invalid_grant") || strings.Contains(err.Error(), "refresh-me") {
		t.Fatalf("err = %v", err)
	}
}

func TestUnusableCredentialFiles(t *testing.T) {
	for name, tc := range map[string]struct {
		content string
		want    string
	}{
		"unsupported type": {`{"type":"external_account"}`, `credential type "external_account"`},
		"not json":         {`nope`, "not valid JSON"},
		"bad key":          {`{"type":"service_account","client_email":"a@b","private_key":"garbage"}`, "not a PEM block"},
		"no refresh token": {`{"type":"authorized_user","client_id":"x"}`, "no refresh token"},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "adc.json")
			_ = os.WriteFile(path, []byte(tc.content), 0o600)
			t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", path)
			_, err := New(llm.ProviderConfig{Name: "vtx", Project: "my-project"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", filepath.Join(t.TempDir(), "missing.json"))
	if _, err := New(llm.ProviderConfig{Name: "vtx", Project: "my-project"}); err == nil || !strings.Contains(err.Error(), "GOOGLE_APPLICATION_CREDENTIALS") {
		t.Fatalf("err = %v", err)
	}
}
