// Package qutil holds helpers shared by the broker adapters: name
// validation, envelope encoding and polling back-off.
package qutil

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
)

var nameRE = regexp.MustCompile(`^[A-Za-z0-9_.\-]+$`)

// ValidName checks a subject or group name against the character set every
// adapter can represent: [A-Za-z0-9_.-], not starting with a dot.
func ValidName(kind, s string) error {
	if s == "" || len(s) > 200 || !nameRE.MatchString(s) || strings.HasPrefix(s, ".") || strings.Contains(s, "..") {
		return fmt.Errorf("queue: invalid %s name %q (allowed: [A-Za-z0-9_.-], no leading dot, no '..')", kind, s)
	}
	return nil
}

// Token turns a dotted name into a single token safe for systems that treat
// '.' specially (NATS stream/consumer names, Kafka-free identifiers). A short
// hash of the original keeps distinct names distinct after replacement
// ("a.b" and "a_b" map to different tokens).
func Token(s string) string {
	r := strings.NewReplacer(".", "_", "-", "_")
	sum := sha1.Sum([]byte(s))
	t := r.Replace(s)
	if len(t) > 48 {
		t = t[:48]
	}
	return t + "_" + hex.EncodeToString(sum[:4])
}

// Graph returns the graph portion of a subject ("<graph>.<channel>").
func Graph(subject string) string {
	if i := strings.IndexByte(subject, '.'); i > 0 {
		return subject[:i]
	}
	return subject
}

// Encode serialises a message for the wire.
func Encode(m *domain.Message) ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

// Decode parses a wire message and stamps the delivery attempt into its
// metadata so agents and the engine can see it.
func Decode(b []byte, attempt int) (*domain.Message, error) {
	var m domain.Message
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("queue: decode message: %w", err)
	}
	if m.Metadata == nil {
		m.Metadata = map[string]any{}
	}
	m.Metadata[domain.MetaAttempt] = attempt
	return &m, nil
}

// Backoff is a polling back-off: starts at Min, doubles up to Max, and is
// reset whenever work is found.
type Backoff struct {
	Min, Max time.Duration
	cur      time.Duration
}

func (b *Backoff) Next() time.Duration {
	if b.cur == 0 {
		b.cur = b.Min
	} else {
		b.cur *= 2
		if b.cur > b.Max {
			b.cur = b.Max
		}
	}
	return b.cur
}

func (b *Backoff) Reset() { b.cur = 0 }

// Sleep waits d or until ctx-like done channel closes; returns false if done.
func Sleep(done <-chan struct{}, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-done:
		return false
	case <-t.C:
		return true
	}
}
