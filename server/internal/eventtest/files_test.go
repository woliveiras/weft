package eventtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateCaptureFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, _, err := ValidateCaptures(dir); err == nil {
		t.Fatal("missing captures accepted")
	}
	const event = `{"seq":1,"id":"evt_fixture","type":"graph.created","time":"2026-01-01T00:00:00Z"}`
	write("events-a.json", `{"test":"one","events":[`+event+","+event+`]}`)
	write("events-b.json", `{"test":"two","events":[`+strings.Replace(event, "graph.created", "graph.started", 1)+`]}`)
	if n, k, s, err := ValidateCaptures(dir); err != nil || n != 3 || k != 2 || s != 2 {
		t.Fatal(n, k, s, err)
	}
	malformedDate := strings.Replace(event, "2026-01-01T00:00:00Z", "yesterday", 1)
	extraField := strings.Replace(event, `"seq":1`, `"seq":1,"unexpected":true`, 1)
	write("events-a.json", `{"test":"one","events":[`+malformedDate+","+extraField+`]}`)
	_, _, _, err := ValidateCaptures(dir)
	if err == nil || !strings.Contains(err.Error(), "event 1:") || !strings.Contains(err.Error(), "event 2:") {
		t.Fatalf("expected both schema failures: %v", err)
	}
	write("events-a.json", `{"test":"one"}`)
	if _, _, _, err := ValidateCaptures(dir); err == nil {
		t.Fatal("missing events accepted")
	}
	write("events-a.json", `{} trailing`)
	if _, _, _, err := ValidateCaptures(dir); err == nil {
		t.Fatal("malformed capture accepted")
	}
}
