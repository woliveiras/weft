// Package store saves notes in a key-value backend.
package store

import (
	"fmt"
	"strings"

	"example.com/notes/text"
)

// MaxKeyBytes is the longest key the backend accepts, in bytes.
const MaxKeyBytes = 32

// Backend is an in-memory stand-in for the key-value service.
type Backend struct {
	data map[string]string
}

// NewBackend returns an empty Backend.
func NewBackend() *Backend {
	return &Backend{data: map[string]string{}}
}

// Put stores value under key. Keys longer than MaxKeyBytes are rejected.
func (b *Backend) Put(key, value string) error {
	if len(key) > MaxKeyBytes {
		return fmt.Errorf("store: key %q is %d bytes, the limit is %d", key, len(key), MaxKeyBytes)
	}
	b.data[key] = value
	return nil
}

// Get returns the value under key.
func (b *Backend) Get(key string) (string, bool) {
	v, ok := b.data[key]
	return v, ok
}

// Key derives the storage key of a note from its title.
func Key(title string) string {
	return text.Truncate(strings.ToLower(strings.TrimSpace(title)), MaxKeyBytes)
}

// Save stores a note's body under the key of its title.
func Save(b *Backend, title, body string) error {
	return b.Put(Key(title), body)
}
