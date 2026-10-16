package config

import (
	"io"
	"os"
	"testing"

	"github.com/Apollogeddon/distribyted/web"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
	yamlv3 "gopkg.in/yaml.v3"
)

func TestTemplateConfig(t *testing.T) {
	t.Parallel()

	require := require.New(t)
	f, err := web.Templates.Open("templates/config_template.yaml")
	require.NoError(err)
	defer f.Close()

	b, err := io.ReadAll(f)
	require.NoError(err)

	conf := &Root{}
	err = yaml.Unmarshal(b, conf)
	require.NoError(err)

	require.Equal(DefaultConfig(), conf)
}

func TestDefaults(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	r := &Root{}
	dr := AddDefaults(r)
	require.NotNil(dr)

	// FUSE can be deactivated
	require.Nil(dr.Fuse)
	require.NotNil(dr.HTTPGlobal)
	require.NotNil(dr.Log)
	require.NotNil(dr.Torrent)

	// Add defaults when fuse is set
	r = &Root{
		Fuse: &FuseGlobal{},
	}

	dr = AddDefaults(r)
	require.NotNil(dr.Fuse)
	require.Equal(mountFolder, dr.Fuse.Path)
}

// An http: or webdav: section that leaves out port or ip gets the documented defaults, not a
// random port.
func TestDefaults_PortsAndAddresses(t *testing.T) {
	t.Parallel()

	dr := AddDefaults(&Root{HTTPGlobal: &HTTPGlobal{User: "u", Pass: "p"}, WebDAV: &WebDAVGlobal{User: "u", Pass: "p"}})
	require.Equal(t, DefaultIP, dr.HTTPGlobal.IP)
	require.Equal(t, DefaultHTTPPort, dr.HTTPGlobal.Port)
	require.Equal(t, DefaultIP, dr.WebDAV.IP)
	require.Equal(t, DefaultWebDAVPort, dr.WebDAV.Port)

	dr = AddDefaults(&Root{
		HTTPGlobal: &HTTPGlobal{IP: "127.0.0.1", Port: 8080},
		WebDAV:     &WebDAVGlobal{IP: "127.0.0.2", Port: 8081},
	})
	require.Equal(t, "127.0.0.1", dr.HTTPGlobal.IP)
	require.Equal(t, 8080, dr.HTTPGlobal.Port)
	require.Equal(t, "127.0.0.2", dr.WebDAV.IP)
	require.Equal(t, 8081, dr.WebDAV.Port)

	// no webdav: section means no WebDAV
	require.Nil(t, AddDefaults(&Root{}).WebDAV)
}

// The example the README points to stays the configuration distribyted generates, in the
// current schema, with the passwords left for the user to choose.
func TestExampleConfig(t *testing.T) {
	t.Parallel()

	f, err := os.Open("../../examples/conf_example.yaml")
	require.NoError(t, err)
	defer f.Close()
	dec := yamlv3.NewDecoder(f)
	dec.KnownFields(true)
	conf := &Root{}
	require.NoError(t, dec.Decode(conf))

	want := DefaultConfig()
	want.HTTPGlobal.Pass, want.WebDAV.Pass = "", ""
	require.Equal(t, want, conf)
	require.Equal(t, DefaultIP, conf.HTTPGlobal.IP)
	require.Equal(t, DefaultIP, conf.WebDAV.IP)

	// it doesn't start until the passwords are set, and then does
	require.Error(t, Validate(AddDefaults(conf)))
	conf.HTTPGlobal.Pass, conf.WebDAV.Pass = "chosen", "chosen"
	require.NoError(t, Validate(conf))
}
