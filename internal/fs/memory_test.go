package fs

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemory(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	mem := NewMemory()

	_ = mem.Storage.Add(NewMemoryFile([]byte("Hello")), "/dir/here")

	fss := map[string]Filesystem{
		"/test": mem,
	}

	c, err := NewContainerFs(fss)
	require.NoError(err)

	f, err := c.Open("/test/dir/here")
	require.NoError(err)
	require.NotNil(f)
	require.Equal(int64(5), f.Size())
	require.False(f.IsDir())
	require.NoError(f.Close())

	files, err := c.ReadDir("/")
	require.NoError(err)
	require.Len(files, 1)

	files, err = c.ReadDir("/test")
	require.NoError(err)
	require.Len(files, 1)

	// Test Memory specific methods
	require.NoError(mem.Mkdir("/newdir"))
	require.NoError(mem.Link("/dir/here", "/dir/here2"))
	require.NoError(mem.Rename("/dir/here2", "/dir/here3"))
	require.NoError(mem.Rmdir("/newdir"))

	files, err = mem.ReadDir("/dir")
	require.NoError(err)
	require.Len(files, 2)
	require.Contains(files, "here")
	require.Contains(files, "here3")
}
