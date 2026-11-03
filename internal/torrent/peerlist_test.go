package torrent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSortPeers(t *testing.T) {
	ps := []PeerInfo{
		{Addr: "b", Down: 10},
		{Addr: "a", Down: 10},
		{Addr: "c", Down: 1, Seed: true},
		{Addr: "d", Down: 99},
	}
	SortPeers(ps)
	var got []string
	for _, p := range ps {
		got = append(got, p.Addr)
	}
	require.Equal(t, []string{"c", "d", "a", "b"}, got)
	require.Nil(t, Peers(&mockTorrent{}), "a torrent with no connections to read lists none")
}
