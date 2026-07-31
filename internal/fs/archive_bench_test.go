package fs

import (
	"archive/zip"
	"bytes"
	"fmt"
	"testing"
)

// buildTestZip creates a zip archive (in memory) with n files, each holding
// content, returning the raw bytes.
func buildTestZip(b *testing.B, n int, content []byte) []byte {
	buf := bytes.NewBuffer(nil)
	zw := zip.NewWriter(buf)
	for i := 0; i < n; i++ {
		fw, err := zw.Create(fmt.Sprintf("dir/file%d.txt", i))
		if err != nil {
			b.Fatal(err)
		}
		if _, err := fw.Write(content); err != nil {
			b.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		b.Fatal(err)
	}
	return buf.Bytes()
}

// BenchmarkArchiveZip_OpenAndRead measures accessing a file within a
// zip-mounted archive (a torrent containing a .zip is transparently
// mounted as its own filesystem — see GetSupportedFactories in storage.go),
// as the number of files in the archive grows. archive.Open parses the
// zip's central directory exactly once (loadOnce, sync.Once) and caches an
// index, so "cold" (first access, real parse cost) and "warm" (repeated
// lookup against the cached index — the steady-state cost for the
// lifetime of a mounted archive) are measured separately, since they have
// very different cost profiles.
func BenchmarkArchiveZip_OpenAndRead(b *testing.B) {
	content := []byte("benchmark file content, repeated a bit to be non-trivial size.")

	for _, n := range []int{10, 100, 1000} {
		zipBytes := buildTestZip(b, n, content)
		// Worst case for a linear scan: the last-created file.
		path := fmt.Sprintf("/dir/file%d.txt", n-1)
		// Deliberately smaller than the full file: reading exactly the
		// remaining length can return (n, io.EOF) on the same call
		// (a common, valid io.Reader pattern), which isn't the "did the
		// read fail" case this benchmark wants to catch.
		out := make([]byte, len(content)/2)

		b.Run(fmt.Sprintf("cold/files=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				zfs := NewArchive(newCBR(zipBytes), int64(len(zipBytes)), &Zip{})
				f, err := zfs.Open(path)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := f.Read(out); err != nil {
					b.Fatal(err)
				}
				_ = f.Close()
			}
		})

		b.Run(fmt.Sprintf("warm/files=%d", n), func(b *testing.B) {
			zfs := NewArchive(newCBR(zipBytes), int64(len(zipBytes)), &Zip{})
			// Prime the index once, outside the timed loop.
			if _, err := zfs.Open(path); err != nil {
				b.Fatal(err)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				f, err := zfs.Open(path)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := f.Read(out); err != nil {
					b.Fatal(err)
				}
				_ = f.Close()
			}
		})
	}
}
