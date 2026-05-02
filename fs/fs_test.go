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

func TestContainerFs_Mutation(t *testing.T) {
	require := require.New(t)
	cfs, _ := NewContainerFs(nil)
	
	// Create
	err := cfs.Create("/test-route/file.txt")
	require.NoError(err) // storage.Add usually handles parent creation
	
	// Remove
	err = cfs.Remove("/test-route/file.txt")
	require.NoError(err)
	
	// RemoveByHash
	cfs.RemoveByHash("any-hash")
}

func TestContainerFs_Links(t *testing.T) {
	require := require.New(t)
	cfs, _ := NewContainerFs(nil)
	
	linkAdded := false
	cfs.OnLinkAdded(func(o, n string) {
		linkAdded = true
	})
	
	linkRemoved := false
	cfs.OnLinkRemoved(func(p string) {
		linkRemoved = true
	})
	
	cfs.onLinkAdded("o", "n")
	require.True(linkAdded)
	
	cfs.onLinkRemoved("p")
	require.True(linkRemoved)
}
