package gemini

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/woliveiras/weft/server/internal/llm"
)

// Failure kinds callers can test with errors.Is. The *llm.APIError behind
// an HTTP failure stays reachable with errors.As, so retry decisions and
// status checks work as for every other provider.
var (
	ErrQuota         = errors.New("gemini: quota or rate limit exceeded")
	ErrPermission    = errors.New("gemini: permission denied")
	ErrModelNotFound = errors.New("gemini: model not found")
	ErrSafetyBlocked = errors.New("gemini: blocked by safety filters")
)

// Error is an actionable Gemini failure: what happened and what to do.
type Error struct {
	Kind     error
	Provider string
	// Message explains the failure and the fix; Detail is Google's own
	// message, when there is one (already redacted by the HTTP client).
	Message, Detail string
	Err             error
}

func (e *Error) Error() string {
	s := fmt.Sprintf("llm %s: %s", e.Provider, e.Message)
	if e.Detail != "" {
		s += " (Google: " + e.Detail + ")"
	}
	return s
}

func (e *Error) Unwrap() []error {
	out := []error{e.Kind}
	if e.Err != nil {
		out = append(out, e.Err)
	}
	return out
}

// googleError is the error body of Google APIs.
type googleError struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
		Details []struct {
			Reason string `json:"reason"`
		} `json:"details"`
	} `json:"error"`
}

// mapError turns HTTP failures Google reports into actionable errors.
// Others (5xx after retries, transport errors, cancellation) are returned
// unchanged.
func (p *Provider) mapError(err error, model string) error {
	var ae *llm.APIError
	if !errors.As(err, &ae) {
		return err
	}
	var g googleError
	_ = json.Unmarshal([]byte(ae.Body), &g)
	detail := strings.TrimSpace(g.Error.Message)
	reasons := map[string]bool{}
	for _, d := range g.Error.Details {
		reasons[d.Reason] = true
	}
	e := &Error{Provider: p.cfg.Name, Detail: detail, Err: err}
	modelText := "the model"
	if model != "" {
		modelText = fmt.Sprintf("model %q", modelID(model))
	}
	switch {
	case ae.Status == http.StatusTooManyRequests || g.Error.Status == "RESOURCE_EXHAUSTED":
		e.Kind = ErrQuota
		e.Message = "quota or rate limit exceeded; wait and try again, lower the provider's concurrent requests, or raise the quota (Gemini API free-tier keys allow few requests per minute and per day)"
	case reasons["API_KEY_INVALID"] || ae.Status == http.StatusUnauthorized || g.Error.Status == "UNAUTHENTICATED":
		e.Kind = ErrPermission
		if p.vertex == nil {
			e.Message = "the API key was rejected; create a new key in Google AI Studio and store it in the provider"
		} else {
			e.Message = "the server's Application Default Credentials were rejected; refresh them (gcloud auth application-default login) or replace the service account key"
		}
	case ae.Status == http.StatusForbidden || g.Error.Status == "PERMISSION_DENIED":
		e.Kind = ErrPermission
		if p.vertex == nil {
			e.Message = fmt.Sprintf("permission denied for %s; check that the API key's project has the Generative Language API enabled and access to the model", modelText)
		} else {
			e.Message = fmt.Sprintf("permission denied in project %s; enable the Vertex AI API there and grant the server's identity the Vertex AI User role (roles/aiplatform.user)", p.vertex.project)
		}
	case ae.Status == http.StatusNotFound || g.Error.Status == "NOT_FOUND":
		e.Kind = ErrModelNotFound
		if p.vertex == nil {
			e.Message = fmt.Sprintf("%s was not found; check the model ID against the provider's model list", modelText)
		} else {
			e.Message = fmt.Sprintf("%s was not found in region %s; check the model ID, or choose a region that serves it (global serves the newest models)", modelText, p.vertex.region)
		}
	default:
		return err
	}
	return e
}

// blocked reports a prompt or answer withheld by safety filters.
func (p *Provider) blocked(what, reason, message string) error {
	return &Error{
		Kind: ErrSafetyBlocked, Provider: p.cfg.Name, Detail: strings.TrimSpace(message),
		Message: fmt.Sprintf("%s was blocked by Gemini's safety filters (reason %s); rephrase the task, or adjust the content that triggers the filter", what, reason),
	}
}
