package config

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Apollogeddon/distribyted/web"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

type EventFunc func(event string)
type ReloadFunc func(*Root, EventFunc) error

type Handler struct {
	p string
}

func NewHandler(path string) *Handler {
	return &Handler{p: path}
}

// generateRandomPassword mirrors the session-ID generation in
// internal/http/auth.go's sessionStore.create().
func generateRandomPassword() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (c *Handler) createFromTemplateFile() ([]byte, error) {
	t, err := web.Templates.Open("templates/config_template.yaml")
	if err != nil {
		return nil, err
	}
	defer func() { _ = t.Close() }()

	tb, err := io.ReadAll(t)
	if err != nil {
		return nil, err
	}

	pass, err := generateRandomPassword()
	if err != nil {
		return nil, fmt.Errorf("error generating default password: %w", err)
	}
	tb = bytes.ReplaceAll(tb, []byte("pass: admin"), []byte("pass: "+pass))
	log.Warn().Str("password", pass).Msg("generated a random default password for http/webdav auth on first run — save it, it will not be shown again")

	if err := os.MkdirAll(filepath.Dir(c.p), 0750); err != nil {
		return nil, fmt.Errorf("error creating path for configuration file: %s, %w", c.p, err)
	}
	return tb, os.WriteFile(c.p, tb, 0600)
}

func (c *Handler) GetRaw() ([]byte, error) {
	f, err := os.ReadFile(c.p)
	if os.IsNotExist(err) {
		log.Info().Str("path", c.p).Msg("configuration file does not exist, creating from template file")
		return c.createFromTemplateFile()
	}

	if err != nil {
		return nil, fmt.Errorf("error reading configuration file: %w", err)
	}

	return f, nil
}

func (c *Handler) Get() (*Root, error) {
	b, err := c.GetRaw()
	if err != nil {
		return nil, err
	}

	conf := &Root{}
	if err := yaml.Unmarshal(b, conf); err != nil {
		return nil, fmt.Errorf("error parsing configuration file: %w", err)
	}

	conf = AddDefaults(conf)

	return conf, nil
}
