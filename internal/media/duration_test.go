package media

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// el is an EBML element: its ID bytes, an 8-byte size, and its data.
func el(id []byte, data ...[]byte) []byte {
	body := bytes.Join(data, nil)
	size := make([]byte, 8)
	binary.BigEndian.PutUint64(size, uint64(len(body)))
	size[0] = 0x01
	return append(append(append([]byte{}, id...), size...), body...)
}

func f64(v float64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, math.Float64bits(v))
	return b
}

func mkv(scale []byte, ticks []byte) []byte {
	info := [][]byte{el([]byte{0x44, 0x89}, ticks)}
	if scale != nil {
		info = append([][]byte{el([]byte{0x2A, 0xD7, 0xB1}, scale)}, info...)
	}
	return append(
		el([]byte{0x1A, 0x45, 0xDF, 0xA3}, []byte{0x42, 0x82, 0x84, 'w', 'e', 'b', 'm'}),
		el([]byte{0x18, 0x53, 0x80, 0x67},
			el([]byte{0x11, 0x4D, 0x9B, 0x74}, make([]byte, 40)), // SeekHead
			el([]byte{0x15, 0x49, 0xA9, 0x66}, info...),
			el([]byte{0x1F, 0x43, 0xB6, 0x75}, make([]byte, 100)), // a cluster
		)...)
}

func dur(t *testing.T, b []byte, name string) (time.Duration, bool) {
	t.Helper()
	return Duration(bytes.NewReader(b), int64(len(b)), name)
}

func TestMatroska(t *testing.T) {
	// 2 h 46 min in the default millisecond ticks
	d, ok := dur(t, mkv(nil, f64(9_960_000)), "Dune.mkv")
	require.True(t, ok)
	require.Equal(t, 2*time.Hour+46*time.Minute, d)

	// a 4-byte float, in ticks of 100 µs
	f := make([]byte, 4)
	binary.BigEndian.PutUint32(f, math.Float32bits(600_000))
	d, ok = dur(t, mkv([]byte{0x01, 0x86, 0xA0}, f), "clip.webm")
	require.True(t, ok)
	require.Equal(t, time.Minute, d)

	_, ok = dur(t, []byte("not matroska at all"), "x.mkv")
	require.False(t, ok)
}

// a box is an MP4 box: its size, its type and its data.
func mp4Box(typ string, data ...[]byte) []byte {
	body := bytes.Join(data, nil)
	b := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(b, uint32(8+len(body)))
	copy(b[4:], typ)
	return append(b, body...)
}

func TestMP4(t *testing.T) {
	mvhd0 := make([]byte, 4+8+8)
	binary.BigEndian.PutUint32(mvhd0[12:], 1000)      // timescale
	binary.BigEndian.PutUint32(mvhd0[16:], 3_000_000) // 50 min
	// moov after the media data, as many files have it
	file := bytes.Join([][]byte{mp4Box("ftyp", []byte("isom")), mp4Box("mdat", make([]byte, 4096)), mp4Box("moov", mp4Box("mvhd", mvhd0))}, nil)
	d, ok := dur(t, file, "S01E01.mp4")
	require.True(t, ok)
	require.Equal(t, 50*time.Minute, d)

	mvhd1 := make([]byte, 4+16+12)
	mvhd1[0] = 1
	binary.BigEndian.PutUint32(mvhd1[20:], 600)
	binary.BigEndian.PutUint64(mvhd1[24:], 600*90)
	d, ok = dur(t, mp4Box("moov", mp4Box("mvhd", mvhd1)), "song.m4a")
	require.True(t, ok)
	require.Equal(t, 90*time.Second, d)

	_, ok = dur(t, mp4Box("ftyp", []byte("isom")), "x.mp4")
	require.False(t, ok, "no moov")
}

func TestFLAC(t *testing.T) {
	b := make([]byte, 42)
	copy(b, "fLaC")
	b[4] = 0x80 // last block, STREAMINFO
	// 44100 Hz, stereo, 16-bit, 44100 * 200 samples
	v := uint64(44100)<<44 | uint64(1)<<41 | uint64(15)<<36 | uint64(44100*200)
	binary.BigEndian.PutUint64(b[18:], v)
	d, ok := dur(t, b, "track.flac")
	require.True(t, ok)
	require.Equal(t, 200*time.Second, d)
}

func TestMP3(t *testing.T) {
	frame := []byte{0xFF, 0xFB, 0x90, 0x00} // MPEG-1 layer III, 128 kbit/s, 44100 Hz, stereo
	// with a Xing header counting 10000 frames: 10000 * 1152 / 44100 s
	x := make([]byte, 4+32+12)
	copy(x, frame)
	copy(x[36:], "Xing")
	x[43] = 1
	binary.BigEndian.PutUint32(x[44:], 10000)
	id3 := append([]byte("ID3\x04\x00\x00\x00\x00\x00\x10"), make([]byte, 16)...)
	b := append(append(id3, x...), make([]byte, 1000)...)
	d, ok := dur(t, b, "song.mp3")
	require.True(t, ok)
	frames, rate := 10000.0, 44100.0
	require.Equal(t, time.Duration(frames*1152/rate*float64(time.Second)), d)

	// constant bitrate: 128 kbit/s for 160000 bytes is 10 s
	cbr := append(append([]byte{}, frame...), make([]byte, 160000-4)...)
	d, ok = dur(t, cbr, "talk.mp3")
	require.True(t, ok)
	require.Equal(t, 10*time.Second, d)
}

// Books, ROMs and documents have no duration, and aren't read.
func TestUntimedFiles(t *testing.T) {
	for _, n := range []string{"book.epub", "manual.pdf", "game.iso", "rom.nes", "archive.zip", "noext"} {
		require.False(t, IsTimed(n), n)
		_, ok := Duration(bytes.NewReader(nil), 0, n)
		require.False(t, ok, n)
	}
	require.True(t, IsTimed("Film.MKV"))
}
