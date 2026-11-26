package api

import "sync"

// Chat inference executes inside this server process. Bound the number of active
// owners as well as calls so idle or revoked identities leave no bookkeeping.
type chatAdmission struct {
	mu     sync.Mutex
	total  int
	active map[string]int
}

func (a *chatAdmission) acquire(owner string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if owner == "" || a.total >= 4 || a.active[owner] >= 2 {
		return false
	}
	if a.active == nil {
		a.active = make(map[string]int)
	}
	a.total++
	a.active[owner]++
	return true
}

func (a *chatAdmission) release(owner string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.total--
	if a.active[owner] == 1 {
		delete(a.active, owner)
	} else {
		a.active[owner]--
	}
}
