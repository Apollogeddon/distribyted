package main

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShutdownClosesTheStoresAfterTheClient(t *testing.T) {
	var order []string
	step := func(name string) func() { return func() { order = append(order, name) } }
	store := func(name string, err error) namedCloser {
		return namedCloser{name, func() error { order = append(order, name); return err }}
	}

	shutdownPlan{
		unmount:      step("unmount"),
		closeServers: step("servers"),
		recordPeers:  step("peers"),
		closeClient:  step("client"),
		// a store that fails to close doesn't stop the others closing
		stores: []namedCloser{store("items", errors.New("boom")), store("magnets", nil), store("piece completion", nil)},
	}.run()

	require.Equal(t, []string{"unmount", "servers", "peers", "client", "items", "magnets", "piece completion"}, order)
}
