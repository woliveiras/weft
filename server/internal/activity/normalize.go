package activity

import (
	"regexp"
	"strings"
)

// maxNormalizedRunes bounds a normalized error message.
const maxNormalizedRunes = 200

// NoErrorMessage groups failures that recorded no error text.
const NoErrorMessage = "(no error message)"

// Rules applied in order. Secrets go first so that no later rule can keep a
// fragment of one; long quoted text goes before ids and paths because it is
// most likely a payload echoed back.
var normalizeRules = []struct {
	re   *regexp.Regexp
	with string
	// fn, when set, replaces each match instead of with.
	fn func(string) string
}{
	// Credentials: auth schemes, key=value pairs and well-known token shapes.
	{re: regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{6,}`), with: "$1 <redacted>"},
	{re: regexp.MustCompile(`(?i)\btoken\s+[A-Za-z0-9._~+/=-]{8,}`), fn: tokenValue},
	{re: regexp.MustCompile(`(?i)\b([a-z_-]*(?:api[_-]?key|secret|password|passwd|token|authorization|credential|access[_-]?key)[a-z_-]*)("?\s*[:=]\s*"?)(?:(?:bearer|basic|token)\s+)?[^\s",;&)]+`), with: "$1$2<redacted>"},
	{re: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`), with: "<redacted>"},
	{re: regexp.MustCompile(`\b(?:sk|pk|rk)-[A-Za-z0-9_-]{8,}`), with: "<redacted>"},
	{re: regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr|github_pat|glpat|xox[abprs])[_-][A-Za-z0-9_-]{8,}`), with: "<redacted>"},
	{re: regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`), with: "<redacted>"},
	// Long quoted text: likely an echoed payload or prompt.
	{re: regexp.MustCompile("\"[^\"]{40,}\"|'[^']{40,}'|`[^`]{40,}`"), with: "<text>"},
	// Locations and identities.
	{re: regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s"'<>]*[^\s"'<>.,:;)]`), with: "<url>"},
	{re: regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`), with: "<email>"},
	{re: regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`), with: "<id>"},
	{re: regexp.MustCompile(`\b[A-Za-z]:\\[^\s"']+`), with: "<path>"},
	{re: regexp.MustCompile(`(^|[\s"'(=:\[,])(?:~|\.{1,2})?/[^\s"'():,\]]+`), with: "$1<path>"},
	{re: regexp.MustCompile(`\b[\w-]+(?:/[\w.-]+)*/[\w-]+\.[A-Za-z][A-Za-z0-9]{0,7}(?::\d+)*\b`), with: "<path>"},
	{re: regexp.MustCompile(`(?i)\b[0-9a-f]{8,}\b`), with: "<id>"},
	// A word of 8+ characters mixing letters and digits: an execution, run,
	// task or container id.
	{re: regexp.MustCompile(`\b[A-Za-z0-9_-]*[0-9][A-Za-z0-9_-]*\b`), fn: idWord},
	{re: regexp.MustCompile(`\b\d+(?:\.\d+)?(?:ns|µs|us|ms|s|m|h|b|kb|mb|gb|KB|MB|GB|KiB|MiB|GiB)?\b`), with: "<n>"},
	{re: regexp.MustCompile(`\s+`), with: " "},
}

// NormalizeError turns an execution error into a stable group key that is
// safe to show: only the first line is kept, credentials are redacted, and
// ids, paths, URLs, numbers and long quoted text become placeholders, so one
// failure does not split into a group per run.
func NormalizeError(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	s = strings.TrimSpace(s)
	if s == "" {
		return NoErrorMessage
	}
	for _, r := range normalizeRules {
		if r.fn != nil {
			s = r.re.ReplaceAllStringFunc(s, r.fn)
			continue
		}
		s = r.re.ReplaceAllString(s, r.with)
	}
	return truncate(s, maxNormalizedRunes)
}

// tokenValue redacts "token <value>" when the value looks like a credential
// (it has a digit), so prose such as "token budget exhausted" stays.
func tokenValue(m string) string {
	i := strings.IndexAny(m, " \t\v\f\r")
	if i < 0 || !strings.ContainsAny(m[i:], "0123456789") {
		return m
	}
	return m[:i] + " <redacted>"
}

// idWord replaces words that look like generated ids (8+ characters with
// both letters and digits) and keeps short ones such as "gpt4" or "http2".
func idWord(w string) string {
	if len(w) < 8 || !strings.ContainsAny(strings.ToLower(w), "abcdefghijklmnopqrstuvwxyz") {
		return w
	}
	return "<id>"
}
