//go:build nativerecovery && (darwin || linux)

package localinstall_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"maps"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestNativeProfileRecovery(t *testing.T) {

	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	temp := t.TempDir()
	run := func(dir, bin string, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), bin, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s failed: %v, %s", filepath.Base(bin), err, out)
		}
	}
	current := filepath.Join(temp, "weftd")
	run(filepath.Join(repo, "server"), "go", "build", "-o", current, "./cmd/weftd")
	web := filepath.Join(temp, "web")
	if err := os.Mkdir(web, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("<title>Upgrade fixture</title>"), 0600); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(temp, "profile")
	run(temp, current, "--init-local", "--data-dir", profile, "--web-dir", web, "--port", port)
	token, err := os.ReadFile(filepath.Join(profile, "admin.token"))
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + addr
	client := &http.Client{Timeout: 5 * time.Second}
	call := func(method, path, body string, status int) map[string]any {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != status {
			t.Fatalf("%s %s: %d", method, path, res.StatusCode)
		}
		var raw json.RawMessage
		if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(path, "/executions") {
			if !bytes.Equal(bytes.TrimSpace(raw), []byte("[]")) {
				t.Fatal("unexpected execution on restart")
			}
			return nil
		}
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err != nil {
			t.Fatal(err)
		}
		return obj
	}
	withServer := func(binary string, verify func()) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), binary, "--local", "--data-dir", profile)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		defer func() {
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
				t.Errorf("stop: %v", err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("server exit: %v", err)
				}
			case <-time.After(15 * time.Second):
				_ = cmd.Process.Kill()
				<-done
				t.Error("server did not stop gracefully")
			}
		}()
		deadline := time.Now().Add(10 * time.Second)
		for {
			res, err := client.Get(base + "/healthz")
			if err == nil {
				res.Body.Close()
				if res.StatusCode == 200 {
					break
				}
			}
			if time.Now().After(deadline) {
				t.Fatal("native server did not become healthy")
			}
			time.Sleep(50 * time.Millisecond)
		}
		verify()
	}
	graph := "/api/v1/graphs/upgrade-fixture"
	factoryPath := "/api/v1/graphs/software-factory"
	chatID := strings.Repeat("ab", 16)
	chat := "/api/v1/chat/sessions/" + chatID
	var original, factory, archived map[string]any
	withServer(current, func() {
		call("POST", "/api/v1/graphs", `{"metadata":{"id":"upgrade-fixture","name":"Preserve this graph"},"spec":{"nodes":[{"id":"worker","runtime":{"kind":"echo"},"loop":{"mode":"once"},"tools":[]}]}}`, 201)
		original = call("GET", graph, "", 200)
	})
	run(temp, current, "--backup-local", filepath.Join(temp, "first-backup.tar.gz"), "--data-dir", profile)
	withServer(current, func() {
		if !reflect.DeepEqual(call("GET", graph, "", 200), original) {
			t.Fatal("restart changed graph")
		}
		factory = call("GET", factoryPath, "", 200)
		if factory["graph"].(map[string]any)["metadata"].(map[string]any)["name"] != "Software Factory" {
			t.Fatal("missing native factory")
		}
		call("GET", graph+"/executions", "", 200)
	})
	withServer(current, func() {
		if !reflect.DeepEqual(call("GET", graph, "", 200), original) || !reflect.DeepEqual(call("GET", factoryPath, "", 200), factory) {
			t.Fatal("restart changed graph or factory")
		}
		session := call("POST", "/api/v1/chat/sessions", `{"requestId":"`+chatID+`","text":"/inspect workflows"}`, 200)
		if !strings.Contains(session["messages"].([]any)[1].(map[string]any)["text"].(string), "upgrade-fixture") {
			t.Fatal("missing original graph in chat")
		}
		archived = call("POST", chat+"/archive", "", 200)
		if archived["archived"] != true {
			t.Fatal("archive failed")
		}
	})
	backup := filepath.Join(temp, "with-chat.tar.gz")
	run(temp, current, "--backup-local", backup, "--data-dir", profile)
	if err := os.Rename(profile, filepath.Join(temp, "original-profile")); err != nil {
		t.Fatal(err)
	}
	run(temp, current, "--restore-local", backup, "--data-dir", profile)
	withServer(current, func() {
		if !reflect.DeepEqual(call("GET", chat, "", 200), archived) {
			t.Fatal("restored chat differs")
		}
		history := call("GET", "/api/v1/chat/sessions?archived=true", "", 200)["items"].([]any)
		if len(history) != 1 || history[0].(map[string]any)["id"] != chatID {
			t.Fatal("restored archive list differs")
		}
	})
	if err := os.WriteFile(filepath.Join(profile, "installation.json"), []byte(`{"version":999}`), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot := func() map[string][32]byte {
		t.Helper()
		files := map[string][32]byte{}
		err := fs.WalkDir(os.DirFS(profile), ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if !d.Type().IsRegular() {
				return nil
			}
			raw, err := os.ReadFile(filepath.Join(profile, path))
			if err == nil {
				files[path] = sha256.Sum256(raw)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return files
	}
	before := snapshot()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, current, "--local", "--data-dir", profile)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	var exit *exec.ExitError
	if err := cmd.Run(); !errors.As(err, &exit) || ctx.Err() != nil {
		t.Fatalf("unsupported format exit: %v", err)
	}
	if !maps.Equal(before, snapshot()) {
		t.Fatal("failed upgrade changed persistent files")
	}
}
