package jiracloud_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/jiracloud"
	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
	"github.com/woliveiras/weft/server/internal/secrets"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

const fixtureCloud = "11111111-2222-3333-4444-555555555555"
const fixtureToken = "unique-cloud-static-fixture-token"

type fixtureTransport struct {
	t      *testing.T
	remote *url.URL
	base   http.RoundTripper
}

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != "api.atlassian.com" || !strings.HasPrefix(r.URL.Path, "/ex/jira/"+fixtureCloud+"/rest/api/3/") {
		f.t.Error("credentialed request escaped the explicit Cloud API scope")
		return nil, errors.New("unexpected Cloud origin")
	}
	clone := r.Clone(r.Context())
	u := *r.URL
	u.Host, u.Scheme = f.remote.Host, f.remote.Scheme
	clone.URL = &u
	return f.base.RoundTrip(clone)
}

type cloudFixture struct {
	root           string
	store          storage.Store
	box            *secrets.Box
	mcp            *mcpconfig.Manager
	api            *mcpconfig.TrackerAPI
	profiles       jiracloud.Profiles
	profile        jiracloud.Profile
	input          jiracloud.Input
	client         *http.Client
	calls          atomic.Int32
	posts          atomic.Int32
	status         atomic.Int32
	mode           atomic.Int32
	beforeMetadata func()
}

func newCloudFixture(t *testing.T) *cloudFixture {
	t.Helper()
	f := &cloudFixture{root: t.TempDir()}
	f.status.Store(10)
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+fixtureToken {
			t.Error("Cloud transport did not use the separately reviewed static MCP credential")
		}
		if f.mode.Load() == 1 {
			w.WriteHeader(403)
			fmt.Fprint(w, `{"error":"private upstream content"}`)
			return
		}
		switch r.URL.Path {
		case "/ex/jira/" + fixtureCloud + "/rest/api/3/issue/APP-7":
			if r.Method != "GET" || r.URL.Query().Get("fields") != "project,issuetype,status,updated" {
				t.Error("workflow request was not a minimal identity read")
			}
			project := "10001"
			if f.mode.Load() == 2 {
				project = "10099"
			}
			if f.mode.Load() == 6 {
				fmt.Fprintf(w, `{"echo":%q}`, fixtureToken)
				return
			}
			// Extra Cloud ADF is deliberately ignored by the workflow snapshot DTO.
			fmt.Fprintf(w, `{"id":"10007","key":"APP-7","fields":{"project":{"id":%q},"issuetype":{"id":"10002"},"status":{"id":"%d","name":"Review"},"updated":"2026-11-20T12:00:00.000+0000","description":{"type":"doc","content":[]}}}`, project, f.status.Load())
		case "/ex/jira/" + fixtureCloud + "/rest/api/3/issue/10007/transitions":
			if r.Method == "GET" {
				if r.URL.Query().Get("expand") != "transitions.fields" {
					t.Error("missing transition field metadata")
				}
				if f.beforeMetadata != nil {
					f.beforeMetadata()
				}
				if f.mode.Load() == 3 {
					fmt.Fprint(w, `{"transitions":[{"id":"31","name":"Review","to":{"id":"20","name":"Review"},"fields":{"resolution":{"required":true,"hasDefaultValue":true,"name":"Resolution","schema":{"type":"string"}}}}]}`)
					return
				}
				fmt.Fprint(w, `{"transitions":[{"id":"31","name":"Revisão","to":{"id":"20","name":"Review"},"fields":{}}]}`)
				return
			}
			if r.Method != "POST" {
				t.Error("unexpected transition method")
			}
			var body map[string]json.RawMessage
			if json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 1 || string(body["transition"]) != `{"id":"31"}` {
				t.Error("transition request included unreviewed fields")
			}
			f.posts.Add(1)
			if f.mode.Load() != 5 {
				f.status.Store(20)
			}
			if f.mode.Load() == 4 || f.mode.Load() == 5 {
				w.WriteHeader(503)
				return
			}
			w.WriteHeader(204)
		default:
			t.Error("unexpected Cloud resource", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(remote.Close)
	u, err := url.Parse(remote.URL)
	if err != nil {
		t.Fatal(err)
	}
	f.client = &http.Client{Transport: fixtureTransport{t: t, remote: u, base: remote.Client().Transport}}
	f.box, err = secrets.NewBox(bytes.Repeat([]byte{7}, secrets.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	f.reopen(t)
	t.Cleanup(func() {
		if err := f.store.Close(); err != nil {
			t.Error(err)
		}
	})
	view, err := f.mcp.Put(t.Context(), mcpconfig.Input{ID: "atlassian", Name: "Cloud fixture", Service: "atlassian", Endpoint: "https://mcp.atlassian.com/v2/mcp", Enabled: true,
		CredentialType: "bearer", Credential: &mcpconfig.Credential{Value: fixtureToken}}, 0, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	f.input = jiracloud.Input{Name: "cloud-team", MCPConnection: view.ID, MCPRevision: view.Revision, CloudID: fixtureCloud,
		SiteURL: "https://api.atlassian.com/ex/jira/" + fixtureCloud, ProjectID: "10001", Enabled: true, ProjectRestricted: true}
	return f
}

func (f *cloudFixture) reopen(t *testing.T) {
	t.Helper()
	store, err := fsstore.Open(f.root)
	if err != nil {
		t.Fatal(err)
	}
	f.store = store
	f.mcp = mcpconfig.New(store, f.box, nil)
	f.api = &mcpconfig.TrackerAPI{Connections: f.mcp, Client: f.client}
	f.profiles = jiracloud.Profiles{Store: store, API: f.api}
}

func (f *cloudFixture) save(t *testing.T) {
	t.Helper()
	var err error
	f.profile, err = f.profiles.Put(t.Context(), f.input, 0, "operator")
	if err != nil {
		t.Fatal(err)
	}
}

func (f *cloudFixture) syncer() jiradc.TransitionSync {
	return jiradc.TransitionSync{Backend: f.profiles.Backend()}
}

func (f *cloudFixture) intent() jiradc.TransitionIntent {
	return jiradc.TransitionIntent{OperationID: "cloud:task-7:pr-delivered", Connection: f.profile.Name, ConnectionRevision: f.profile.Revision,
		IssueKey: "APP-7", IssueID: "10007", IssueTypeID: "10002", FromStatusID: "10", TransitionID: "31", TargetStatusID: "20"}
}

func TestProfilePreviewPersistenceAndOfflineDisableDoNotContactJira(t *testing.T) {
	f := newCloudFixture(t)
	if _, err := f.profiles.Preview(t.Context(), f.input); err != nil {
		t.Fatal(err)
	}
	if rows, err := f.store.ListSettings(t.Context(), jiracloud.ProfilePrefix); err != nil || len(rows) != 0 || f.calls.Load() != 0 {
		t.Fatal("preview wrote a profile or contacted Jira", rows, err)
	}
	f.save(t)
	if f.profile.Revision != 1 || f.calls.Load() != 0 {
		t.Fatal("profile save contacted Jira")
	}
	encoded, err := json.Marshal(f.profile)
	if err != nil || strings.Contains(string(encoded), fixtureToken) || strings.Contains(string(encoded), "sealed") {
		t.Fatal("profile retained credential material")
	}
	before, _ := f.store.ListEvents(t.Context(), storage.EventFilter{Ascending: true})
	if _, err := f.profiles.Put(t.Context(), f.input, 1, "operator"); err != nil {
		t.Fatal(err)
	}
	after, _ := f.store.ListEvents(t.Context(), storage.EventFilter{Ascending: true})
	if len(before) != len(after) {
		t.Fatal("no-op profile save wrote an audit mutation")
	}
	if _, err := f.profiles.Put(t.Context(), f.input, 0, "operator"); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("profile CAS was bypassed", err)
	}
	f.disableMCP(t)
	f.input.Enabled = false
	p, err := f.profiles.Put(t.Context(), f.input, 1, "operator")
	if err != nil || p.Enabled || p.Revision != 2 || f.calls.Load() != 0 {
		t.Fatal("exact disabling needed remote/credential access", p, err)
	}
	rows, err := f.profiles.List(t.Context())
	if err != nil || len(rows) != 1 || rows[0].Revision != 2 {
		t.Fatal(rows, err)
	}
}

func (f *cloudFixture) disableMCP(t *testing.T) {
	t.Helper()
	view, err := f.mcp.Get(t.Context(), f.input.MCPConnection)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.mcp.Put(t.Context(), mcpconfig.Input{ID: view.ID, Name: view.Name, Service: view.Service, Endpoint: view.Endpoint, Enabled: false, CredentialType: view.CredentialType}, view.Revision, "revoking-operator")
	if err != nil {
		t.Fatal(err)
	}
}

func TestCloudTransitionSharedCoreSucceedsAndKeepsEditionNamespaces(t *testing.T) {
	f := newCloudFixture(t)
	f.save(t)
	backend := f.profiles.Backend()
	guards, err := backend.SettingGuards(t.Context(), f.profile.Name, f.profile.Revision)
	if err != nil || len(guards) != 2 || guards[0].Key != jiracloud.ProfilePrefix+f.profile.Name || guards[1].Key != "mcp/connections/atlassian" || guards[1].Revision != f.input.MCPRevision {
		t.Fatal("profile and MCP were not independently guarded", guards, err)
	}
	read, err := f.profiles.Open(t.Context(), f.profile.Name, 1, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sent, err := read.SendTransition(t.Context(), f.intent()); sent || !errors.Is(err, jiracloud.ErrWriteDisabled) || f.posts.Load() != 0 {
		t.Fatal("read client acquired transition authority", sent, err)
	}
	mapping, err := (jiradc.WorkflowMappings{Backend: backend}).Put(t.Context(), jiradc.MappingInput{Name: "review-step", Connection: f.profile.Name, ConnectionRevision: 1,
		SampleIssueKey: "APP-7", SampleIssueID: "10007", IssueTypeID: "10002", FromStatusID: "10", TransitionID: "31", TargetStatusID: "20", IssueUpdated: "2026-11-20T12:00:00.000+0000", Enabled: true}, 0, "operator")
	if err != nil || mapping.TransitionName != "Revisão" || mapping.SiteURL != f.input.SiteURL {
		t.Fatal(mapping, err)
	}
	in := f.intent()
	in.MappingName, in.MappingRevision = mapping.Name, mapping.Revision
	got, err := f.syncer().Apply(t.Context(), in, "operator")
	if err != nil || got.State != "target_observed" || got.ObservedStatusID != "20" || f.posts.Load() != 1 {
		t.Fatal(got, err, f.posts.Load())
	}
	if _, err := f.syncer().Apply(t.Context(), in, "operator"); err != nil || f.posts.Load() != 1 {
		t.Fatal("completed Cloud operation was repeated", err)
	}
	cloud, err := f.store.ListSettings(t.Context(), jiracloud.SyncPrefix)
	if err != nil || len(cloud) != 1 {
		t.Fatal(cloud, err)
	}
	dc, err := f.store.ListSettings(t.Context(), jiradc.SyncPrefix)
	if err != nil || len(dc) != 0 {
		t.Fatal("Cloud journal collided with DC", dc, err)
	}
	audits, err := f.store.ListEvents(t.Context(), storage.EventFilter{Types: []domain.EventType{"tracker.workflow_mapping_change_requested", "tracker.transition_sync_requested"}, Ascending: true})
	if err != nil || len(audits) != 2 {
		t.Fatal("missing scoped Cloud audit evidence", len(audits), err)
	}
	for _, event := range audits {
		var scope struct {
			SiteURL   string `json:"siteUrl"`
			ProjectID string `json:"projectId"`
		}
		if json.Unmarshal(event.Data, &scope) != nil || scope.SiteURL != f.input.SiteURL || scope.ProjectID != f.input.ProjectID || strings.Contains(string(event.Data), fixtureToken) {
			t.Fatal("Cloud audit lost edition/site/project identity", event.Type, scope)
		}
	}
}

func TestCloudScopeRequiredFieldsAndTransportErrorsDoNotBecomeSuccess(t *testing.T) {
	for _, mode := range []int32{1, 2, 3, 6} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			f := newCloudFixture(t)
			f.save(t)
			f.mode.Store(mode)
			got, err := f.syncer().Apply(t.Context(), f.intent(), "operator")
			if err == nil || got.State != "failed" || f.posts.Load() != 0 || strings.Contains(err.Error(), fixtureToken) || strings.Contains(got.Error, "private upstream") {
				t.Fatal("invalid/preflight response became transition success", got.State, err, f.posts.Load())
			}
		})
	}
}

func TestCloudRevocationDuringDiscoveryPreventsTransition(t *testing.T) {
	for _, scope := range []string{"profile", "mcp"} {
		t.Run(scope, func(t *testing.T) {
			f := newCloudFixture(t)
			f.save(t)
			f.beforeMetadata = func() {
				if scope == "mcp" {
					f.disableMCP(t)
					return
				}
				in := f.profile.Input
				in.ProjectRestricted = false
				if _, err := f.profiles.Put(t.Context(), in, 1, "revoking-operator"); err != nil {
					t.Error(err)
				}
			}
			_, err := f.syncer().Apply(t.Context(), f.intent(), "operator")
			if err == nil || f.posts.Load() != 0 {
				t.Fatal("cached Cloud client bypassed revoked authority", err, f.posts.Load())
			}
		})
	}
}

func TestCloudUnknownDispatchSurvivesRestartAndNeverReposts(t *testing.T) {
	for _, mode := range []int32{4, 5} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			f := newCloudFixture(t)
			f.save(t)
			f.mode.Store(mode)
			in := f.intent()
			first, err := f.syncer().Apply(t.Context(), in, "operator")
			if err == nil || first.State != "unknown" || f.posts.Load() != 1 {
				t.Fatal("ambiguous write was not retained", first, err)
			}
			if err := f.store.Close(); err != nil {
				t.Fatal(err)
			}
			f.reopen(t)
			got, err := f.syncer().Apply(t.Context(), in, "operator")
			if mode == 4 && (err != nil || got.State != "target_observed") {
				t.Fatal("lost response was not reconciled by reading", got, err)
			}
			if mode == 5 && (!errors.Is(err, jiradc.ErrSyncUnconfirmed) || got.State != "unknown") {
				t.Fatal("unknown remote outcome became a retry or success", got, err)
			}
			if f.posts.Load() != 1 {
				t.Fatal("unknown write was posted again")
			}
		})
	}
}

func TestCloudProfileRejectsWrongCredentialAudienceAndUnconfirmedWrites(t *testing.T) {
	for _, mode := range []string{"custom", "disabled", "missing"} {
		t.Run(mode, func(t *testing.T) {
			f := newCloudFixture(t)
			switch mode {
			case "custom":
				v, err := f.mcp.Put(t.Context(), mcpconfig.Input{ID: "custom", Name: "Custom MCP", Service: "atlassian", Endpoint: "https://custom.example/mcp", Enabled: true, Credential: &mcpconfig.Credential{Value: fixtureToken}}, 0, "operator")
				if err != nil {
					t.Fatal(err)
				}
				f.input.MCPConnection, f.input.MCPRevision = v.ID, v.Revision
			case "disabled":
				f.disableMCP(t)
				f.input.MCPRevision++
			case "missing":
				f.input.MCPConnection = "missing"
			}
			if _, err := f.profiles.Preview(t.Context(), f.input); !errors.Is(err, jiracloud.ErrInvalid) || f.calls.Load() != 0 {
				t.Fatal("incompatible MCP profile became a server error or contacted Jira", err)
			}
		})
	}
	f := newCloudFixture(t)
	f.input.ProjectRestricted = false
	f.save(t)
	if _, err := f.profiles.Open(t.Context(), f.profile.Name, 1, true, nil); !errors.Is(err, jiracloud.ErrWriteDisabled) || f.calls.Load() != 0 {
		t.Fatal("unconfirmed account obtained write authority", err)
	}
}

type cloudGuardRaceStore struct {
	storage.Store
	prefix string
	change func()
}

func (s *cloudGuardRaceStore) PutSettingIf(ctx context.Context, key string, raw []byte, expected int64, actor string, guards []storage.SettingRevision) (int64, error) {
	if strings.HasPrefix(key, s.prefix) && s.change != nil {
		change := s.change
		s.change = nil
		change()
	}
	return s.Store.PutSettingIf(ctx, key, raw, expected, actor, guards)
}

func TestCloudProfileAndJournalCASGuardMCPRevocation(t *testing.T) {
	for _, stage := range []string{"profile", "journal"} {
		t.Run(stage, func(t *testing.T) {
			f := newCloudFixture(t)
			prefix := jiracloud.ProfilePrefix
			if stage == "journal" {
				f.save(t)
				prefix = jiracloud.SyncPrefix
			}
			f.profiles.Store = &cloudGuardRaceStore{Store: f.store, prefix: prefix, change: func() { f.disableMCP(t) }}
			if stage == "profile" {
				if _, err := f.profiles.Put(t.Context(), f.input, 0, "operator"); !errors.Is(err, storage.ErrConflict) {
					t.Fatal("profile reservation ignored the changed MCP record", err)
				}
			} else {
				if _, err := f.syncer().Apply(t.Context(), f.intent(), "operator"); !errors.Is(err, storage.ErrConflict) {
					t.Fatal("journal reservation ignored the changed MCP record", err)
				}
			}
			rows, err := f.store.ListSettings(t.Context(), prefix)
			if err != nil || len(rows) != 0 || f.posts.Load() != 0 {
				t.Fatal("changed MCP scope persisted authority or dispatched a write", len(rows), err, f.posts.Load())
			}
		})
	}
}

type cloudReadFailure struct {
	storage.Store
	failure error
}

func (s cloudReadFailure) GetSetting(ctx context.Context, key string) (*storage.Setting, error) {
	if strings.HasPrefix(key, "mcp/connections/") {
		return nil, s.failure
	}
	return s.Store.GetSetting(ctx, key)
}

func TestCloudProfileStorageFailurePropagatesWithoutProviderAccess(t *testing.T) {
	f := newCloudFixture(t)
	failure := errors.New("fixture storage unavailable")
	f.profiles.Store = cloudReadFailure{Store: f.store, failure: failure}
	if _, err := f.profiles.Preview(t.Context(), f.input); !errors.Is(err, failure) || f.calls.Load() != 0 {
		t.Fatal("dependency read failure was hidden as configuration success", err)
	}
}

type cloudSecondReadFailure struct {
	storage.Store
	reads   atomic.Int32
	failure error
}

func (s *cloudSecondReadFailure) GetSetting(ctx context.Context, key string) (*storage.Setting, error) {
	if strings.HasPrefix(key, "mcp/connections/") && s.reads.Add(1) == 2 {
		return nil, s.failure
	}
	return s.Store.GetSetting(ctx, key)
}

func TestCloudProfileSecondMCPReadFailureRemainsDependencyFailure(t *testing.T) {
	f := newCloudFixture(t)
	failure := errors.New("fixture second MCP metadata read unavailable")
	store := &cloudSecondReadFailure{Store: f.store, failure: failure}
	f.profiles.Store = store
	f.api.Connections = mcpconfig.New(store, f.box, nil)
	if _, err := f.profiles.Preview(t.Context(), f.input); !errors.Is(err, failure) || errors.Is(err, jiracloud.ErrInvalid) || f.calls.Load() != 0 {
		t.Fatal("second storage failure was classified as invalid user configuration", err)
	}
}
