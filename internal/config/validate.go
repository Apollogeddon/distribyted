package config

import (
	"errors"
	"fmt"
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

	return nil
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
