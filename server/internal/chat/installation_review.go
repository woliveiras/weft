package chat

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/installation"
)

const PlanInstallation = "installation.plan"
const ReviewApplied = "applied"
const ReviewIncomplete = "incomplete"

var ErrInstallationHandoff = errors.New("use installation configuration or the connection setup form for this document")

// ConfigReview contains the exact proposed documents and the server's dry-run
// response. It is private to the administrator who requested the operation.
type ConfigReview struct {
	Document string                    `json:"document"`
	Objects  []installation.ObjectPlan `json:"objects"`
	Consent  bool                      `json:"intakeConsent,omitempty"`
}

type ConfigReceipt struct {
	Key      string `json:"key"`
	Result   string `json:"result"`
	Revision string `json:"revision,omitempty"`
}

// ParseChatConfiguration accepts the ordinary installation contract with a
// smaller review envelope. Installing credentials uses the existing setup
// surface. A model cannot select an environment variable or secret file.
func ParseChatConfiguration(raw string) ([]installation.Document, error) {
	if len(raw) == 0 || !safeText(raw, MaxRequestBytes) {
		return nil, ErrInstallationHandoff
	}
	docs, err := installation.Parse([]string{raw})
	if err != nil || len(docs) > 8 {
		return nil, ErrInstallationHandoff
	}
	for _, doc := range docs {
		if !safeConfigTree(doc.Spec) || !safeConfigTree(doc.Metadata) || !safeConfigTree(doc.Extensions) || !safeText(doc.Name(), 200) {
			return nil, ErrInstallationHandoff
		}
	}
	return docs, nil
}

func safeConfigTree(value any) bool {
	switch x := value.(type) {
	case []any:
		for _, item := range x {
			if !safeConfigTree(item) {
				return false
			}
		}
	case map[string]any:
		for key, item := range x {
			lower := strings.ToLower(strings.ReplaceAll(key, "_", ""))
			switch {
			case strings.HasSuffix(lower, "secretref") || strings.HasSuffix(lower, "usernameref"):
				ref, ok := item.(map[string]any)
				if !ok || len(ref) != 1 || ref["preserve"] != true {
					return false
				}
			case lower == "processenvironmentrefs":
				refs, ok := item.(map[string]any)
				if !ok {
					return false
				}
				for _, value := range refs {
					ref, ok := value.(map[string]any)
					if !ok || len(ref) != 1 || ref["preserve"] != true {
						return false
					}
				}
			case lower == "credential" || lower == "authentication" || lower == "authorizationheader" ||
				lower == "processenvironment" || lower == "env" || lower == "environmentvariables" ||
				strings.HasSuffix(lower, "password") || strings.HasSuffix(lower, "token") ||
				strings.HasSuffix(lower, "secret") || strings.HasSuffix(lower, "apikey"):
				return false
			default:
				if !safeConfigTree(item) {
					return false
				}
			}
		}
	case string:
		return safeText(x, MaxRequestBytes)
	}
	return true
}

func NewConfigReview(owner, session string, config ConfigReview, now time.Time) (*OperationReview, error) {
	if len(config.Objects) == 0 || len(config.Objects) > 8 {
		return nil, ErrInstallationHandoff
	}
	for _, obj := range config.Objects {
		for _, change := range obj.Changes {
			// A removed field may exist only in the prior installation state.
			for _, field := range strings.Split(change.Path, ".") {
				lower := strings.ToLower(field)
				if lower == "env" || lower == "headers" || lower == "processenvironment" ||
					strings.HasSuffix(lower, "password") || strings.HasSuffix(lower, "token") || strings.HasSuffix(lower, "apikey") || strings.HasSuffix(lower, "secret") {
					return nil, ErrInstallationHandoff
				}
			}
			if !safeConfigTree(change.Before) || !safeConfigTree(change.After) {
				return nil, ErrInstallationHandoff
			}
		}
	}
	preview, err := json.Marshal(config.Objects)
	if err != nil || !safeText(string(preview), 16<<10) {
		return nil, ErrInstallationHandoff
	}
	return newReview(owner, session, now, OperationReview{Configuration: &config})
}
