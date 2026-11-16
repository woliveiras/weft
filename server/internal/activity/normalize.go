package activity

import "github.com/woliveiras/weft/server/internal/secrets"

const maxNormalizedRunes = 200
const NoErrorMessage = secrets.NoErrorMessage

// NormalizeError retains the shared bounded diagnostic normalization.
func NormalizeError(s string) string { return secrets.DiagnosticSummary(s) }
