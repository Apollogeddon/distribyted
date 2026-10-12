package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/Apollogeddon/distribyted/internal/config"
)

const healthcheckTimeout = 5 * time.Second

// healthcheck asks the web server of a distribyted running on this host whether it's healthy, or
// ready, so a container image needs no curl. It finds the server as distribyted does: the port
// and address flags, else the configuration file, which it reads but never creates, else the
// defaults.
func healthcheck(configPath, ip string, port int, ready bool) error {
	url, err := healthcheckURL(configPath, ip, port, ready)
	if err != nil {
		return cli.Exit(err.Error(), 1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), healthcheckTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return cli.Exit(err.Error(), 1)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return cli.Exit(fmt.Sprintf("unhealthy: %v", err), 1)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return cli.Exit("unhealthy: "+resp.Status, 1)
	}
	return nil
}

func healthcheckURL(configPath, ip string, port int, ready bool) (string, error) {
	conf := config.AddDefaults(&config.Root{})
	if _, err := os.Stat(configPath); err == nil {
		if conf, err = config.NewHandler(configPath).Get(); err != nil {
			return "", err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("error reading configuration file: %w", err)
	}
	if port == 0 {
		port = conf.HTTPGlobal.Port
	}

	host := conf.HTTPGlobal.IP
	if ip != "" {
		host = ip
	}
	// a server listening on every address is reached on loopback
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}

	path := "/healthz"
	if ready {
		path = "/readyz"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + path, nil
}
