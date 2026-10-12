package auth

import (
	"sync"
	"time"
)

const (
	// FreeAttempts is how many logins in a row may fail from one address before it must wait.
	FreeAttempts = 5
	maxWait      = 15 * time.Minute
	// defaultMaxTracked bounds how many addresses are remembered; past it, the one that
	// failed longest ago is forgotten
	defaultMaxTracked = 10000
)

// Limiter slows password guessing: after a few failed logins from one address, that address
// must wait before trying again, longer after each further failure.
type Limiter struct {
	mu         sync.Mutex
	attempts   map[string]*attempts
	now        func() time.Time
	maxTracked int
}

type attempts struct {
	failures int
	until    time.Time // no attempts before this
	last     time.Time
}

func NewLimiter() *Limiter {
	return &Limiter{attempts: make(map[string]*attempts), now: time.Now, maxTracked: defaultMaxTracked}
}

// Allowed reports whether addr may try to log in now.
func (l *Limiter) Allowed(addr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.attempts[addr]
	return !ok || !l.now().Before(a.until)
}

// Failed records a failed login from addr.
func (l *Limiter) Failed(addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for k, a := range l.attempts {
		if now.Sub(a.last) > maxWait && !now.Before(a.until) {
			delete(l.attempts, k)
		}
	}
	a, ok := l.attempts[addr]
	if !ok {
		if len(l.attempts) >= l.maxTracked {
			l.forgetOldest()
		}
		a = &attempts{}
		l.attempts[addr] = a
	}
	a.failures++
	a.last = now
	if a.failures >= FreeAttempts {
		wait := time.Second << min(a.failures-FreeAttempts, 10)
		a.until = now.Add(min(wait, maxWait))
	}
}

func (l *Limiter) forgetOldest() {
	var oldest string
	var at time.Time
	for k, a := range l.attempts {
		if oldest == "" || a.last.Before(at) {
			oldest, at = k, a.last
		}
	}
	delete(l.attempts, oldest)
}

// Succeeded clears addr's failed logins.
func (l *Limiter) Succeeded(addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, addr)
}
