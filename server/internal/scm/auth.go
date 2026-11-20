package scm

import (
	"encoding/json"
	"net/mail"
	"strings"
	"unicode"
)

const (
	AuthBearer   = "bearer"
	AuthAPIToken = "api-token"
)

func effectiveAuthType(value string) string {
	if value == "" {
		return AuthBearer
	}
	return value
}

func validAPIToken(value string) bool {
	return strings.TrimSpace(value) != "" && !strings.ContainsFunc(value, unicode.IsControl)
}

func containsCredentialMaterial(c Connection, token, value string) bool {
	if strings.Contains(value, token) {
		return true
	}
	if c.AuthType != AuthAPIToken {
		return false
	}
	return value != redactAuthMaterial(value, token, BasicAuth(c.Username, token)) ||
		value != redactAuthMaterial(value, token, BasicAuth("x-bitbucket-api-token-auth", token))
}

func validateAuth(kind, authType, username string) []Issue {
	var issues []Issue
	switch effectiveAuthType(authType) {
	case AuthBearer:
		if username != "" {
			issues = append(issues, Issue{"username", "omit the username for Bearer authentication"})
		}
	case AuthAPIToken:
		if kind != "bitbucket-cloud" {
			issues = append(issues, Issue{"authType", "API-token authentication is available only for Bitbucket Cloud"})
		}
		address, err := mail.ParseAddress(username)
		if err != nil || address.Address != username || strings.ContainsAny(username, ":\r\n\t") || len(username) > 254 {
			issues = append(issues, Issue{"username", "enter the Atlassian account email address without a display name"})
		}
	default:
		issues = append(issues, Issue{"authType", "select bearer or api-token"})
	}
	return issues
}

// Legacy Bearer destinations retain their historical update contract. API-token
// credentials are bound to the explicitly selected account and destination.
func credentialScopeChanged(old Connection, in Input) bool {
	if effectiveAuthType(old.AuthType) != effectiveAuthType(in.AuthType) || old.Username != in.Username {
		return true
	}
	if effectiveAuthType(old.AuthType) != AuthAPIToken && effectiveAuthType(in.AuthType) != AuthAPIToken {
		return false
	}
	return old.Kind != in.Kind || old.Repository != in.Repository || old.APIURL != in.APIURL || old.GitURL != in.GitURL
}

// Redact both the raw token and the independently reusable Basic credential.
// JSON-escaped forms are included before decoding untrusted provider responses.
func redactAuthMaterial(value, token, authorization string) string {
	for _, secret := range []string{authorization, strings.TrimPrefix(authorization, "Basic "), token} {
		if secret == "" {
			continue
		}
		encoded, _ := json.Marshal(secret)
		value = strings.ReplaceAll(value, string(encoded[1:len(encoded)-1]), "[redacted]")
		value = strings.ReplaceAll(value, secret, "[redacted]")
		value = redactPercentEncoded(value, secret)
	}
	return value
}

// Providers can reflect credentials into URL components rather than plain text.
// Match their decoded bytes while retaining the original representation of all
// unrelated content. Percent escapes accept either hex case; a malformed escape
// elsewhere cannot bypass redaction. '+' is kept literal: Basic payloads contain
// no spaces, and a literal '+' in a query is itself encoded as %2B.
func redactPercentEncoded(value, secret string) string {
	if !strings.Contains(value, "%") {
		return value
	}
	decoded := make([]byte, 0, len(value))
	starts := make([]int, 0, len(value)+1)
	for i := 0; i < len(value); {
		starts = append(starts, i)
		if value[i] == '%' && i+2 < len(value) {
			hi, lo := hexNibble(value[i+1]), hexNibble(value[i+2])
			if hi >= 0 && lo >= 0 {
				decoded = append(decoded, byte(hi<<4|lo))
				i += 3
				continue
			}
		}
		decoded = append(decoded, value[i])
		i++
	}
	starts = append(starts, len(value))
	text := string(decoded)
	var out strings.Builder
	from, copied := 0, 0
	for {
		at := strings.Index(text[from:], secret)
		if at < 0 {
			break
		}
		at += from
		out.WriteString(value[copied:starts[at]])
		out.WriteString("[redacted]")
		from = at + len(secret)
		copied = starts[from]
	}
	if copied == 0 {
		return value
	}
	out.WriteString(value[copied:])
	return out.String()
}

func hexNibble(value byte) int {
	switch {
	case value >= '0' && value <= '9':
		return int(value - '0')
	case value >= 'a' && value <= 'f':
		return int(value-'a') + 10
	case value >= 'A' && value <= 'F':
		return int(value-'A') + 10
	default:
		return -1
	}
}

func redactJSONStrings(value any, redact func(string) string) any {
	switch v := value.(type) {
	case string:
		return redact(v)
	case []any:
		for i := range v {
			v[i] = redactJSONStrings(v[i], redact)
		}
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			out[redact(key)] = redactJSONStrings(child, redact)
		}
		return out
	}
	return value
}
