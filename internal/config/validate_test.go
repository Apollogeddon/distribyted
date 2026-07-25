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
