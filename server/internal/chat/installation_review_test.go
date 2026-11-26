package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/installation"
)

func TestInstallationReviewRejectsCredentialsAndBindsDocumentAndConsent(t *testing.T) {
	const head = "apiVersion: weft.io/v1\nkind: MCPConnection\nmetadata: {name: example}\nspec:\n"
	for _, body := range []string{"  secretRef: {env: WEFT_SECRET_FIXTURE}", "  secretRef: {file: fixture}",
		"  processEnvironmentRefs: {AUTH: {file: fixture}}", "  nested: {token: synthetic}", "  credential: {value: synthetic}",
		"  authentication: {method: bearer}", "  processEnvironmentRefs: [invalid]"} {
		if _, err := ParseChatConfiguration(head + body); err == nil {
			t.Errorf("unsafe or invalid config accepted: %s", body)
		}
	}
	raw := head + "  secretRef: {preserve: true}"
	if _, err := ParseChatConfiguration(raw); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseChatConfiguration(strings.Repeat(raw, 200)); err == nil {
		t.Fatal("accepted oversized input")
	}
	at := time.Now()
	r, err := NewConfigReview("owner", "session", ConfigReview{Document: raw, Objects: []installation.ObjectPlan{{Key: "MCPConnection/example", Action: "update", Revision: "old"}}}, at)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.ValidateConfirmation("owner", "session", r.Digest, at); err != nil {
		t.Fatal(err)
	}
	r.Configuration.Consent = true
	if err = r.ValidateConfirmation("owner", "session", r.Digest, at); err == nil {
		t.Fatal("consent was not bound")
	}
	r.Configuration.Consent = false
	r.Configuration.Document += " "
	if err = r.ValidateConfirmation("owner", "session", r.Digest, at); err == nil {
		t.Fatal("document was not bound")
	}
}

func TestInstallationReviewRejectsCredentialAssignmentsAtIngress(t *testing.T) {
	for _, raw := range []string{`{"apiKey": "synthetic-value"}`, `{'password':'synthetic-value'}`, `token: synthetic-value`, `{"clientSecret":"synthetic-value"}`} {
		if ValidateMessage(raw) == nil {
			t.Errorf("accepted credential assignment: %s", raw)
		}
	}
}

func TestInstallationReviewDoesNotCopySensitiveRemovedFields(t *testing.T) {
	for _, change := range []installation.Change{
		{Path: "spec.headers.Authorization", Before: "fixture"},
		{Path: "spec.environment", Before: map[string]any{"token": "fixture"}},
	} {
		_, err := NewConfigReview("owner", "session", ConfigReview{Document: "test", Objects: []installation.ObjectPlan{{Key: "Graph/test", Changes: []installation.Change{change}}}}, time.Now())
		if err == nil {
			t.Fatalf("unsafe prior state persisted: %s", change.Path)
		}
	}
}
