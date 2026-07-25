package config

import "errors"

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

	return nil
}
