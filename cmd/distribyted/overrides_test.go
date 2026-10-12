package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Apollogeddon/distribyted/internal/config"
)

func TestOverridesApply(t *testing.T) {
	conf := config.AddDefaults(&config.Root{
		HTTPGlobal: &config.HTTPGlobal{User: "u", Pass: "p"},
		WebDAV:     &config.WebDAVGlobal{User: "u", Pass: "p"},
	})

	// nothing given keeps the config's
	overrides{}.apply(conf)
	require.Equal(t, config.DefaultIP, conf.HTTPGlobal.IP)
	require.Equal(t, config.DefaultWebDAVPort, conf.WebDAV.Port)

	overrides{httpIP: "0.0.0.0", webDAVIP: "0.0.0.0", httpPort: 8080, webDAVPort: 8081}.apply(conf)
	require.Equal(t, "0.0.0.0", conf.HTTPGlobal.IP)
	require.Equal(t, 8080, conf.HTTPGlobal.Port)
	require.Equal(t, "0.0.0.0", conf.WebDAV.IP)
	require.Equal(t, 8081, conf.WebDAV.Port)

	// a config without WebDAV keeps it off
	noDAV := config.AddDefaults(&config.Root{})
	overrides{webDAVIP: "0.0.0.0", webDAVPort: 8081}.apply(noDAV)
	require.Nil(t, noDAV.WebDAV)
}
