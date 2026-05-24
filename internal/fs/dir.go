package fs

import "io"

var _ File = &Dir{}

type Dir struct {
	BaseFile
}

func (d *Dir) Size() int64 {
	return 0
}

func (d *Dir) IsDir() bool {
	return true
}

func (d *Dir) Close() error {
	return nil
}

func (d *Dir) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

func (d *Dir) ReadAt(p []byte, off int64) (n int, err error) {
	return 0, io.EOF
}
