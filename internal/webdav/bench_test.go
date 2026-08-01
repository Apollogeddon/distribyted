package webdav

import (
	"context"
	"testing"

	"github.com/Apollogeddon/distribyted/internal/fs"
	"github.com/rs/zerolog"
)

// BenchmarkWebDAVOpenAndRead measures the cost of the WebDAV layer itself —
// a separate code path from the native HTTP API/file-browser (benchmarked
// in internal/http) or a FUSE mount, and the one most media players (Infuse,
// VLC via WebDAV, etc.) actually stream through in practice. Each iteration
// opens fresh: webDAVFile.Read uses its own pos field via ReadAt rather than
// a shared cursor on the underlying file, so this is safe to loop without
// exhausting the content.
//
// This backs onto fs.NewMemory(), not a torrent-backed filesystem, so it
// cannot detect a change anywhere in the torrent read path (readahead,
// responsive reads, storage backend) — only in the WebDAV handler wrapping
// it. Torrent-read-path changes need internal/testenv's benchmarks instead.
func BenchmarkWebDAVOpenAndRead(b *testing.B) {
	mfs := fs.NewMemory()
	content := make([]byte, 1024*1024) // 1MB
	for i := range content {
		content[i] = byte(i)
	}
	if err := mfs.Storage.Add(fs.NewMemoryFile(content), "/folder/file.txt"); err != nil {
		b.Fatal(err)
	}

	wfs := newFS(mfs, zerolog.Nop())
	buf := make([]byte, 64*1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f, err := wfs.OpenFile(context.Background(), "/folder/file.txt", 0, 0)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := f.Read(buf); err != nil {
			b.Fatal(err)
		}
		_ = f.Close()
	}
}
