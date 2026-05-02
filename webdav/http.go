package webdav

import (
	"fmt"
	"net"
	"net/http"

	"github.com/Apollogeddon/distribyted/fs"
	dlog "github.com/Apollogeddon/distribyted/log"
	"github.com/rs/zerolog/log"
)

func NewWebDAVServer(fs fs.Filesystem, port int, user, pass string) error {
	log.Info().Str(dlog.KeyHost, fmt.Sprintf("0.0.0.0:%d", port)).Msg("starting webDAV server")
	return http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", port), NewWebDAVHandler(fs, user, pass))
}

func NewWebDAVServerWithListener(l net.Listener, fs fs.Filesystem, user, pass string) error {
	return http.Serve(l, NewWebDAVHandler(fs, user, pass))
}

func NewWebDAVHandler(fs fs.Filesystem, user, pass string) http.Handler {
	srv := newHandler(fs)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, _ := r.BasicAuth()
		if username == user && password == pass {
			srv.ServeHTTP(w, r)
			return
		}

		w.Header().Set("WWW-Authenticate", `Basic realm="BASIC WebDAV REALM"`)
		w.WriteHeader(401)
		_, _ = w.Write([]byte("401 Unauthorized\n"))
	})
}
