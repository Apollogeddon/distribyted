package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigPath(t *testing.T) {
	// no argument: --config, or its default
	p, err := configPath(nil, defaultConfigPath, false)
	require.NoError(t, err)
	require.Equal(t, defaultConfigPath, p)
	p, err = configPath(nil, "flag.yaml", true)
	require.NoError(t, err)
	require.Equal(t, "flag.yaml", p)

	// the one argument is the configuration file, as `task start -- my-config.yaml` passes it
	p, err = configPath([]string{"my-config.yaml"}, defaultConfigPath, false)
	require.NoError(t, err)
	require.Equal(t, "my-config.yaml", p)
	p, err = configPath([]string{"same.yaml"}, "same.yaml", true)
	require.NoError(t, err)
	require.Equal(t, "same.yaml", p)

	// anything else is refused rather than ignored
	_, err = configPath([]string{"a.yaml", "b.yaml"}, defaultConfigPath, false)
	require.ErrorContains(t, err, "unexpected arguments")
	_, err = configPath([]string{"my-config.yaml", "--http-port"}, defaultConfigPath, false)
	require.ErrorContains(t, err, "unexpected arguments")
	_, err = configPath([]string{"b.yaml"}, "a.yaml", true)
	require.ErrorContains(t, err, "two configuration files")
}
