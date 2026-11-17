package mcpconfig

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
)

var numericHeaderID = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
var workspaceHeaderID = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)

type metadataHeadersKey struct{}

func validateMetadataHeaders(in Input) error {
	if len(in.MetadataHeaders) > 5 {
		return fmt.Errorf("%w: too many metadata headers", ErrInvalid)
	}
	for name, value := range in.MetadataHeaders {
		valid := false
		switch name {
		case "x-dbt-prod-environment-id", "x-dbt-dev-environment-id", "x-dbt-user-id":
			valid = in.Service == "generic" && in.CredentialType != "basic" && in.CredentialType != "langsmith" && numericHeaderID.MatchString(value)
		case "langsmith-workspace-id":
			valid = in.CredentialType == "langsmith" && workspaceHeaderID.MatchString(value)
		case "langsmith-endpoint":
			valid = in.CredentialType == "langsmith" && (value == "https://api.smith.langchain.com" || value == "https://eu.api.smith.langchain.com")
		}
		if !valid {
			return fmt.Errorf("%w: unsupported metadata header or invalid bounded value", ErrInvalid)
		}
	}
	return nil
}
func applyTransportHeaders(ctx context.Context, req *http.Request, token string) {
	if token != "" {
		if ctx.Value(credentialSchemeKey{}) == "langsmith" {
			req.Header.Set("Langsmith-Api-Key", token)
		} else {
			req.Header.Set("Authorization", authorizationHeader(ctx, token))
		}
	}
	headers, _ := ctx.Value(metadataHeadersKey{}).(map[string]string)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
}
