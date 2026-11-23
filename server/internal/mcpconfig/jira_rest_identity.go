package mcpconfig

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Only these Jira response positions describe identities. An email in issue
// text, an arbitrary emailAddress property or another endpoint is not exempt.
func jiraRESTIdentityPaths(method, requestPath string) [][]string {
	u, err := url.ParseRequestURI(requestPath)
	if err != nil {
		return nil
	}
	_, route, ok := strings.Cut(u.Path, "/rest/api/3/")
	if !ok {
		return nil
	}
	parts := strings.Split(route, "/")
	if method == http.MethodGet && route == "myself" {
		return [][]string{{}}
	}
	if len(parts) < 2 || parts[0] != "issue" || parts[1] == "" {
		return nil
	}
	actors := []string{"author", "updateAuthor"}
	if method == http.MethodGet && len(parts) == 2 {
		paths := [][]string{{"fields", "reporter"}, {"fields", "assignee"}, {"fields", "creator"}}
		for _, actor := range actors {
			paths = append(paths, []string{"fields", "comment", "comments", "*", actor})
		}
		return paths
	}
	if len(parts) >= 3 && parts[2] == "comment" {
		if method == http.MethodGet && len(parts) == 3 {
			return [][]string{{"comments", "*", "author"}, {"comments", "*", "updateAuthor"}}
		}
		if method == http.MethodPost && len(parts) == 3 || method == http.MethodGet && len(parts) == 4 && parts[3] != "" {
			return [][]string{{"author"}, {"updateAuthor"}}
		}
	}
	return nil
}

// Redaction precedes the unchanged credential-reflection guard, but reusable
// secrets are checked first. Duplicate keys cannot hide a secret or an email in
// a discarded value. Number tokens preserve opaque numeric provider metadata.
func redactJiraRESTIdentityEmail(raw json.RawMessage, token, method, requestPath string) (json.RawMessage, error) {
	paths := jiraRESTIdentityPaths(method, requestPath)
	if len(paths) == 0 || len(raw) == 0 {
		return raw, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return nil, errors.New("invalid Jira REST credential")
	}
	email, password, ok := strings.Cut(string(decoded), ":")
	if !ok || email == "" || password == "" {
		return nil, errors.New("invalid Jira REST credential")
	}
	secret := func(value string) bool {
		var unescaped strings.Builder
		for i := 0; i < len(value); i++ {
			if value[i] == '%' && i+2 < len(value) {
				if b, err := strconv.ParseUint(value[i+1:i+3], 16, 8); err == nil {
					unescaped.WriteByte(byte(b))
					i += 2
					continue
				}
			}
			unescaped.WriteByte(value[i])
		}
		for _, part := range []string{token, password, string(decoded)} {
			if strings.Contains(value, part) || strings.Contains(unescaped.String(), part) {
				return true
			}
		}
		return false
	}
	if secret(string(raw)) {
		return nil, errors.New("Jira REST credential reflected")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := validateJiraRESTIdentityJSON(decoder, secret, 0); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errors.New("invalid Jira REST identity response")
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, errors.New("invalid Jira REST identity response")
	}
	changed := false
	for _, path := range paths {
		changed = redactJiraRESTIdentityPath(value, path, email) || changed
	}
	if !changed {
		return raw, nil
	}
	out, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("Jira REST identity response could not be redacted")
	}
	return out, nil
}

func validateJiraRESTIdentityJSON(decoder *json.Decoder, secret func(string) bool, depth int) error {
	if depth > 128 {
		return errors.New("Jira REST identity response exceeds nesting limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return errors.New("invalid Jira REST identity response")
	}
	if text, ok := token.(string); ok && secret(text) {
		return errors.New("Jira REST credential reflected")
	}
	if delim, ok := token.(json.Delim); ok {
		if delim != '{' && delim != '[' {
			return errors.New("invalid Jira REST identity response")
		}
		seen := map[string]bool{}
		for decoder.More() {
			if delim == '{' {
				key, err := decoder.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] || secret(name) {
					return errors.New("invalid Jira REST identity response")
				}
				seen[name] = true
			}
			if err := validateJiraRESTIdentityJSON(decoder, secret, depth+1); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return errors.New("invalid Jira REST identity response")
		}
	}
	return nil
}

func redactJiraRESTIdentityPath(value any, path []string, email string) bool {
	if len(path) == 0 {
		if object, ok := value.(map[string]any); ok && object["emailAddress"] == email {
			delete(object, "emailAddress")
			return true
		}
		return false
	}
	if path[0] == "*" {
		changed := false
		if entries, ok := value.([]any); ok {
			for _, entry := range entries {
				changed = redactJiraRESTIdentityPath(entry, path[1:], email) || changed
			}
		}
		return changed
	}
	if object, ok := value.(map[string]any); ok {
		return redactJiraRESTIdentityPath(object[path[0]], path[1:], email)
	}
	return false
}
