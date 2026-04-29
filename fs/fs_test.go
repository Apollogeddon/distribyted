package fs

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileinfo(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	fi := NewFileInfo("name", 42, false)

	require.Equal(fi.IsDir(), false)
	require.Equal(fi.Name(), "name")
	require.Equal(fi.Size(), int64(42))
	require.NotNil(fi.ModTime())
	require.Equal(fi.Mode(), os.FileMode(0777))
	require.Equal(fi.Sys(), nil)

	fiDir := NewFileInfo("dir", 0, true)
	require.Equal(fiDir.IsDir(), true)
	require.Equal(fiDir.Mode(), os.FileMode(0777)|os.ModeDir)
}

func TestDir(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	d := &Dir{}
	require.True(d.IsDir())
	require.Equal(int64(0), d.Size())
	require.NoError(d.Close())

	n, err := d.Read(nil)
	require.Equal(0, n)
	require.NoError(err)

	n, err = d.ReadAt(nil, 0)
	require.Equal(0, n)
	require.NoError(err)
}
