//go:build linux || darwin

package toolchain_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Run the distribution entrypoint against disposable child processes. The
// fixture has no container daemon, host firewall binary or privileged mounts.
func TestSandboxJournalSupervision(t *testing.T) {
	source, err := os.ReadFile("../../../deploy/sandbox-daemon/entrypoint.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, rule string }{
		{"collector exits", ""},
		{"forward rule refused", "-I DOCKER-USER -i docker0 ! -o docker0 -d 10.0.0.0/8 -j DROP"},
		{"input rule refused", "-I INPUT -i docker0 -m conntrack ! --ctstate ESTABLISHED,RELATED -j DROP"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write := func(name, body string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0700); err != nil {
					t.Fatal(err)
				}
			}
			write("chown", "exit 0")
			write("dockerd-entrypoint.sh", `trap 'touch "$FIXTURE/daemon-stopped"; exit 0' TERM
touch "$FIXTURE/daemon-ready"
while :; do sleep 0.1; done`)
			write("iptables", `printf '%s\n' "$*" >> "$FIXTURE/calls"
if [ "$1" = -nL ]; then test -f "$FIXTURE/daemon-ready" && test -f "$FIXTURE/journal-ready"; exit $?; fi
if [ "$*" = "$REJECT_RULE" ]; then touch "$FIXTURE/rejected"; exit 1; fi
exit 0`)
			write("weft-workspace-journal", `trap 'touch "$FIXTURE/journal-stopped"; exit 0' TERM
touch "$FIXTURE/journal-ready"
if [ -z "$REJECT_RULE" ]; then
 while [ ! -f "$FIXTURE/ready" ]; do sleep 0.01; done
 exit 3
fi
while :; do sleep 0.1; done`)
			entry := strings.NewReplacer("/run/weft-iptables", dir+"/ready", "/run/weft-workspace-journal.pid", dir+"/journal.pid", "/var/lib/weft/journal", dir+"/journal", "/usr/local/sbin/.iptables-legacy/iptables", dir+"/unused-legacy").Replace(string(source))
			write("entrypoint", entry)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "sh", filepath.Join(dir, "entrypoint"))
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
			cmd.Env = []string{"PATH=" + dir + ":" + os.Getenv("PATH"), "FIXTURE=" + dir, "REJECT_RULE=" + tc.rule}
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil || cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 1 {
				t.Fatalf("entrypoint exit: %v, %s", err, out)
			}
			exists := func(name string) bool {
				t.Helper()
				_, err := os.Stat(filepath.Join(dir, name))
				if err != nil && !errors.Is(err, os.ErrNotExist) {
					t.Fatal(err)
				}
				return err == nil
			}
			if !exists("daemon-stopped") {
				t.Fatal("daemon still running after failure")
			}
			if tc.rule != "" {
				if !exists("rejected") || exists("ready") || !exists("journal-stopped") || !strings.Contains(string(out), "could not install firewall rules") {
					t.Fatalf("rule rejection did not stop both children: %s", out)
				}
				return
			}
			calls, err := os.ReadFile(filepath.Join(dir, "calls"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(out), "workspace journal stopped") || !exists("ready") {
				t.Fatalf("collector failure: %s", out)
			}
			for _, rule := range []string{"-I DOCKER-USER", "-I INPUT", "-j DROP"} {
				if !strings.Contains(string(calls), rule) {
					t.Errorf("missing rule %q", rule)
				}
			}
		})
	}
}
