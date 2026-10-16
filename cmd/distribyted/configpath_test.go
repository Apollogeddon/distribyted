package main

import (
	"os"
	"path/filepath"
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

	// a word that isn't a configuration file is refused, not written as a new one and started on
	for _, arg := range []string{"version", "halthcheck", "sync", "config"} {
		_, err = configPath([]string{arg}, defaultConfigPath, false)
		require.ErrorIs(t, err, errUsage, arg)
		require.ErrorContains(t, err, "unknown command or argument", arg)
		require.NoFileExists(t, arg)
	}
	for _, arg := range []string{"conf.yml", "CONF.YAML", "conf.json", filepath.Join("dir", "conf.yaml")} {
		p, err = configPath([]string{arg}, defaultConfigPath, false)
		require.NoError(t, err, arg)
		require.Equal(t, arg, p)
	}
	// an existing file is taken whatever its name, but not a folder
	existing := filepath.Join(t.TempDir(), "distribyted.conf")
	require.NoError(t, os.WriteFile(existing, []byte("http:\n"), 0o600))
	p, err = configPath([]string{existing}, defaultConfigPath, false)
	require.NoError(t, err)
	require.Equal(t, existing, p)
	_, err = configPath([]string{t.TempDir()}, defaultConfigPath, false)
	require.ErrorIs(t, err, errUsage)
}
