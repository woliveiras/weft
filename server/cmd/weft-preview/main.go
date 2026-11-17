// weft-preview is the small in-container supervisor and stdio HTTP relay.
// It has no access to platform credentials or the Docker API.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 2 && args[0] == "connect" {
		port, e := strconv.Atoi(args[1])
		if e != nil || port < 1024 || port > 65535 {
			return errors.New("invalid port")
		}
		c, e := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", args[1]), 5*time.Second)
		if e != nil {
			return e
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(15 * time.Minute))
		go func() {
			_, _ = io.Copy(c, os.Stdin)
			if tcp, ok := c.(*net.TCPConn); ok {
				_ = tcp.CloseWrite()
			}
		}()
		_, e = io.Copy(os.Stdout, c)
		return e
	}
	if len(args) != 2 || args[0] != "serve" {
		return errors.New("usage: weft-preview serve config.json | connect port")
	}
	var cfg struct {
		Command []string `json:"command"`
		Cwd     string   `json:"cwd"`
		Caches  []string `json:"caches,omitempty"`
	}
	f, e := os.Open(args[1])
	if e != nil {
		return e
	}
	e = json.NewDecoder(f).Decode(&cfg)
	_ = f.Close()
	if e != nil {
		return e
	}
	if len(cfg.Command) == 0 {
		return errors.New("empty command")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	for _, target := range cfg.Caches {
		if !strings.HasPrefix(target, "/home/sandbox/") || strings.Contains(target, "..") {
			return errors.New("invalid cache destination")
		}
		if e = os.MkdirAll(target, 0755); e != nil {
			return fmt.Errorf("prepare private cache: %w", e)
		}
		copyCtx, cancelCopy := context.WithTimeout(ctx, 90*time.Second)
		copy := exec.CommandContext(copyCtx, "cp", "-a", "/weft-preview-cache"+target+"/.", target)
		out, e := copy.CombinedOutput()
		cancelCopy()
		if e != nil {
			return fmt.Errorf("copy private cache: %w: %s", e, out)
		}
	}
	cmd := exec.CommandContext(ctx, cfg.Command[0], cfg.Command[1:]...)
	cmd.Dir = cfg.Cwd
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 5 * time.Second
	return cmd.Run()
}
