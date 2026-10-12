package config

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/rs/zerolog/log"
)

// Validate rejects configurations that would silently run unauthenticated.
// It must be called after AddDefaults.
func Validate(r *Root) error {
	if r.HTTPGlobal != nil && !r.HTTPGlobal.DisableAuth {
		if r.HTTPGlobal.User == "" || r.HTTPGlobal.Pass == "" {
			return errors.New("http.user and http.pass must be set (or set http.disable_auth: true to run the web interface and API unauthenticated)")
		}
	}

	if r.WebDAV != nil {
		if r.WebDAV.User == "" || r.WebDAV.Pass == "" {
			return errors.New("webdav.user and webdav.pass must be set; remove the webdav: section to disable WebDAV")
		}
	}

	if r.HTTPGlobal != nil {
		warnDefaultCredentials("http", !r.HTTPGlobal.DisableAuth, r.HTTPGlobal.User, r.HTTPGlobal.Pass)
	}
	if r.WebDAV != nil {
		warnDefaultCredentials("webdav", true, r.WebDAV.User, r.WebDAV.Pass)
	}

	for _, rt := range r.Routes {
		if err := ValidateRouteName(rt.Name); err != nil {
			return err
		}
	}

	return validateNumbers(r)
}

// validateNumbers rejects ports, timeouts and sizes out of range, and a torrent.ip that isn't one. 0 means the
// default for each; a negative timeout would fail every read or add at once, and a negative
// watcher interval stops the watcher with a panic.
func validateNumbers(r *Root) error {
	var errs []error
	check := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}
	port := func(section string, port int) {
		check(port >= 0 && port <= 65535, "%s.port %d is not a port (1-65535)", section, port)
	}
	if h := r.HTTPGlobal; h != nil {
		port("http", h.Port)
	}
	if w := r.WebDAV; w != nil {
		port("webdav", w.Port)
	}
	if t := r.Torrent; t != nil {
		check(t.ReadTimeout >= 0, "torrent.read_timeout %d is negative", t.ReadTimeout)
		check(t.AddTimeout >= 0, "torrent.add_timeout %d is negative", t.AddTimeout)
		check(t.GlobalCacheSize >= -1, "torrent.global_cache_size %d: use -1 for no limit", t.GlobalCacheSize)
		check(t.ArchiveExtractLimit >= -1, "torrent.archive_extract_limit %d: use -1 for no limit", t.ArchiveExtractLimit)
		check(t.ListenPort >= -1 && t.ListenPort <= 65535, "torrent.listen_port %d is not a port (-1 for any)", t.ListenPort)
		check(t.MaxConnsPerTorrent >= 0, "torrent.max_conns_per_torrent %d is negative", t.MaxConnsPerTorrent)
		check(t.IP == "" || net.ParseIP(t.IP) != nil, "torrent.ip %q is not an IP address", t.IP)
	}
	if l := r.Log; l != nil {
		check(l.MaxBackups >= 0 && l.MaxSize >= 0 && l.MaxAge >= 0, "log.max_backups, max_size and max_age can't be negative")
	}
	for _, s := range r.Servers {
		check(s.WatcherInterval >= 0, "servers %q: watcher_interval %d is negative", s.Name, s.WatcherInterval)
	}
	return errors.Join(errs...)
}

// ValidateRouteName rejects route names that aren't a single path segment. A route name
// becomes a folder at the root of the mount and part of a database key, so "." or ".."
// would mount over the root and a "/" would reach into other keys.
func ValidateRouteName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid route name %q", name)
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("invalid route name %q: it can't contain / or \\", name)
	}
	for _, c := range name {
		if c < 0x20 || c == 0x7f {
			return fmt.Errorf("invalid route name %q: it can't contain control characters", name)
		}
	}
	return nil
}

// warnDefaultCredentials flags the admin/admin login that older config templates shipped
// with. It isn't rejected, so existing setups keep starting.
func warnDefaultCredentials(section string, enabled bool, user, pass string) {
	if enabled && user == "admin" && pass == "admin" {
		log.Warn().Str("section", section).Msg("the login is still admin/admin; change " + section + ".pass in the configuration file")
	}
}
