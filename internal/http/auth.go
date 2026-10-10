package http

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Apollogeddon/distribyted/internal/auth"
	"github.com/Apollogeddon/distribyted/internal/config"
	"github.com/gin-gonic/gin"
)

const (
	sessionCookieName = "SID"
	sessionTTL        = time.Hour
)

// authConfig holds the credentials the HTTP server checks logins against.
type authConfig struct {
	user, pass string
	disabled   bool
}

func newAuthConfig(c *config.HTTPGlobal) authConfig {
	if c == nil {
		return authConfig{}
	}
	return authConfig{user: c.User, pass: c.Pass, disabled: c.DisableAuth}
}

// sessionStore tracks active session IDs with a sliding expiry. Expired
// entries are pruned opportunistically on create, so no background
// goroutine is needed.
type sessionStore struct {
	mu       sync.RWMutex
	sessions map[string]time.Time
	ttl      time.Duration
}

// loginLimiter slows password guessing: after a few failed logins from one address, that
// address must wait before trying again, longer after each further failure.
type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]*loginAttempts
	now      func() time.Time
}

type loginAttempts struct {
	failures int
	until    time.Time // no attempts before this
	last     time.Time
}

const (
	freeLoginAttempts = 5
	maxLoginWait      = 15 * time.Minute
)

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: make(map[string]*loginAttempts), now: time.Now}
}

// allowed reports whether addr may try to log in now.
func (l *loginLimiter) allowed(addr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.attempts[addr]
	return !ok || !l.now().Before(a.until)
}

func (l *loginLimiter) failed(addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for k, a := range l.attempts {
		if now.Sub(a.last) > maxLoginWait && !now.Before(a.until) {
			delete(l.attempts, k)
		}
	}
	a, ok := l.attempts[addr]
	if !ok {
		a = &loginAttempts{}
		l.attempts[addr] = a
	}
	a.failures++
	a.last = now
	if a.failures >= freeLoginAttempts {
		wait := time.Second << min(a.failures-freeLoginAttempts, 10)
		a.until = now.Add(min(wait, maxLoginWait))
	}
}

func (l *loginLimiter) succeeded(addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, addr)
}

// clientAddr is the connection's own address: X-Forwarded-For can be set by the client,
// which would let anyone sidestep the limit.
func clientAddr(c *gin.Context) string {
	return c.RemoteIP()
}

func newSessionStore(ttl time.Duration) *sessionStore {
	return &sessionStore{sessions: make(map[string]time.Time), ttl: ttl}
}

func (s *sessionStore) create() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	sid := base64.RawURLEncoding.EncodeToString(b)

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, exp := range s.sessions {
		if now.After(exp) {
			delete(s.sessions, id)
		}
	}
	s.sessions[sid] = now.Add(s.ttl)

	return sid, nil
}

func (s *sessionStore) validate(sid string) bool {
	if sid == "" {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	exp, ok := s.sessions[sid]
	if !ok || time.Now().After(exp) {
		delete(s.sessions, sid)
		return false
	}

	s.sessions[sid] = time.Now().Add(s.ttl) // sliding expiry
	return true
}

func (s *sessionStore) destroy(sid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sid)
}

func sessionValid(c *gin.Context, ac authConfig, st *sessionStore) bool {
	if ac.disabled {
		return true
	}
	sid, err := c.Cookie(sessionCookieName)
	if err != nil || sid == "" {
		return false
	}
	return st.validate(sid)
}

// setSessionCookie sets a browser-session cookie. The server expires a session after an
// hour without use; a Max-Age of an hour expired it in the browser an hour after login, even
// for someone using it all along.
func setSessionCookie(c *gin.Context, sid string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookieName, sid, 0, "/", "", overHTTPS(c), true)
}

func clearSessionCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookieName, "", -1, "/", "", overHTTPS(c), true)
}

// overHTTPS reports whether the browser reached distribyted over HTTPS, directly or through
// a proxy that terminates TLS. The session cookie is Secure then. It can't always be: most
// installs are plain HTTP on a home network, where browsers drop a Secure cookie and nobody
// could log in.
func overHTTPS(c *gin.Context) bool {
	return c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
}

// --- qBittorrent-compatible API (/api/v2) ---

func qBitLoginHandler(ac authConfig, st *sessionStore, ll *loginLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if ac.disabled {
			c.String(http.StatusOK, "Ok.")
			return
		}

		addr := clientAddr(c)
		if !ll.allowed(addr) {
			c.String(http.StatusTooManyRequests, "Fails.")
			return
		}

		user := c.PostForm("username")
		pass := c.PostForm("password")

		if !auth.CredentialsMatch(user, pass, ac.user, ac.pass) {
			ll.failed(addr)
			c.String(http.StatusOK, "Fails.")
			return
		}
		ll.succeeded(addr)

		sid, err := st.create()
		if err != nil {
			c.String(http.StatusInternalServerError, "Fails.")
			return
		}

		setSessionCookie(c, sid)
		c.String(http.StatusOK, "Ok.")
	}
}

func qBitLogoutHandler(st *sessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sid, err := c.Cookie(sessionCookieName); err == nil {
			st.destroy(sid)
		}
		clearSessionCookie(c)
		c.String(http.StatusOK, "Ok.")
	}
}

func qbitAuthMiddleware(ac authConfig, st *sessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sessionValid(c, ac, st) {
			c.Next()
			return
		}
		c.String(http.StatusForbidden, "Forbidden")
		c.Abort()
	}
}

// --- Browser-facing auth (WebUI + HTTPFS) ---

func browserAuthMiddleware(ac authConfig, st *sessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sessionValid(c, ac, st) {
			c.Next()
			return
		}
		next := url.QueryEscape(c.Request.URL.RequestURI())
		c.Redirect(http.StatusFound, "/login?next="+next)
		c.Abort()
	}
}

func loginPageHandler(c *gin.Context) {
	c.HTML(http.StatusOK, "login.html", gin.H{
		"Next":    safeNext(c.Query("next")),
		"Error":   c.Query("error") == "1",
		"TooMany": c.Query("error") == "2",
	})
}

func loginSubmitHandler(ac authConfig, st *sessionStore, ll *loginLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		next := safeNext(c.PostForm("next"))
		addr := clientAddr(c)

		if !ac.disabled && !ll.allowed(addr) {
			c.Redirect(http.StatusFound, "/login?error=2&next="+url.QueryEscape(next))
			return
		}

		if ac.disabled || auth.CredentialsMatch(c.PostForm("username"), c.PostForm("password"), ac.user, ac.pass) {
			if !ac.disabled {
				ll.succeeded(addr)
				sid, err := st.create()
				if err != nil {
					c.Redirect(http.StatusFound, "/login?error=1&next="+url.QueryEscape(next))
					return
				}
				setSessionCookie(c, sid)
			}
			c.Redirect(http.StatusFound, next)
			return
		}

		ll.failed(addr)
		c.Redirect(http.StatusFound, "/login?error=1&next="+url.QueryEscape(next))
	}
}

func logoutHandler(st *sessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sid, err := c.Cookie(sessionCookieName); err == nil {
			st.destroy(sid)
		}
		clearSessionCookie(c)
		c.Redirect(http.StatusFound, "/login")
	}
}

// safeNext keeps redirect targets confined to this site, preventing an
// open redirect via a crafted ?next= value.
func safeNext(next string) string {
	// browsers read a backslash as a slash, so /\evil.com is //evil.com, another host
	if next == "" || !strings.HasPrefix(next, "/") || strings.ContainsAny(next, "\\") {
		return "/"
	}
	for _, r := range next {
		if r < 0x20 || r == 0x7f {
			return "/"
		}
	}
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" || strings.HasPrefix(next, "//") {
		return "/"
	}
	return next
}
