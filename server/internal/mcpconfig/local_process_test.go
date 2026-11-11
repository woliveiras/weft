package mcpconfig

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/tools"
)

func localInput(t *testing.T) Input {
	spec := processFixture(t, "success")
	return Input{ID: "local", Name: "Local fixture", Service: "generic", Enabled: true, LocalProcess: &LocalProcess{Command: spec.Command, Args: spec.Args, Directory: spec.Directory}}
}

func TestLocalProcessConfigurationBoundaries(t *testing.T) {
	in := localInput(t)
	for _, field := range []string{"endpoint", "oauth", "credential", "profile", "service", "orphan-env"} {
		t.Run(field, func(t *testing.T) {
			copy := in
			switch field {
			case "endpoint":
				copy.Endpoint = "https://example.com"
			case "oauth":
				copy.OAuth = &OAuthConfig{}
			case "credential":
				copy.Credential = &Credential{}
			case "profile":
				copy.TransportProfile = statelessProfile
			case "service":
				copy.Service = "github"
			case "orphan-env":
				copy.LocalProcess = nil
				env := map[string]string{}
				copy.ProcessEnvironment = &env
			}
			if !errors.Is(validate(copy), ErrInvalid) {
				t.Fatal("invalid local configuration accepted")
			}
		})
	}
	m := testManager(t)
	if _, err := m.Put(t.Context(), in, 0, "admin"); !errors.Is(err, ErrInvalid) {
		t.Fatal("non-native registration allowed", err)
	}
}

func TestLocalProcessStorageGatewayIntegration(t *testing.T) {
	m := testManager(t)
	m.AllowLocalProcesses = true
	in := localInput(t)
	env := map[string]string{"FIXTURE_SECRET": "synthetic-private-fixture-value"}
	in.ProcessEnvironment = &env
	v, err := m.Put(t.Context(), in, 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := m.Store.GetSetting(t.Context(), prefix+in.ID)
	if err != nil {
		t.Fatal(err)
	}
	public, _ := json.Marshal(v)
	if strings.Contains(string(stored.Value), env["FIXTURE_SECRET"]) || strings.Contains(string(public), env["FIXTURE_SECRET"]) || len(v.EnvironmentKeys) != 1 {
		t.Fatal("environment storage/public boundary")
	}
	v, err = m.Refresh(t.Context(), in.ID, "admin")
	if err != nil || v.Status != "ready" {
		t.Fatal(v, err)
	}
	audit := &auditLog{}
	gw := tools.NewGateway(tools.NewRegistry(), nil, audit)
	gw.ResolveTool = m.Resolve
	p := &tools.Principal{GraphID: "g", NodeID: "worker", ExecutionID: "e"}
	if _, err := gw.Invoke(t.Context(), p, "mcp.local.inspect", []byte(`{}`)); !errors.Is(err, tools.ErrDenied) {
		t.Fatal("ungranted call", err)
	}
	p.Grants = []domain.ToolGrant{{Name: "mcp.local.inspect"}}
	res, err := gw.Invoke(t.Context(), p, "mcp.local.inspect", []byte(`{}`))
	if err != nil || !strings.Contains(res.Content, "fixture result") {
		t.Fatal(res, err)
	}
	p.Grants[0].Hosts = []string{"example.com"}
	if _, err := gw.Invoke(t.Context(), p, "mcp.local.inspect", []byte(`{}`)); !errors.Is(err, tools.ErrDenied) {
		t.Fatal("host constraint silently ignored", err)
	}
	p.Grants[0].Hosts = nil
	in.ProcessEnvironment = nil
	changed := *in.LocalProcess
	changed.Args = append(append([]string{}, changed.Args...), "changed")
	in.LocalProcess = &changed
	if _, err := m.Put(t.Context(), in, v.Revision, "admin"); !errors.Is(err, ErrInvalid) {
		t.Fatal("environment transferred to new command", err)
	}
	empty := map[string]string{}
	in.ProcessEnvironment = &empty
	v, err = m.Put(t.Context(), in, v.Revision, "admin")
	if err != nil || len(v.Tools) != 0 || len(v.EnvironmentKeys) != 0 {
		t.Fatal(v, err)
	}
	if _, err := gw.Invoke(t.Context(), p, "mcp.local.inspect", []byte(`{}`)); !errors.Is(err, tools.ErrDenied) {
		t.Fatal("stale catalogue accepted", err)
	}
	m.AllowLocalProcesses = false
	if _, err := m.Refresh(t.Context(), in.ID, "admin"); err == nil {
		current, _ := m.Get(t.Context(), in.ID)
		if current.Status == "ready" {
			t.Fatal("imported native connection executed on server profile")
		}
	}
}

func TestLocalProcessQuotaAndDisableIntegration(t *testing.T) {
	m := testManager(t)
	m.AllowLocalProcesses = true
	in := localInput(t)
	in.CallsPerMinute = 1
	if _, err := m.Put(t.Context(), in, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	v, err := m.Refresh(t.Context(), in.ID, "admin")
	if err != nil || v.Status != "ready" {
		t.Fatal(v, err)
	}
	gw := tools.NewGateway(tools.NewRegistry(), nil, &auditLog{})
	gw.ResolveTool = m.Resolve
	p := &tools.Principal{GraphID: "g", NodeID: "worker", ExecutionID: "e", Grants: []domain.ToolGrant{{Name: "mcp.local.inspect"}}}
	if _, err := gw.Invoke(t.Context(), p, "mcp.local.inspect", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.Invoke(t.Context(), p, "mcp.local.inspect", []byte(`{}`)); !errors.Is(err, tools.ErrRateLimited) {
		t.Fatal("quota bypass", err)
	}
	in.Enabled = false
	if _, err := m.Put(t.Context(), in, v.Revision, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.Invoke(t.Context(), p, "mcp.local.inspect", []byte(`{}`)); !errors.Is(err, tools.ErrDenied) {
		t.Fatal("disabled process accepted", err)
	}
}
