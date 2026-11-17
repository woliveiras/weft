package mcpconfig

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/mail"
	"strings"
)

// Only the manager can select the wire scheme; remote metadata and agent arguments
// cannot reinterpret a bearer token as another authorization header.
type credentialSchemeKey struct{}

func credentialContext(ctx context.Context, v View) context.Context {
	ctx = context.WithValue(ctx, transportProfileKey{}, v.TransportProfile)
	ctx = context.WithValue(ctx, metadataHeadersKey{}, v.MetadataHeaders)
	return context.WithValue(ctx, credentialSchemeKey{}, v.CredentialType)
}
func authorizationHeader(ctx context.Context, token string) string {
	if ctx.Value(credentialSchemeKey{}) == "basic" {
		return "Basic " + token
	}
	return "Bearer " + token
}
func containsTransportCredential(ctx context.Context, raw json.RawMessage, token string) bool {
	if containsCredential(raw, token) {
		return true
	}
	if ctx.Value(credentialSchemeKey{}) != "basic" {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return true
	}
	email, password, ok := strings.Cut(string(decoded), ":")
	return !ok || containsCredential(raw, email) || containsCredential(raw, password) || containsCredential(raw, string(decoded))
}
func validateCredentialMode(in Input) error {
	if in.CredentialType != "" && in.CredentialType != "bearer" && in.CredentialType != "basic" && in.CredentialType != "langsmith" {
		return fmt.Errorf("%w: unsupported credential type", ErrInvalid)
	}
	if in.CredentialType == "langsmith" && (in.Service != "generic" || in.OAuth != nil) {
		return fmt.Errorf("%w: standalone LangSmith API keys require a custom connection without OAuth", ErrInvalid)
	}
	if in.CredentialType == "basic" {
		if in.OAuth != nil || !atlassianConnection(View{Service: in.Service, Endpoint: in.Endpoint}) {
			return fmt.Errorf("%w: Basic credentials require an Atlassian MCP connection without OAuth", ErrInvalid)
		}
	}
	if c := in.Credential; c != nil {
		if c.Username != "" && (in.CredentialType != "basic" || c.Clear || c.Value == "") {
			return fmt.Errorf("%w: email requires a replacement Basic token", ErrInvalid)
		}
		if in.CredentialType == "basic" && c.Value != "" {
			address, err := mail.ParseAddress(c.Username)
			if err != nil || address.Address != c.Username || len(c.Username) > 320 || strings.ContainsAny(c.Username, ":\r\n") {
				return fmt.Errorf("%w: valid Basic credential email required", ErrInvalid)
			}
		}
	}
	return nil
}

func credentialKind(kind string) string {
	if kind == "" {
		return "bearer"
	}
	return kind
}
