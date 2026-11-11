package repositoryimport

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBridgeRestrictsMethodsHeadersAndTransfer(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.String() != importtestURL+"/info/refs?service=git-upload-pack" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("unsafe request", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/x-git-upload-pack-advertisement"}}, Body: io.NopCloser(strings.NewReader("01234567890123456789"))}, nil
	})}
	var bytes atomic.Int64
	endpoint, close, status, err := bridge(t.Context(), importtestURL, client, 8, &bytes)
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	req, _ := http.NewRequest("GET", endpoint+"/info/refs?service=git-upload-pack", nil)
	req.Header.Set("Authorization", "Bearer synthetic-should-not-forward")
	req.Header.Set("Cookie", "fixture=secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if status.Load() != 413 || bytes.Load() <= 8 {
		t.Fatal("transfer limit not applied")
	}
	for _, path := range []string{"/git-receive-pack", "/../secret", "/info/refs?service=git-receive-pack"} {
		r, err := http.Get(endpoint + path)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != 403 {
			t.Fatal(r.StatusCode)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("unsupported request forwarded")
	}
}

const importtestURL = "https://repository.fixture.example/source.git"

func TestBridgeRefusesAuthenticationRedirectsAndNonGit(t *testing.T) {
	for _, code := range []int{401, 403, 302, 200} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader("sensitive upstream message"))}, nil
			})}
			var received atomic.Int64
			endpoint, close, status, err := bridge(context.Background(), importtestURL, client, 1024, &received)
			if err != nil {
				t.Fatal(err)
			}
			defer close()
			resp, err := http.Get(endpoint + "/info/refs?service=git-upload-pack")
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if strings.Contains(string(b), "sensitive") || status.Load() == 0 {
				t.Fatal("unsafe upstream response", string(b))
			}
		})
	}
}
func TestGitDoesNotInheritHostConfiguration(t *testing.T) {
	home := t.TempDir()
	global := filepath.Join(home, ".gitconfig")
	if err := os.WriteFile(global, []byte("[alias]\n exposed = secret-fixture\n[credential]\n helper = secret-helper\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "alias.injected")
	t.Setenv("GIT_CONFIG_VALUE_0", "secret-env")
	dir := t.TempDir()
	if _, err := safeGit(t.Context(), dir, "config", "--get", "alias.exposed"); err == nil {
		t.Fatal("host global configuration inherited")
	}
	if _, err := safeGit(t.Context(), dir, "config", "--get", "alias.injected"); err == nil {
		t.Fatal("host environment configuration inherited")
	}
}
