// Package prefs keeps user preferences.
package prefs

import "sync"

// Store keeps preference values by key.
type Store interface {
	Get(key string) (string, bool)
	Set(key, value string)
	Delete(key string)
}

// MemStore is an in-memory Store, safe for concurrent use.
type MemStore struct {
	mu sync.Mutex
	m  map[string]string
}

// NewMemStore returns an empty MemStore.
func NewMemStore() *MemStore {
	return &MemStore{m: map[string]string{}}
}

// Get returns the value of key.
func (s *MemStore) Get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	return v, ok
}

// Set stores value under key.
func (s *MemStore) Set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = value
}

// Delete removes key.
func (s *MemStore) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
}
