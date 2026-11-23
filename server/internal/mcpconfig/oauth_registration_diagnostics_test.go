package mcpconfig

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRegistrationResponseFields(t *testing.T) {
	valid := `{"client_id":"client","client_secret":"fixture-secret","token_endpoint_auth_method":"client_secret_basic","redirect_uris":["` + registrationCallback + `"]}`
	for _, tc := range []struct {
		name, body, method string
		fields             []string
	}{
		{"valid", valid, "client_secret_basic", nil},
		{"public valid", `{"client_id":"client","token_endpoint_auth_method":"none","redirect_uris":["` + registrationCallback + `"]}`, "none", nil},
		{"malformed", `{"client_id":"fixture-secret"`, "none", []string{"response"}},
		{"non object", `[]`, "none", []string{"response"}},
		{"null response", `null`, "none", []string{"response"}},
		{"missing id", strings.Replace(valid, `"client_id":"client",`, "", 1), "client_secret_basic", []string{"client_id"}},
		{"id type", strings.Replace(valid, `"client_id":"client"`, `"client_id":{"fixture-secret":true}`, 1), "client_secret_basic", []string{"client_id"}},
		{"id length", strings.Replace(valid, `"client_id":"client"`, `"client_id":"`+strings.Repeat("x", 257)+`"`, 1), "client_secret_basic", []string{"client_id"}},
		{"secret length", strings.Replace(valid, `"fixture-secret"`, `"`+strings.Repeat("x", 16385)+`"`, 1), "client_secret_basic", []string{"client_secret"}},
		{"missing secret", strings.Replace(valid, `"client_secret":"fixture-secret",`, "", 1), "client_secret_basic", []string{"client_secret"}},
		{"secret type", strings.Replace(valid, `"client_secret":"fixture-secret"`, `"client_secret":9`, 1), "client_secret_basic", []string{"client_secret"}},
		{"missing method", strings.Replace(valid, `"token_endpoint_auth_method":"client_secret_basic",`, "", 1), "client_secret_basic", []string{"token_endpoint_auth_method"}},
		{"method mismatch", strings.Replace(valid, `"token_endpoint_auth_method":"client_secret_basic"`, `"token_endpoint_auth_method":"none"`, 1), "client_secret_basic", []string{"token_endpoint_auth_method"}},
		{"redirect mismatch", strings.Replace(valid, registrationCallback, "https://fixture-secret.example", 1), "client_secret_basic", []string{"redirect_uris"}},
		{"redirect type", strings.Replace(valid, `["`+registrationCallback+`"]`, `"fixture-secret"`, 1), "client_secret_basic", []string{"redirect_uris"}},
		{"expires", strings.Replace(valid, `}`, `,"client_secret_expires_at":99}`, 1), "client_secret_basic", []string{"client_secret_expires_at"}},
		{"expiry type", strings.Replace(valid, `}`, `,"client_secret_expires_at":"fixture-secret"}`, 1), "client_secret_basic", []string{"client_secret_expires_at"}},
		{"scope type", strings.Replace(valid, `}`, `,"scope":{"fixture-secret":true}}`, 1), "client_secret_basic", []string{"scope"}},
		{"public secret", strings.Replace(valid, `"client_secret_basic"`, `"none"`, 1), "none", []string{"client_secret"}},
		{"multiple", `{"client_id":false,"client_secret":12,"token_endpoint_auth_method":"fixture-secret","redirect_uris":[],"scope":false,"client_secret_expires_at":10}`, "none", []string{"client_id", "client_secret", "token_endpoint_auth_method", "redirect_uris", "scope", "client_secret_expires_at"}},
		{"unknown fields ignored", strings.Replace(valid, `}`, `,"fixture-secret":"ignored"}`, 1), "client_secret_basic", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, fields := decodeRegistrationResponse([]byte(tc.body), tc.method, registrationCallback)
			if !reflect.DeepEqual(fields, tc.fields) {
				t.Fatalf("fields = %v, want %v", fields, tc.fields)
			}
		})
	}
	for _, field := range []string{"client_id", "client_secret", "token_endpoint_auth_method", "redirect_uris", "scope", "client_secret_expires_at"} {
		t.Run("null "+field, func(t *testing.T) {
			var body map[string]json.RawMessage
			if err := json.Unmarshal([]byte(valid), &body); err != nil {
				t.Fatal(err)
			}
			body[field] = json.RawMessage(`null`)
			data, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			_, fields := decodeRegistrationResponse(data, "client_secret_basic", registrationCallback)
			if !reflect.DeepEqual(fields, []string{field}) {
				t.Fatalf("fields = %v, want %s", fields, field)
			}
		})
	}
}

func TestRegistrationDiagnosticsPersistThroughTLSAndRestart(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		fields         []string
	}{
		{"incompatible", `{"client_id":"fixture-client-must-not-leak","client_secret":"fixture-secret-must-not-leak","token_endpoint_auth_method":"fixture-method-must-not-leak","redirect_uris":["https://fixture-callback-must-not-leak.example"],"client_secret_expires_at":3}`, []string{"client_secret", "token_endpoint_auth_method", "redirect_uris", "client_secret_expires_at"}},
		{"malformed", `{"fixture-secret-must-not-leak":`, []string{"response"}},
		{"typed", `{"client_id":42,"token_endpoint_auth_method":"none","redirect_uris":false}`, []string{"client_id", "redirect_uris"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testManager(t)
			var registrations atomic.Int32
			var provider *httptest.Server
			provider = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/mcp":
					w.WriteHeader(http.StatusUnauthorized)
				case "/.well-known/oauth-protected-resource/mcp":
					json.NewEncoder(w).Encode(map[string]any{"resource": provider.URL + "/mcp", "authorization_servers": []string{provider.URL}})
				case "/.well-known/oauth-authorization-server":
					json.NewEncoder(w).Encode(map[string]any{"issuer": provider.URL, "authorization_endpoint": provider.URL + "/authorize", "token_endpoint": provider.URL + "/token", "registration_endpoint": provider.URL + "/register", "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"}})
				case "/register":
					if r.Method != http.MethodPost {
						t.Error("unexpected registration method")
					}
					registrations.Add(1)
					w.WriteHeader(http.StatusCreated)
					w.Write([]byte(tc.response))
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer provider.Close()
			m.OAuthClient = provider.Client()
			in := input()
			in.Service = "generic"
			in.Endpoint = provider.URL + "/mcp"
			in.OAuth = &OAuthConfig{Issuer: provider.URL, RegistrationMode: "dcr", Scopes: []string{"read"}}
			if _, err := m.Put(t.Context(), in, 0, "admin"); err != nil {
				t.Fatal(err)
			}
			if _, err := m.StartOAuthReviewed(t.Context(), in.ID, "admin", registrationCallback, "dcr"); err == nil || strings.Contains(err.Error(), "must-not-leak") {
				t.Fatalf("unsafe or absent registration error: %v", err)
			}
			restarted := New(m.Store, m.Box, nil)
			restarted.OAuthClient = provider.Client()
			v, err := restarted.Get(t.Context(), in.ID)
			if err != nil {
				t.Fatal(err)
			}
			diagnostic := v.OAuth.RegistrationDiagnostic
			if v.OAuth.RegistrationStatus != "uncertain" || diagnostic == nil || diagnostic.Code != "response_incompatible" || diagnostic.HTTPStatus != 201 {
				t.Fatal("registration diagnosis not persisted")
			}
			data, err := json.Marshal(diagnostic)
			if err != nil {
				t.Fatal(err)
			}
			var public struct {
				Fields []string `json:"fields"`
			}
			if err = json.Unmarshal(data, &public); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(public.Fields, tc.fields) {
				t.Fatalf("persisted fields = %v, want %v", public.Fields, tc.fields)
			}
			data, err = json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "must-not-leak") {
				t.Fatal("upstream value leaked in public view")
			}
			setting, err := m.Store.GetSetting(t.Context(), prefix+in.ID)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(setting.Value), "must-not-leak") {
				t.Fatal("rejected provider values persisted")
			}
			if _, err = restarted.StartOAuthReviewed(t.Context(), in.ID, "admin", registrationCallback, "dcr"); err == nil {
				t.Fatal("uncertain registration replay accepted")
			}
			if registrations.Load() != 1 {
				t.Fatal("duplicate registration")
			}
		})
	}
}
