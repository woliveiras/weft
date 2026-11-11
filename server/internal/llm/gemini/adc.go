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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Application Default Credentials, resolved the way Google's client
// libraries do, with the standard library only (golang.org/x/oauth2 is not
// a dependency of the server):
//
//  1. the JSON file named by GOOGLE_APPLICATION_CREDENTIALS;
//  2. gcloud's well-known file (`gcloud auth application-default login`);
//  3. the metadata server of the Google Cloud machine the server runs on.
//
// Service-account keys and gcloud user credentials are supported; other
// credential types (workload identity federation, impersonation) fail with
// an explanation. Tokens live only in this process: they are added to the
// outgoing request and never reach runtimes, prompts, events or the browser.

const (
	cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"
	googleTokenURL     = "https://oauth2.googleapis.com/token"
	defaultMetadata    = "metadata.google.internal"
	// expiryMargin renews a token this long before it expires.
	expiryMargin = time.Minute
	maxTokenBody = 1 << 20
)

type credentialsFile struct {
	Type string `json:"type"`
	// service_account
	ClientEmail  string `json:"client_email"`
	PrivateKey   string `json:"private_key"`
	PrivateKeyID string `json:"private_key_id"`
	TokenURI     string `json:"token_uri"`
	// authorized_user
	ClientID       string `json:"client_id"`
	ClientSecret   string `json:"client_secret"`
	RefreshToken   string `json:"refresh_token"`
	QuotaProjectID string `json:"quota_project_id"`
}

// tokenSource returns OAuth access tokens for one ADC identity.
type tokenSource struct {
	client *http.Client
	now    func() time.Time
	// fetch obtains a fresh token.
	fetch func(ctx context.Context) (token, error)
	// user is set for gcloud user credentials, which need a quota project
	// (x-goog-user-project); quotaProject is the one they name, if any.
	user         bool
	quotaProject string

	mu  sync.Mutex
	tok token
}

type token struct {
	value  string
	expiry time.Time
}

// findADC locates and parses the server's Application Default Credentials.
// Only the file lookup happens here; no network call is made until the
// first request. With no file, the metadata server is used.
func findADC(client *http.Client) (*tokenSource, error) {
	ts := &tokenSource{client: client, now: time.Now}
	path, explicit := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"), true
	if path == "" {
		path, explicit = wellKnownFile(), false
	}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
	case explicit:
		return nil, fmt.Errorf("read GOOGLE_APPLICATION_CREDENTIALS: %w", err)
	case errors.Is(err, os.ErrNotExist):
		ts.fetch = ts.metadataToken
		return ts, nil
	default:
		return nil, fmt.Errorf("read Application Default Credentials %s: %w", path, err)
	}
	var f credentialsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("Application Default Credentials file %s is not valid JSON", path)
	}
	switch f.Type {
	case "service_account":
		key, err := parseKey(f.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("service account key in %s: %w", path, err)
		}
		if f.ClientEmail == "" {
			return nil, fmt.Errorf("service account key in %s has no client_email", path)
		}
		if f.TokenURI == "" {
			f.TokenURI = googleTokenURL
		}
		ts.fetch = func(ctx context.Context) (token, error) { return ts.serviceAccountToken(ctx, f, key) }
	case "authorized_user":
		if f.RefreshToken == "" || f.ClientID == "" {
			return nil, fmt.Errorf("user credentials in %s have no refresh token; run gcloud auth application-default login again", path)
		}
		ts.user, ts.quotaProject = true, f.QuotaProjectID
		ts.fetch = func(ctx context.Context) (token, error) { return ts.userToken(ctx, f) }
	default:
		return nil, fmt.Errorf("credential type %q in %s is not supported; use a service account key, gcloud application-default user credentials, or the metadata server of a Google Cloud machine", f.Type, path)
	}
	return ts, nil
}

func wellKnownFile() string {
	if dir := os.Getenv("CLOUDSDK_CONFIG"); dir != "" {
		return filepath.Join(dir, "application_default_credentials.json")
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("APPDATA"), "gcloud", "application_default_credentials.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "gcloud", "application_default_credentials.json")
}

func parseKey(pemKey string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		return nil, errors.New("private_key is not a PEM block")
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rk, ok := k.(*rsa.PrivateKey); ok {
			return rk, nil
		}
		return nil, errors.New("private_key is not an RSA key")
	}
	k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("private_key cannot be parsed")
	}
	return k, nil
}

// Token returns a valid access token, fetching a new one when the cached
// one is about to expire. Concurrent callers share one fetch.
func (ts *tokenSource) Token(ctx context.Context) (string, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.tok.value != "" && ts.now().Add(expiryMargin).Before(ts.tok.expiry) {
		return ts.tok.value, nil
	}
	t, err := ts.fetch(ctx)
	if err != nil {
		return "", err
	}
	ts.tok = t
	return t.value, nil
}

func (ts *tokenSource) serviceAccountToken(ctx context.Context, f credentialsFile, key *rsa.PrivateKey) (token, error) {
	now := ts.now()
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	if f.PrivateKeyID != "" {
		header["kid"] = f.PrivateKeyID
	}
	claims := map[string]any{"iss": f.ClientEmail, "scope": cloudPlatformScope, "aud": f.TokenURI,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix()}
	unsigned := enc(header) + "." + enc(claims)
	sum := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return token{}, fmt.Errorf("sign service account assertion: %w", err)
	}
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {unsigned + "." + base64.RawURLEncoding.EncodeToString(sig)},
	}
	return ts.exchange(ctx, f.TokenURI, form)
}

// userTokenURL is where gcloud user credentials are refreshed (tests
// replace it).
var userTokenURL = googleTokenURL

func (ts *tokenSource) userToken(ctx context.Context, f credentialsFile) (token, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {f.ClientID},
		"client_secret": {f.ClientSecret},
		"refresh_token": {f.RefreshToken},
	}
	return ts.exchange(ctx, userTokenURL, form)
}

func (ts *tokenSource) exchange(ctx context.Context, endpoint string, form url.Values) (token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return ts.do(req, "token endpoint")
}

func (ts *tokenSource) metadataToken(ctx context.Context) (token, error) {
	host := os.Getenv("GCE_METADATA_HOST")
	if host == "" {
		host = defaultMetadata
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+host+"/computeMetadata/v1/instance/service-accounts/default/token", nil)
	if err != nil {
		return token{}, err
	}
	req.Header.Set("Metadata-Flavor", "Google")
	t, err := ts.do(req, "metadata server")
	if err != nil && ctx.Err() == nil {
		return token{}, fmt.Errorf("no Application Default Credentials found (%v): set GOOGLE_APPLICATION_CREDENTIALS to a service account key, run gcloud auth application-default login for the user running the server, or run it on Google Cloud", err)
	}
	return t, err
}

// do performs a token request. Error bodies are reduced to the OAuth error
// code and description, which never contain the credential.
func (ts *tokenSource) do(req *http.Request, what string) (token, error) {
	resp, err := ts.client.Do(req)
	if err != nil {
		if ctxErr := req.Context().Err(); ctxErr != nil {
			return token{}, ctxErr
		}
		return token{}, fmt.Errorf("%s unreachable: %v", what, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenBody))
	if err != nil {
		return token{}, fmt.Errorf("read %s response: %w", what, err)
	}
	var out struct {
		AccessToken      string `json:"access_token"`
		ExpiresIn        int    `json:"expires_in"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &out)
	if resp.StatusCode != http.StatusOK || out.AccessToken == "" {
		reason := strings.TrimSpace(strings.Trim(out.Error+": "+out.ErrorDescription, ": "))
		if reason == "" {
			reason = "no access token in the response"
		}
		return token{}, fmt.Errorf("%s answered HTTP %d: %s", what, resp.StatusCode, reason)
	}
	if out.ExpiresIn <= 0 {
		out.ExpiresIn = 300
	}
	return token{value: out.AccessToken, expiry: ts.now().Add(time.Duration(out.ExpiresIn) * time.Second)}, nil
}
