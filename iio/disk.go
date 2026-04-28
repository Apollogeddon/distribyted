package iio

import (
	"io"
	"os"
	"sync"
)

type DiskTeeReader struct {
	io.ReaderAt
	io.Closer
	io.Reader

	m sync.Mutex

	fo int64
	fr *os.File
	to int64
	tr io.Reader
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
	tb := off + int64(len(p))

	if tb > dtr.fo {
		w, err := io.CopyN(io.Discard, dtr.tr, tb-dtr.fo)
		dtr.to += w
		if err != nil && err != io.EOF {
			return 0, err
		}
	}

	n, err := dtr.fr.ReadAt(p, off)
	dtr.fo += int64(n)
	return n, err
}

func (dtr *DiskTeeReader) Read(p []byte) (n int, err error) {
	dtr.m.Lock()
	defer dtr.m.Unlock()
	// use directly tee reader here
	n, err = dtr.tr.Read(p)
	dtr.to += int64(n)
	return
}

func (dtr *DiskTeeReader) Close() error {
	if err := dtr.fr.Close(); err != nil {
		return err
	}

	return os.Remove(dtr.fr.Name())
}
