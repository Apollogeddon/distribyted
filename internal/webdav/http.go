package webdav

import (
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/Apollogeddon/distribyted/internal/auth"
	"github.com/Apollogeddon/distribyted/internal/fs"
	dlog "github.com/Apollogeddon/distribyted/internal/log"
	"github.com/rs/zerolog/log"
)

// readHeaderTimeout bounds how long a client may take to send its request headers. There's
// no read or write timeout, as streaming a large file can rightly take hours.
const readHeaderTimeout = 30 * time.Second

func NewWebDAVServer(fs fs.Filesystem, ip string, port int, user, pass string) error {
	addr := net.JoinHostPort(ip, strconv.Itoa(port))
	log.Info().Str(dlog.KeyHost, addr).Msg("starting webDAV server")
	srv := newServer(fs, user, pass)
	srv.Addr = addr
	return srv.ListenAndServe()
}

func NewWebDAVServerWithListener(l net.Listener, fs fs.Filesystem, user, pass string) error {
	return newServer(fs, user, pass).Serve(l)
}

func newServer(fs fs.Filesystem, user, pass string) *http.Server {
	return &http.Server{
		Handler:           NewWebDAVHandler(fs, user, pass),
		ReadHeaderTimeout: readHeaderTimeout,
	}
}

func NewWebDAVHandler(fs fs.Filesystem, user, pass string) http.Handler {
	srv := newHandler(fs)
	limiter := auth.NewLimiter()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// the connection's own address, as the web UI's login limit uses
		addr, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			addr = r.RemoteAddr
		}
		if !limiter.Allowed(addr) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "429 Too Many Requests", http.StatusTooManyRequests)
			return
		}

		username, password, ok := r.BasicAuth()
		if ok && auth.CredentialsMatch(username, password, user, pass) {
			limiter.Succeeded(addr)
			srv.ServeHTTP(w, r)
			return
		}
		// clients ask without credentials first to be challenged, which isn't a guess
		if ok {
			limiter.Failed(addr)
		}

		w.Header().Set("WWW-Authenticate", `Basic realm="BASIC WebDAV REALM"`)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("401 Unauthorized\n"))
	})
}
