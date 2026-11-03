package loader

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDB_LinksAdded(t *testing.T) {
	db, err := NewDB("")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	at := time.Unix(1_790_000_000, 0)
	require.NoError(t, db.SetLinkAdded("/movies/Dune (2021)/Dune.mkv", at))
	require.NoError(t, db.SetLinkAdded("/series/Show/S01E01.mkv", at.Add(time.Hour)))
	got, err := db.LinksAdded()
	require.NoError(t, err)
	require.Equal(t, map[string]time.Time{
		"/movies/Dune (2021)/Dune.mkv": at,
		"/series/Show/S01E01.mkv":      at.Add(time.Hour),
	}, got)

	require.NoError(t, db.ForgetLinkAdded("/movies/Dune (2021)/Dune.mkv"))
	got, err = db.LinksAdded()
	require.NoError(t, err)
	require.Len(t, got, 1)
}
