package loader

import (
	"testing"

	"github.com/Apollogeddon/distribyted/config"
	"github.com/stretchr/testify/require"
)

func TestConfigLoader(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	routes := []*config.Route{
		{
			Name: "route1",
			Torrents: []*config.Torrent{
				{MagnetURI: "magnet1"},
				{TorrentPath: "path1"},
			},
		},
		{
			Name: "route2",
			Torrents: []*config.Torrent{
				{MagnetURI: "magnet2"},
			},
		},
	}

	l := NewConfig(routes)
	
	magnets, err := l.ListMagnets()
	require.NoError(err)
	require.Len(magnets, 2)
	require.Equal([]string{"magnet1"}, magnets["route1"])
	require.Equal([]string{"magnet2"}, magnets["route2"])

	paths, err := l.ListTorrentPaths()
	require.NoError(err)
	require.Len(paths, 1)
	require.Equal([]string{"path1"}, paths["route1"])
}
