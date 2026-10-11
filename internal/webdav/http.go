package webdav

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/Apollogeddon/distribyted/internal/auth"
	"github.com/Apollogeddon/distribyted/internal/fs"
	dlog "github.com/Apollogeddon/distribyted/internal/log"
	"github.com/rs/zerolog/log"
)

// readHeaderTimeout bounds how long a client may take to send its request headers. There's
// no read or write timeout, as streaming a large file can rightly take hours.
const readHeaderTimeout = 30 * time.Second

func NewWebDAVServer(fs fs.Filesystem, port int, user, pass string) error {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if ok && auth.CredentialsMatch(username, password, user, pass) {
			srv.ServeHTTP(w, r)
			return
		}

		w.Header().Set("WWW-Authenticate", `Basic realm="BASIC WebDAV REALM"`)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("401 Unauthorized\n"))
	})
}
