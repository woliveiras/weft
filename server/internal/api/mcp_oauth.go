package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/woliveiras/weft/server/internal/storage"
)

const oauthCookiePath = "/api/v1/mcp/oauth"

func oauthActor(r *http.Request) string {
	sum := sha256.Sum256([]byte(r.Header.Get("Authorization")))
	return identityFrom(r.Context()).Name + ":" + hex.EncodeToString(sum[:])
}
func (s *Server) oauthBase() (string, error) {
	u, e := url.Parse(s.Config.Server.PublicURL)
	if e != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.Scheme != "https" && (u.Scheme != "http" || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1")) {
		return "", badRequest("Configure server.publicUrl (WEFT_PUBLIC_URL) as an HTTPS origin or HTTP loopback origin")
	}
	return strings.TrimRight(u.String(), "/"), nil
}
func (s *Server) mcpOAuthStart(w http.ResponseWriter, r *http.Request) error {
	if s.MCP == nil {
		return storage.ErrNotFound
	}
	base, err := s.oauthBase()
	if err != nil {
		return err
	}
	result, err := s.MCP.StartOAuthReviewed(r.Context(), r.PathValue("connection"), oauthActor(r), base+oauthCookiePath+"/callback", r.URL.Query().Get("registrationMode"))
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: "lf_mcp_" + result.State, Value: result.Binding, Path: oauthCookiePath, HttpOnly: true, Secure: strings.HasPrefix(base, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: 600})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, result)
	return nil
}
func oauthBinding(r *http.Request, state string) string {
	if len(state) != 43 {
		return ""
	}
	c, e := r.Cookie("lf_mcp_" + state)
	if e != nil {
		return ""
	}
	return c.Value
}
func (s *Server) mcpOAuthCallback(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if s.MCP == nil {
		return storage.ErrNotFound
	}
	base, err := s.oauthBase()
	if err != nil {
		return err
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	valid := err == nil && len(r.URL.RawQuery) <= 16384
	for key, values := range q {
		if len(values) != 1 || !strings.Contains("|state|code|iss|error|error_description|error_uri|", "|"+key+"|") {
			valid = false
		}
	}
	state := q.Get("state")
	if valid {
		err = s.MCP.CaptureOAuth(state, oauthBinding(r, state), q.Get("code"), q.Get("iss"), q.Get("error"))
	} else {
		err = fmt.Errorf("invalid OAuth return")
	}
	target := base + "/#/integrations/oauth?state=" + url.QueryEscape(state)
	if err != nil {
		target = base + "/#/integrations/oauth?error=invalid_return"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
	return nil
}
func (s *Server) mcpOAuthComplete(w http.ResponseWriter, r *http.Request) error {
	if s.MCP == nil {
		return storage.ErrNotFound
	}
	var in struct {
		State string `json:"state"`
	}
	d := json.NewDecoder(io.LimitReader(r.Body, 1025))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF || len(in.State) != 43 {
		return badRequest("Invalid OAuth return")
	}
	v, err := s.MCP.CompleteOAuth(r.Context(), in.State, oauthBinding(r, in.State), oauthActor(r))
	if err != nil {
		return err
	}
	base, _ := s.oauthBase()
	http.SetCookie(w, &http.Cookie{Name: "lf_mcp_" + in.State, Value: "", Path: oauthCookiePath, HttpOnly: true, Secure: strings.HasPrefix(base, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: -1})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, v)
	return nil
}
func (s *Server) mcpOAuthRevoke(w http.ResponseWriter, r *http.Request) error {
	if s.MCP == nil {
		return storage.ErrNotFound
	}
	v, err := s.MCP.RevokeOAuth(r.Context(), r.PathValue("connection"), identityFrom(r.Context()).Name)
	if err != nil {
		return err
	}
	writeJSON(w, 200, v)
	return nil
}

func (s *Server) mcpOAuthConfig(w http.ResponseWriter, r *http.Request) error {
	base, err := s.oauthBase()
	if err != nil {
		return err
	}
	writeJSON(w, 200, map[string]string{"callbackUrl": base + oauthCookiePath + "/callback"})
	return nil
}

func (s *Server) mcpOAuthPreview(w http.ResponseWriter, r *http.Request) error {
	if s.MCP == nil {
		return storage.ErrNotFound
	}
	base, err := s.oauthBase()
	if err != nil {
		return err
	}
	plan, err := s.MCP.PreviewOAuth(r.Context(), r.PathValue("connection"), base+oauthCookiePath+"/callback")
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, plan)
	return nil
}
func (s *Server) mcpOAuthClientDocument(w http.ResponseWriter, r *http.Request) error {
	if s.MCP == nil {
		return storage.ErrNotFound
	}
	base, err := s.oauthBase()
	if err != nil {
		return err
	}
	doc, err := s.MCP.ClientDocument(r.Context(), r.PathValue("connection"), base+oauthCookiePath+"/callback")
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, doc)
	return nil
}
