package webdav

import (
	"errors"
	"net/http"
	"testing"

	"github.com/Apollogeddon/distribyted/fs"
	"github.com/stretchr/testify/require"
)

func TestNewHandler(t *testing.T) {
	t.Parallel()
	mfs := fs.NewMemory()
	h := newHandler(mfs)
	require.NotNil(t, h)
	require.NotNil(t, h.FileSystem)
	require.NotNil(t, h.LockSystem)
	require.NotNil(t, h.Logger)

	req, _ := http.NewRequest("GET", "/", nil)
	h.Logger(req, nil)
	h.Logger(req, errors.New("test error"))
}
