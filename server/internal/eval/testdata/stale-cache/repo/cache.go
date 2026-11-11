package prefs

import "sync"

// Cache is a read-through cache in front of a Store: a value read once is
// served from memory afterwards, so the Store is not asked again.
type Cache struct {
	next Store
	mu   sync.Mutex
	m    map[string]string
}

// NewCache returns a Cache in front of next.
func NewCache(next Store) *Cache {
	return &Cache{next: next, m: map[string]string{}}
}

// Get returns the value of key, from memory when it was read before.
func (c *Cache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.m[key]; ok {
		return v, true
	}
	v, ok := c.next.Get(key)
	if ok {
		c.m[key] = v
	}
	return v, ok
}

// Set writes value to the Store and remembers it.
func (c *Cache) Set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next.Set(key, value)
	c.m[key] = value
}
