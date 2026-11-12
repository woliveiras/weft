package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
)

func TestSandboxAdmissionHookBeforeDocker(t *testing.T) {
	for _, group := range []bool{false, true} {
		cfg, dir := composeFixture(t)
		cfg.ValidateAdmission = func(context.Context) error { return errors.New("policy revoked") }
		var start func(context.Context) error
		if group {
			s, err := NewComposeSandbox(cfg, fixtureCompanions)
			if err != nil {
				t.Fatal(err)
			}
			start = s.Start
		} else {
			s, err := NewDockerSandbox(cfg)
			if err != nil {
				t.Fatal(err)
			}
			start = s.Start
		}
		if err := start(context.Background()); err == nil || !strings.Contains(err.Error(), "policy revoked") {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(filepath.Join(dir, "calls")); len(b) != 0 {
			t.Fatal("denial invoked Docker", string(b))
		}
	}
}

func TestSandboxRechecksPolicyAfterCapacityAndOnResume(t *testing.T) {
	for _, group := range []bool{false, true} {
		t.Run(map[bool]string{false: "standalone", true: "group"}[group], func(t *testing.T) {
			cfg, dir := composeFixture(t)
			checks := 0
			denyAfterAdmission := true
			revoked := false
			cfg.ValidateAdmission = func(context.Context) error {
				checks++
				if revoked || (denyAfterAdmission && checks == 2) {
					return errors.New("policy revoked")
				}
				return nil
			}
			type lifecycle interface {
				Start(context.Context) error
				Suspend(context.Context) error
				Close() error
			}
			var sb lifecycle
			if group {
				s, err := NewComposeSandbox(cfg, fixtureCompanions)
				if err != nil {
					t.Fatal(err)
				}
				sb = s
			} else {
				s, err := NewDockerSandbox(cfg)
				if err != nil {
					t.Fatal(err)
				}
				sb = s
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := sb.Start(ctx); err == nil {
				t.Fatal("policy change during capacity wait ignored")
			}
			data, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if strings.Contains(string(data), "run ") {
				t.Fatal("started after denial", string(data))
			}
			if err := cfg.Capacity.ReserveGroup(ctx, "other", 128<<20, 1000, 2, func(context.Context) ([]sandboxcapacity.Container, error) { return nil, nil }); err != nil {
				t.Fatal("reservation leaked", err)
			}
			if err := cfg.Capacity.Release(ctx, "other"); err != nil {
				t.Fatal(err)
			}
			denyAfterAdmission = false
			if err := sb.Start(ctx); err != nil {
				t.Fatal(err)
			}
			revoked = true
			if err := sb.Start(ctx); err != nil {
				t.Fatal("policy implicitly revoked active work", err)
			}
			if err := sb.Suspend(ctx); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if err := sb.Start(ctx); err == nil {
				t.Fatal("suspended sandbox ignored policy")
			}
			after, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if string(before) != string(after) {
				t.Fatal("denied resume changed Docker resources")
			}
			revoked = false
			if err := sb.Start(ctx); err != nil {
				t.Fatal(err)
			}
			if err := sb.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
