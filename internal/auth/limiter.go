package auth

import (
	"strconv"
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
	return l.Wait(addr) == 0
}

// Wait is how long addr must wait before it may try to log in again; 0 means now.
func (l *Limiter) Wait(addr string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.attempts[addr]
	if !ok {
		return 0
	}
	return max(a.until.Sub(l.now()), 0)
}

// RetryAfter is Wait as a Retry-After header's whole seconds, rounded up, for a refused login.
// It's at least 1: the wait can run out between the refusal and this, and "0" would invite
// a client to retry at once, in a loop.
func (l *Limiter) RetryAfter(addr string) string {
	return strconv.FormatInt(max(int64((l.Wait(addr)+time.Second-1)/time.Second), 1), 10)
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
