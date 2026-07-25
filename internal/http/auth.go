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

func setSessionCookie(c *gin.Context, sid string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookieName, sid, int(sessionTTL.Seconds()), "/", "", false, true)
}

func clearSessionCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookieName, "", -1, "/", "", false, true)
}

// --- qBittorrent-compatible API (/api/v2) ---

func qBitLoginHandler(ac authConfig, st *sessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		if ac.disabled {
			c.String(http.StatusOK, "Ok.")
			return
		}

		user := firstNonEmpty(c.PostForm("username"), c.Query("username"))
		pass := firstNonEmpty(c.PostForm("password"), c.Query("password"))

		if !auth.CredentialsMatch(user, pass, ac.user, ac.pass) {
			c.String(http.StatusOK, "Fails.")
			return
		}

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
		"Next":  safeNext(c.Query("next")),
		"Error": c.Query("error") == "1",
	})
}

func loginSubmitHandler(ac authConfig, st *sessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		next := safeNext(c.PostForm("next"))

		if ac.disabled || auth.CredentialsMatch(c.PostForm("username"), c.PostForm("password"), ac.user, ac.pass) {
			if !ac.disabled {
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
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "/"
	}
	return next
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
