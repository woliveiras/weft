// Package domain holds the vocabulary every other package speaks: graphs,
// nodes, channels, messages, executions and the state machines that govern
// them. It has no dependencies on adapters so that queue, storage, runtime
// and API packages can all import it without cycles.
package domain

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// Message is the unit of work that crosses a channel. The six top-level
// fields are the public wire contract and are deliberately minimal; every
// piece of routing, tracing and bookkeeping data travels in Metadata under
// the well-known keys below so the envelope stays stable as the platform
// grows.
type Message struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Source   string          `json:"source"`
	Target   string          `json:"target"`
	Payload  json.RawMessage `json:"payload"`
	Metadata map[string]any  `json:"metadata"`
}

// Well-known metadata keys. Anything else in Metadata is user data and is
// carried through untouched.
const (
	MetaGraph         = "graph"          // graph id the message belongs to
	MetaGraphVersion  = "graph_version"  // graph version that produced it
	MetaChannel       = "channel"        // channel id it was published on
	MetaRunID         = "run_id"         // graph run (start..stop) it belongs to
	MetaCorrelationID = "correlation_id" // stable id across a whole workflow
	MetaCausationID   = "causation_id"   // id of the message that caused this one
	MetaExecutionID   = "execution_id"   // execution (turn) that emitted it
	MetaHops          = "hops"           // number of agent hops so far (cycle guard)
	MetaCreatedAt     = "created_at"     // RFC3339Nano timestamp
	MetaTraceParent   = "traceparent"    // W3C trace context
	MetaTraceState    = "tracestate"     // W3C trace state
	MetaInjectedBy    = "injected_by"    // set when a human injected the message
	MetaAttempt       = "attempt"        // delivery attempt, set by the consumer side
	MetaReplica       = "replica"        // replica of the source node that emitted it
	MetaHeldFor       = "held_for"       // approval id while held at a gate
)

// NewID returns a sortable, collision-resistant identifier: a millisecond
// timestamp prefix (so ids sort by creation time in every store and broker)
// followed by 80 random bits.
func NewID(prefix string) string {
	var b [10]byte
	_, _ = rand.Read(b[:])
	ts := strconv.FormatInt(time.Now().UnixMilli(), 36)
	if prefix == "" {
		return ts + "-" + hex.EncodeToString(b[:])
	}
	return prefix + "_" + ts + hex.EncodeToString(b[:])
}

// NewMessage builds a message with a fresh id, creation timestamp and
// correlation id (the message's own id, unless a parent supplies one).
func NewMessage(typ, source, target string, payload any) (*Message, error) {
	raw, err := MarshalPayload(payload)
	if err != nil {
		return nil, err
	}
	id := NewID("msg")
	return &Message{
		ID:      id,
		Type:    typ,
		Source:  source,
		Target:  target,
		Payload: raw,
		Metadata: map[string]any{
			MetaCreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
			MetaCorrelationID: id,
			MetaHops:          0,
		},
	}, nil
}

// MarshalPayload accepts raw JSON, a string or any JSON-serialisable value
// and returns it as a JSON document. A nil payload becomes {}.
func MarshalPayload(payload any) (json.RawMessage, error) {
	switch p := payload.(type) {
	case nil:
		return json.RawMessage(`{}`), nil
	case json.RawMessage:
		if len(p) == 0 {
			return json.RawMessage(`{}`), nil
		}
		if !json.Valid(p) {
			return nil, fmt.Errorf("payload is not valid JSON")
		}
		return p, nil
	case []byte:
		if !json.Valid(p) {
			return nil, fmt.Errorf("payload is not valid JSON")
		}
		return json.RawMessage(p), nil
	default:
		b, err := json.Marshal(p)
		if err != nil {
			return nil, fmt.Errorf("marshal payload: %w", err)
		}
		return b, nil
	}
}

// Child derives a message caused by m: it inherits the correlation id, run,
// graph and trace context, records m as its cause and increments the hop
// counter that protects cyclic graphs from running away.
func (m *Message) Child(typ, source, target string, payload any) (*Message, error) {
	c, err := NewMessage(typ, source, target, payload)
	if err != nil {
		return nil, err
	}
	for _, k := range []string{MetaGraph, MetaGraphVersion, MetaRunID, MetaCorrelationID, MetaTraceParent, MetaTraceState} {
		if v, ok := m.Metadata[k]; ok {
			c.Metadata[k] = v
		}
	}
	c.Metadata[MetaCausationID] = m.ID
	c.Metadata[MetaHops] = m.Hops() + 1
	return c, nil
}

// Clone returns a deep-enough copy: payload bytes and the metadata map are
// copied so per-subscriber mutation (attempt counters, channel ids) never
// leaks between deliveries.
func (m *Message) Clone() *Message {
	c := *m
	c.Payload = append(json.RawMessage(nil), m.Payload...)
	c.Metadata = make(map[string]any, len(m.Metadata))
	for k, v := range m.Metadata {
		c.Metadata[k] = v
	}
	return &c
}

// MetaString returns a metadata value as a string, or "".
func (m *Message) MetaString(key string) string {
	if m == nil || m.Metadata == nil {
		return ""
	}
	switch v := m.Metadata[key].(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

// MetaInt returns a numeric metadata value. JSON decoding turns numbers into
// float64, brokers may hand back strings; both are accepted.
func (m *Message) MetaInt(key string) int {
	if m == nil || m.Metadata == nil {
		return 0
	}
	switch v := m.Metadata[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		i, _ := v.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(v)
		return i
	}
	return 0
}

// SetMeta sets a metadata key, allocating the map if needed.
func (m *Message) SetMeta(key string, v any) {
	if m.Metadata == nil {
		m.Metadata = map[string]any{}
	}
	m.Metadata[key] = v
}

// Hops is the number of agent-to-agent hops this message has travelled.
func (m *Message) Hops() int { return m.MetaInt(MetaHops) }

// CreatedAt parses the creation timestamp; zero if absent or malformed.
func (m *Message) CreatedAt() time.Time {
	t, _ := time.Parse(time.RFC3339Nano, m.MetaString(MetaCreatedAt))
	return t
}

// Validate checks the envelope invariants every adapter relies on.
func (m *Message) Validate() error {
	if m == nil {
		return fmt.Errorf("message is nil")
	}
	if m.ID == "" {
		return fmt.Errorf("message id is required")
	}
	if m.Type == "" {
		return fmt.Errorf("message type is required")
	}
	if len(m.Payload) > 0 && !json.Valid(m.Payload) {
		return fmt.Errorf("message payload is not valid JSON")
	}
	return nil
}

// PayloadMap decodes the payload into a generic map (nil if it is not an object).
func (m *Message) PayloadMap() map[string]any {
	var out map[string]any
	_ = json.Unmarshal(m.Payload, &out)
	return out
}
