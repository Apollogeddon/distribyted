package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidate_HTTPMissingCredentials(t *testing.T) {
	t.Parallel()

	r := &Root{HTTPGlobal: &HTTPGlobal{}}
	require.Error(t, Validate(r))
}

func TestValidate_HTTPAuthExplicitlyDisabled(t *testing.T) {
	t.Parallel()

	r := &Root{HTTPGlobal: &HTTPGlobal{DisableAuth: true}}
	require.NoError(t, Validate(r))
}

func TestValidate_HTTPWithCredentials(t *testing.T) {
	t.Parallel()

	r := &Root{HTTPGlobal: &HTTPGlobal{User: "admin", Pass: "admin"}}
	require.NoError(t, Validate(r))
}

func TestValidate_WebDAVMissingCredentials(t *testing.T) {
	t.Parallel()

	r := &Root{WebDAV: &WebDAVGlobal{}}
	require.Error(t, Validate(r))
}

func TestValidate_WebDAVWithCredentials(t *testing.T) {
	t.Parallel()

	r := &Root{WebDAV: &WebDAVGlobal{User: "admin", Pass: "admin"}}
	require.NoError(t, Validate(r))
}

func TestValidate_NilSections(t *testing.T) {
	t.Parallel()

	require.NoError(t, Validate(&Root{}))
}

func TestValidate_TemplateConfig(t *testing.T) {
	t.Parallel()

	conf := AddDefaults(DefaultConfig())
	require.NoError(t, Validate(conf))
}

func TestValidateRouteName(t *testing.T) {
	for _, name := range []string{"multimedia", "TV Shows", "films-2024", "série"} {
		if err := ValidateRouteName(name); err != nil {
			t.Errorf("ValidateRouteName(%q) = %v, want nil", name, err)
		}
	}
	for _, name := range []string{"", ".", "..", "a/b", "../link", `a\b`, "a\nb", "a\x00b"} {
		if err := ValidateRouteName(name); err == nil {
			t.Errorf("ValidateRouteName(%q) = nil, want an error", name)
		}
	}
}

func TestValidate_NumbersOutOfRange(t *testing.T) {
	t.Parallel()

	valid := func() *Root {
		return AddDefaults(&Root{
			HTTPGlobal: &HTTPGlobal{User: "u", Pass: "p"},
			WebDAV:     &WebDAVGlobal{User: "u", Pass: "p"},
			Servers:    []*Server{{Name: "s"}},
		})
	}
	require.NoError(t, Validate(valid()))

	cases := map[string]func(r *Root){
		"http port":         func(r *Root) { r.HTTPGlobal.Port = 70000 },
		"webdav port":       func(r *Root) { r.WebDAV.Port = -1 },
		"read timeout":      func(r *Root) { r.Torrent.ReadTimeout = -1 },
		"add timeout":       func(r *Root) { r.Torrent.AddTimeout = -5 },
		"cache size":        func(r *Root) { r.Torrent.GlobalCacheSize = -2 },
		"extract limit":     func(r *Root) { r.Torrent.ArchiveExtractLimit = -2 },
		"listen port":       func(r *Root) { r.Torrent.ListenPort = 65536 },
		"conns per torrent": func(r *Root) { r.Torrent.MaxConnsPerTorrent = -1 },
		"torrent ip":        func(r *Root) { r.Torrent.IP = "1.2.3" },
		"log size":          func(r *Root) { r.Log.MaxSize = -1 },
		"watcher interval":  func(r *Root) { r.Servers[0].WatcherInterval = -1 },
	}
	for name, mutate := range cases {
		r := valid()
		mutate(r)
		require.Error(t, Validate(r), name)
	}

	// the documented "no limit" and "any port" values stay valid
	r := valid()
	r.Torrent.GlobalCacheSize, r.Torrent.ArchiveExtractLimit, r.Torrent.ListenPort = -1, -1, -1
	r.HTTPGlobal.IP = "::1"
	require.NoError(t, Validate(r))
}

func TestExposed(t *testing.T) {
	t.Parallel()

	r := &Root{HTTPGlobal: &HTTPGlobal{IP: "127.0.0.1"}, WebDAV: &WebDAVGlobal{IP: "::1"}}
	require.Empty(t, Exposed(r))
	require.Empty(t, Exposed(&Root{HTTPGlobal: &HTTPGlobal{IP: "localhost"}}))

	r = &Root{HTTPGlobal: &HTTPGlobal{IP: "0.0.0.0"}, WebDAV: &WebDAVGlobal{IP: "192.168.1.2"}}
	require.Equal(t, []string{"http", "webdav"}, Exposed(r))
	require.Equal(t, []string{"http"}, Exposed(&Root{HTTPGlobal: &HTTPGlobal{IP: "::"}}))
}
