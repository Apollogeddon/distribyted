package fs

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContainer(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	fss := map[string]Filesystem{
		"/test": &DummyFs{},
	}

	c, err := NewContainerFs(fss)
	require.NoError(err)

	f, err := c.Open("/test/dir/here")
	require.NoError(err)
	require.NotNil(f)

	files, err := c.ReadDir("/")
	require.NoError(err)
	require.Len(files, 1)

	// Test Mkdir
	err = c.Mkdir("/newdir")
	require.NoError(err)
	require.True(c.s.Has("/newdir"))

	// Test Link
	err = c.Link("/test/dir/here/file1.txt", "/linked_file.txt")
	require.NoError(err)
	require.True(c.s.Has("/linked_file.txt"))

	// Test Rename
	err = c.Rename("/linked_file.txt", "/renamed_file.txt")
	require.NoError(err)
	require.True(c.s.Has("/renamed_file.txt"))
	require.False(c.s.Has("/linked_file.txt"))

	// Test Rmdir
	err = c.Rmdir("/newdir")
	require.NoError(err)
	require.False(c.s.Has("/newdir"))
}
