package iio

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

var testData []byte = []byte("Hello World")

func TestReadData(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	br := bytes.NewReader(testData)
	r, err := NewDiskTeeReader(br)
	require.NoError(err)

	toRead := make([]byte, 5)

	n, err := r.ReadAt(toRead, 6)
	require.NoError(err)
	require.Equal(5, n)
	require.Equal("World", string(toRead))

	_, _ = r.ReadAt(toRead, 0)
	require.NoError(err)
	require.Equal(5, n)
	require.Equal("Hello", string(toRead))
}

func TestReadDataEOF(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	br := bytes.NewReader(testData)
	r, err := NewDiskTeeReader(br)
	require.NoError(err)

	toRead := make([]byte, 6)

	n, err := r.ReadAt(toRead, 6)
	require.Equal(io.EOF, err)
	require.Equal(5, n)
	require.Equal("World\x00", string(toRead))

	// Test Read
	r2, _ := NewDiskTeeReader(bytes.NewReader(testData))
	out := make([]byte, 11)
	rn, rerr := r2.Read(out)
	require.NoError(rerr)
	require.Equal(11, rn)
	require.Equal(testData, out)

	// Test Close
	require.NoError(r.Close())
	require.NoError(r2.Close())
	}
