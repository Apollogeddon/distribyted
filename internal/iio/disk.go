package iio

import (
	"errors"
	"io"
	"os"
	"sync"
)

type DiskTeeReader struct {
	io.ReaderAt
	io.Closer
	io.Reader

	m sync.Mutex

	fo  int64 // bytes copied to fr so far
	fr  *os.File
	tr  io.Reader
	pos int64 // where the next Read starts
}

func NewDiskTeeReader(r io.Reader) (Reader, error) {
	fr, err := os.CreateTemp("", "dtb_tmp")
	if err != nil {
		return nil, err
	}
	tr := io.TeeReader(r, fr)
	return &DiskTeeReader{fr: fr, tr: tr}, nil
}

func (dtr *DiskTeeReader) ReadAt(p []byte, off int64) (int, error) {
	dtr.m.Lock()
	defer dtr.m.Unlock()
	return dtr.readAt(p, off)
}

func (dtr *DiskTeeReader) readAt(p []byte, off int64) (int, error) {
	tb := off + int64(len(p))

	if tb > dtr.fo {
		w, err := io.CopyN(io.Discard, dtr.tr, tb-dtr.fo)
		dtr.fo += w
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
	}

	return dtr.fr.ReadAt(p, off)
}

// Read reads from its own position through the copy on disk, so it can follow a ReadAt
// that has already pulled the stream further on.
func (dtr *DiskTeeReader) Read(p []byte) (n int, err error) {
	dtr.m.Lock()
	defer dtr.m.Unlock()
	n, err = dtr.readAt(p, dtr.pos)
	dtr.pos += int64(n)
	if n > 0 && errors.Is(err, io.EOF) {
		err = nil
	}
	return n, err
}

func (dtr *DiskTeeReader) Close() error {
	if err := dtr.fr.Close(); err != nil {
		return err
	}

	return os.Remove(dtr.fr.Name())
}
