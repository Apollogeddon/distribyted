package webdav

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/webdav"

	"github.com/Apollogeddon/distribyted/internal/fs"
)

// readCountingFile counts reads, as a torrent file's reads are downloads.
type readCountingFile struct {
	*fs.MemoryFile
	reads *atomic.Int64
	hash  string
}

func (f *readCountingFile) Read(p []byte) (int, error) {
	f.reads.Add(1)
	return f.MemoryFile.Read(p)
}

func (f *readCountingFile) ReadAt(p []byte, off int64) (int, error) {
	f.reads.Add(1)
	return f.MemoryFile.ReadAt(p, off)
}

func (f *readCountingFile) Hash() string { return f.hash }

// TestFileInfoIsStable: asking about the same file twice gives the same time and ETag, so
// clients can resume downloads and cache, and a different file gets a different ETag.
func TestFileInfoIsStable(t *testing.T) {
	mfs := fs.NewMemory()
	require.NoError(t, mfs.Storage.Add(fs.NewMemoryFile([]byte("film")), "/films/a.mkv"))
	require.NoError(t, mfs.Storage.Add(fs.NewMemoryFile([]byte("film")), "/films/b.mkv"))
	wfs := newFS(mfs, zerolog.Nop())

	ctx := context.Background()
	first, err := wfs.Stat(ctx, "films/a.mkv")
	require.NoError(t, err)
	time.Sleep(1100 * time.Millisecond)
	second, err := wfs.Stat(ctx, "films/a.mkv")
	require.NoError(t, err)
	require.Equal(t, first.ModTime(), second.ModTime())

	etag := func(fi any) string {
		e, err := fi.(webdav.ETager).ETag(ctx)
		require.NoError(t, err)
		return e
	}
	require.Equal(t, etag(first), etag(second))
	other, err := wfs.Stat(ctx, "films/b.mkv")
	require.NoError(t, err)
	require.NotEqual(t, etag(first), etag(other))

	// the listing describes a file the same way Stat does
	dir, err := wfs.OpenFile(ctx, "/films", 0, 0)
	require.NoError(t, err)
	entries, err := dir.Readdir(0)
	require.NoError(t, err)
	for _, e := range entries {
		if e.Name() == "a.mkv" {
			require.Equal(t, etag(first), etag(e))
		}
	}

	ct, err := first.(webdav.ContentTyper).ContentType(ctx)
	require.NoError(t, err)
	require.Equal(t, "video/x-matroska", ct)
	require.NoError(t, mfs.Storage.Add(fs.NewMemoryFile([]byte("x")), "/films/README"))
	noext, err := wfs.Stat(ctx, "films/README")
	require.NoError(t, err)
	ct, err = noext.(webdav.ContentTyper).ContentType(ctx)
	require.NoError(t, err)
	require.Equal(t, "application/octet-stream", ct)
}

// TestListingReadsNoFiles: a PROPFIND of a folder used to open and read the start of
// every file to guess its type, which for a torrent means downloading it.
func TestListingReadsNoFiles(t *testing.T) {
	var reads atomic.Int64
	mfs := fs.NewMemory()
	for _, name := range []string{"/lib/a.mkv", "/lib/b", "/lib/c.srt"} {
		f := &readCountingFile{MemoryFile: fs.NewMemoryFile([]byte("some data")), reads: &reads, hash: "abc"}
		require.NoError(t, mfs.Storage.Add(f, name))
	}
	h := newHandler(mfs)

	req := httptest.NewRequest("PROPFIND", "/lib/", strings.NewReader(`<?xml version="1.0"?><propfind xmlns="DAV:"><allprop/></propfind>`))
	req.Header.Set("Depth", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusMultiStatus, w.Code)
	body := w.Body.String()
	require.Contains(t, body, "getetag")
	require.Contains(t, body, "<D:displayname>a.mkv</D:displayname>")
	require.Zero(t, reads.Load(), "listing read file data")
}
